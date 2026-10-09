package tryon

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/storage"
)

const maxGarmentBytes = 10 << 20

// LookGenerator renders the agent's looks on the user's photo in the background.
type LookGenerator struct {
	Store       session.Store
	Images      storage.ImageStore
	VTON        VTONProvider
	Timeout     time.Duration // per look
	HTTPClient  *http.Client
	Concurrency int
}

// GenerateAll renders every look of the session. Safe to run in a goroutine.
func (g *LookGenerator) GenerateAll(ctx context.Context, sessionID string) {
	st, err := g.Store.Get(ctx, sessionID)
	if err != nil {
		slog.ErrorContext(ctx, "looks: load session", "error", err, "session_id", sessionID)
		return
	}
	if len(st.Looks) == 0 || st.ImageKey == "" {
		return
	}
	person, err := g.Images.Download(ctx, st.ImageKey)
	if err != nil {
		slog.ErrorContext(ctx, "looks: download person image", "error", err, "session_id", sessionID)
		return
	}

	conc := g.Concurrency
	if conc <= 0 {
		conc = 2
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for _, look := range st.Looks {
		wg.Add(1)
		go func(l session.Look) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			timeout := g.Timeout
			if timeout <= 0 {
				timeout = 90 * time.Second
			}
			lookCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			g.generateLook(lookCtx, sessionID, l, st.Products, person)
		}(look)
	}
	wg.Wait()
	slog.InfoContext(ctx, "looks: all done", "session_id", sessionID, "count", len(st.Looks))
}

func (g *LookGenerator) generateLook(ctx context.Context, sessionID string, look session.Look, products map[string]shopping.Product, person []byte) {
	start := time.Now()
	g.setStatus(ctx, sessionID, look.ID, session.LookStatusGenerating)

	current := person
	var pieces []session.LookPieceResult
	var lastURL string

	for _, piece := range orderPieces(look.Pieces) {
		product, ok := products[piece.ProductID]
		res := session.LookPieceResult{Slot: piece.Slot, ProductID: piece.ProductID, ProductName: product.Title, ProductImageURL: product.Thumbnail}
		if !ok || product.Thumbnail == "" {
			slog.WarnContext(ctx, "looks: piece without product image", "look_id", look.ID, "slot", piece.Slot)
			pieces = append(pieces, res)
			continue
		}
		garment, err := DownloadImage(ctx, g.HTTPClient, product.Thumbnail, maxGarmentBytes)
		if err != nil {
			slog.WarnContext(ctx, "looks: garment download failed", "error", err, "look_id", look.ID, "slot", piece.Slot)
			pieces = append(pieces, res)
			continue
		}
		out, err := g.VTON.Generate(ctx, VTONRequest{PersonImage: current, GarmentImage: garment, GarmentDesc: product.Title, Category: piece.Slot})
		if err != nil {
			slog.WarnContext(ctx, "looks: vton failed", "error", err, "look_id", look.ID, "slot", piece.Slot)
			pieces = append(pieces, res)
			continue
		}
		key := fmt.Sprintf("sessions/%s/look_%s_%s_%d.jpg", sessionID, look.ID, piece.Slot, time.Now().UnixMilli())
		up, err := g.Images.Upload(ctx, storage.UploadInput{Key: key, Body: bytes.NewReader(out.ImageBytes), ContentType: out.MimeType})
		if err != nil {
			slog.WarnContext(ctx, "looks: upload failed", "error", err, "look_id", look.ID, "slot", piece.Slot)
			res.GenerationMs = out.GenerationMs
			pieces = append(pieces, res)
			continue
		}
		current = out.ImageBytes
		lastURL = up.URL
		res.TryOnImageURL = up.URL
		res.GenerationMs = out.GenerationMs
		pieces = append(pieces, res)
	}

	result := session.LookResult{
		LookID:        look.ID,
		Status:        session.LookStatusReady,
		Pieces:        pieces,
		FinalImageURL: lastURL,
		GenerationMs:  time.Since(start).Milliseconds(),
		CreatedAt:     time.Now(),
	}
	if lastURL == "" {
		result.Status = session.LookStatusFailed
		result.ErrorMessage = "no se pudo generar ninguna prenda del look"
	}
	g.setResult(ctx, sessionID, result)
	slog.InfoContext(ctx, "looks: look done", "session_id", sessionID, "look_id", look.ID, "status", result.Status, "elapsed", time.Since(start))
}

func (g *LookGenerator) setStatus(ctx context.Context, sessionID, lookID string, status session.LookStatus) {
	err := g.Store.Update(ctx, sessionID, func(st *session.State) error {
		for i := range st.LookResults {
			if st.LookResults[i].LookID == lookID {
				st.LookResults[i].Status = status
				return nil
			}
		}
		st.LookResults = append(st.LookResults, session.LookResult{LookID: lookID, Status: status, CreatedAt: time.Now()})
		return nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "looks: save status", "error", err, "session_id", sessionID)
	}
}

func (g *LookGenerator) setResult(ctx context.Context, sessionID string, result session.LookResult) {
	err := g.Store.Update(ctx, sessionID, func(st *session.State) error {
		for i := range st.LookResults {
			if st.LookResults[i].LookID == result.LookID {
				st.LookResults[i] = result
				return nil
			}
		}
		st.LookResults = append(st.LookResults, result)
		return nil
	})
	if err != nil {
		slog.ErrorContext(ctx, "looks: save result", "error", err, "session_id", sessionID)
	}
}

// orderPieces puts upper_body first so lower_body chains on top of it.
func orderPieces(pieces []session.LookPiece) []session.LookPiece {
	rank := map[string]int{"upper_body": 0, "outerwear": 1, "lower_body": 2, "footwear": 3}
	ordered := make([]session.LookPiece, len(pieces))
	copy(ordered, pieces)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && rankOf(rank, ordered[j].Slot) < rankOf(rank, ordered[j-1].Slot); j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	return ordered
}

func rankOf(rank map[string]int, slot string) int {
	if r, ok := rank[slot]; ok {
		return r
	}
	return 9
}
