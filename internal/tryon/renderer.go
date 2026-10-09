package tryon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/storage"
)

// Renderer produces try-on images for garment combinations. Every prefix of
// the chain (shirt; shirt+pants; shirt+pants+shoes) is cached in the session,
// so moving one swipe away usually costs a single VTON call, and identical
// requests in flight are collapsed.
type Renderer struct {
	Store   session.Store
	Images  storage.ImageStore
	VTON    VTONProvider
	Details shopping.DetailResolver // optional: larger photos and merchant links
	HTTP    *http.Client
	Timeout time.Duration // per VTON call

	sem         chan struct{}
	prefetchSem chan struct{}
	mu          sync.Mutex
	inflight    map[string]chan struct{}
}

// NewRenderer builds a renderer with bounded concurrency. prefetch jobs use at
// most prefetchConcurrency of the concurrency slots, so a user's request
// never waits behind a full queue of speculative renders.
func NewRenderer(store session.Store, images storage.ImageStore, vton VTONProvider, details shopping.DetailResolver, httpClient *http.Client, concurrency, prefetchConcurrency int, timeout time.Duration) *Renderer {
	if concurrency <= 0 {
		concurrency = 3
	}
	if prefetchConcurrency <= 0 || prefetchConcurrency >= concurrency {
		prefetchConcurrency = max(1, concurrency-1)
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Renderer{
		Store: store, Images: images, VTON: vton, Details: details, HTTP: httpClient, Timeout: timeout,
		sem:         make(chan struct{}, concurrency),
		prefetchSem: make(chan struct{}, prefetchConcurrency),
		inflight:    map[string]chan struct{}{},
	}
}

// Request renders in the background. prefetch marks speculative work.
func (r *Renderer) Request(sessionID string, pieces []session.ComboPiece, prefetch bool) {
	if len(pieces) == 0 {
		return
	}
	go func() {
		if prefetch {
			r.prefetchSem <- struct{}{}
			defer func() { <-r.prefetchSem }()
		}
		r.sem <- struct{}{}
		defer func() { <-r.sem }()

		ctx, cancel := context.WithTimeout(context.Background(), r.Timeout*time.Duration(len(pieces))+30*time.Second)
		defer cancel()
		if _, err := r.Render(ctx, sessionID, pieces); err != nil {
			slog.WarnContext(ctx, "render: failed", "session_id", sessionID, "key", session.ComboKey(pieces), "prefetch", prefetch, "error", err)
		}
	}()
}

// Render produces the image for the full combination synchronously and
// returns its record. Already-rendered prefixes are reused.
func (r *Renderer) Render(ctx context.Context, sessionID string, pieces []session.ComboPiece) (session.Render, error) {
	if len(pieces) == 0 {
		return session.Render{}, errors.New("render: empty combination")
	}
	st, err := r.Store.Get(ctx, sessionID)
	if err != nil {
		return session.Render{}, err
	}
	if st.ImageKey == "" {
		return session.Render{}, errors.New("render: session has no photo")
	}
	fullKey := session.ComboKey(pieces)
	if rd, ok := st.Renders[fullKey]; !ok || rd.Status == session.LookStatusFailed || rd.Status == session.LookStatusPending {
		r.setRender(ctx, sessionID, session.Render{Key: fullKey, Pieces: pieces, Status: session.LookStatusGenerating, UpdatedAt: time.Now()})
	}

	var person []byte // bytes of the previous layer; nil means "load lazily"
	var prev session.Render
	for i := range pieces {
		prefix := pieces[:i+1]
		key := session.ComboKey(prefix)

		if rd, ok := st.Renders[key]; ok && rd.Status == session.LookStatusReady {
			prev, person = rd, nil
			continue
		}

		done, first := r.claim(sessionID, key)
		if !first {
			select {
			case <-done:
			case <-ctx.Done():
				return session.Render{}, ctx.Err()
			}
			st, err = r.Store.Get(ctx, sessionID)
			if err != nil {
				return session.Render{}, err
			}
			rd, ok := st.Renders[key]
			if !ok || rd.Status != session.LookStatusReady {
				return r.failFull(ctx, sessionID, pieces, rd.Error), fmt.Errorf("render: %s failed in another request", key)
			}
			prev, person = rd, nil
			continue
		}

		rd, outBytes, err := r.renderStep(ctx, st, prefix, prev, person)
		r.release(sessionID, key)
		if err != nil {
			if key != fullKey {
				r.failFull(ctx, sessionID, pieces, rd.Error)
			}
			return rd, err
		}
		prev, person = rd, outBytes
		st.Renders = withRender(st.Renders, rd)
	}
	return prev, nil
}

// failFull records the whole combination as failed with the prefix's reason.
func (r *Renderer) failFull(ctx context.Context, sessionID string, pieces []session.ComboPiece, reason string) session.Render {
	if reason == "" {
		reason = "una prenda anterior no se pudo generar"
	}
	rd := session.Render{Key: session.ComboKey(pieces), Pieces: pieces, Status: session.LookStatusFailed, Error: reason, UpdatedAt: time.Now()}
	r.setRender(ctx, sessionID, rd)
	return rd
}

// renderStep renders one more garment on top of prev and persists the result.
func (r *Renderer) renderStep(ctx context.Context, st *session.State, prefix []session.ComboPiece, prev session.Render, person []byte) (session.Render, []byte, error) {
	key := session.ComboKey(prefix)
	piece := prefix[len(prefix)-1]
	start := time.Now()
	r.setRender(ctx, st.ID, session.Render{Key: key, Pieces: prefix, Status: session.LookStatusGenerating, UpdatedAt: time.Now()})

	fail := func(msg string, err error) (session.Render, []byte, error) {
		rd := session.Render{Key: key, Pieces: prefix, Status: session.LookStatusFailed, Error: msg, UpdatedAt: time.Now()}
		r.setRender(ctx, st.ID, rd)
		return rd, nil, fmt.Errorf("render %s: %s: %w", key, msg, err)
	}

	var err error
	if person == nil {
		src := st.ImageKey
		if len(prefix) > 1 {
			src = prev.ImageKey
		}
		if person, err = r.Images.Download(ctx, src); err != nil {
			return fail("no pude cargar la imagen base", err)
		}
	}

	product, ok := st.Products[piece.ProductID]
	if !ok {
		return fail("producto desconocido", errors.New(piece.ProductID))
	}
	garment, product, err := r.garmentImage(ctx, st.ID, product)
	if err != nil {
		return fail("no pude descargar la foto de la prenda", err)
	}

	vtonCtx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	out, err := r.VTON.Generate(vtonCtx, VTONRequest{PersonImage: person, GarmentImage: garment, GarmentDesc: product.Title, Category: piece.Slot})
	if err != nil {
		return fail(explainVTONError(err), err)
	}

	imgKey := fmt.Sprintf("sessions/%s/render_%s_%d.jpg", st.ID, shortKey(key), time.Now().UnixMilli())
	up, err := r.Images.Upload(ctx, storage.UploadInput{Key: imgKey, Body: bytes.NewReader(out.ImageBytes), ContentType: out.MimeType})
	if err != nil {
		return fail("no pude guardar la imagen", err)
	}
	rd := session.Render{
		Key: key, Pieces: prefix, Status: session.LookStatusReady,
		ImageKey: imgKey, ImageURL: up.URL, GenerationMs: time.Since(start).Milliseconds(), UpdatedAt: time.Now(),
	}
	r.setRender(ctx, st.ID, rd)
	slog.InfoContext(ctx, "render: ready", "session_id", st.ID, "key", key, "elapsed_ms", rd.GenerationMs)
	return rd, out.ImageBytes, nil
}

// garmentImage picks the largest available photo of the product, resolving
// details on first use and persisting what it learned.
func (r *Renderer) garmentImage(ctx context.Context, sessionID string, p shopping.Product) ([]byte, shopping.Product, error) {
	var candidates []string
	changed := false
	if p.LargeImage != "" {
		candidates = append(candidates, p.LargeImage)
	} else if r.Details != nil && p.SourceID != "" {
		if d, err := r.Details.ProductDetails(ctx, p.SourceID); err == nil && d != nil {
			for i, img := range d.Images {
				if i >= 3 {
					break
				}
				candidates = append(candidates, img)
			}
			if d.MerchantLink != "" && d.MerchantLink != p.Link {
				p.Link, changed = d.MerchantLink, true
			}
			if d.Availability != "" && d.Availability != p.Availability {
				p.Availability, changed = d.Availability, true
			}
		} else if err != nil {
			slog.WarnContext(ctx, "render: product details failed", "product", p.ID, "error", err)
		}
	}
	if p.Thumbnail != "" {
		candidates = append(candidates, p.Thumbnail)
	}
	if len(candidates) == 0 {
		return nil, p, errors.New("product has no image")
	}

	type got struct {
		url  string
		data []byte
		area int
	}
	results := make([]got, len(candidates))
	var wg sync.WaitGroup
	for i, u := range candidates {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			data, err := DownloadImage(ctx, r.HTTP, u, 10<<20)
			if err != nil {
				return
			}
			results[i] = got{url: u, data: data, area: pixelArea(data)}
		}(i, u)
	}
	wg.Wait()
	best := -1
	for i, g := range results {
		if g.data == nil {
			continue
		}
		if best < 0 || g.area > results[best].area || (g.area == results[best].area && len(g.data) > len(results[best].data)) {
			best = i
		}
	}
	if best < 0 {
		return nil, p, errors.New("every candidate image failed to download")
	}
	if results[best].url != p.LargeImage {
		p.LargeImage, changed = results[best].url, true
	}
	if changed {
		_ = r.Store.Update(ctx, sessionID, func(cur *session.State) error {
			if cur.Products == nil {
				cur.Products = map[string]shopping.Product{}
			}
			if existing, ok := cur.Products[p.ID]; ok {
				existing.LargeImage, existing.Link, existing.Availability = p.LargeImage, p.Link, p.Availability
				cur.Products[p.ID] = existing
			}
			return nil
		})
	}
	return results[best].data, p, nil
}

func pixelArea(data []byte) int {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0
	}
	return cfg.Width * cfg.Height
}

func (r *Renderer) setRender(ctx context.Context, sessionID string, rd session.Render) {
	if err := r.Store.Update(ctx, sessionID, func(cur *session.State) error {
		cur.Renders = withRender(cur.Renders, rd)
		return nil
	}); err != nil {
		slog.ErrorContext(ctx, "render: save state", "session_id", sessionID, "key", rd.Key, "error", err)
	}
}

func withRender(m map[string]session.Render, rd session.Render) map[string]session.Render {
	if m == nil {
		m = map[string]session.Render{}
	}
	m[rd.Key] = rd
	return m
}

// claim marks key as in flight. first is false when another goroutine owns it;
// the returned channel closes when that goroutine finishes.
func (r *Renderer) claim(sessionID, key string) (chan struct{}, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := sessionID + "|" + key
	if ch, ok := r.inflight[k]; ok {
		return ch, false
	}
	ch := make(chan struct{})
	r.inflight[k] = ch
	return ch, true
}

func (r *Renderer) release(sessionID, key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := sessionID + "|" + key
	if ch, ok := r.inflight[k]; ok {
		close(ch)
		delete(r.inflight, k)
	}
}

func shortKey(key string) string {
	var sb bytes.Buffer
	for _, c := range key {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			sb.WriteRune(c)
		case c == '|':
			sb.WriteRune('-')
		}
	}
	s := sb.String()
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// explainVTONError turns provider errors into a short reason the app can show
// and the operator can act on.
func explainVTONError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "BILLING_DISABLED"):
		return "try-on no disponible: el proyecto de Google Cloud no tiene facturación habilitada"
	case strings.Contains(msg, "SERVICE_DISABLED") || strings.Contains(msg, "has not been used in project"):
		return "try-on no disponible: la API de Vertex AI no está habilitada en el proyecto"
	case strings.Contains(msg, "RESOURCE_EXHAUSTED") || strings.Contains(msg, "returned 429"):
		return "try-on saturado: cuota de Vertex AI agotada, intenta en un momento"
	case strings.Contains(msg, "context deadline exceeded"):
		return "la generación tardó demasiado"
	default:
		return "la generación falló"
	}
}
