package tryon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"stylerag/internal/rag"
	"stylerag/internal/session"
	"stylerag/internal/storage"
)

// LookGenerator handles background VTON generation for composed looks.
type LookGenerator struct {
	Store        session.SessionStore
	ImageStore   storage.ImageStore
	RAGEngine    rag.Engine
	VTONProvider VTONProvider
	Timeout      time.Duration // per-look timeout
}

// GenerateAll runs VTON generation for all looks in parallel.
// It is designed to run in a background goroutine after the recommendation response.
func (g *LookGenerator) GenerateAll(ctx context.Context, sessionID string) {
	state, err := g.Store.Get(sessionID)
	if err != nil {
		slog.ErrorContext(ctx, "look_generator: failed to load session", "error", err, "session_id", sessionID)
		return
	}

	if len(state.Looks) == 0 {
		return
	}

	// Download person image once for all looks
	if state.ImageKey == "" {
		slog.ErrorContext(ctx, "look_generator: no image key in session", "session_id", sessionID)
		return
	}
	personBytes, err := g.ImageStore.Download(ctx, state.ImageKey)
	if err != nil {
		slog.ErrorContext(ctx, "look_generator: failed to download person image", "error", err, "session_id", sessionID)
		return
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, look := range state.Looks {
		wg.Add(1)
		go func(l session.Look) {
			defer wg.Done()
			lookCtx, cancel := context.WithTimeout(ctx, g.Timeout)
			defer cancel()
			g.generateSingleLook(lookCtx, sessionID, l, personBytes, &mu)
		}(look)
	}

	wg.Wait()
	slog.InfoContext(ctx, "look_generator: all looks finished", "session_id", sessionID, "look_count", len(state.Looks))
}

func (g *LookGenerator) generateSingleLook(ctx context.Context, sessionID string, look session.Look, personBytes []byte, mu *sync.Mutex) {
	start := time.Now()

	// Update status to generating
	g.updateLookStatus(ctx, sessionID, look.ID, session.LookStatusGenerating, "", mu)

	// Order pieces: upper_body first, then lower_body (for chaining)
	ordered := orderPieces(look.Pieces)

	var pieceResults []session.LookPieceResult
	currentPersonBytes := personBytes
	var lastTryOnURL string

	for _, piece := range ordered {
		pieceStart := time.Now()

		// RAG search for matching product
		products, err := g.RAGEngine.Search(ctx, rag.SearchQuery{
			Text:       piece.Description,
			Categories: []string{piece.Category},
			Limit:      1,
		})
		if err != nil {
			slog.WarnContext(ctx, "look_generator: RAG search failed",
				"error", err, "look_id", look.ID, "slot", piece.Slot)
			pieceResults = append(pieceResults, session.LookPieceResult{
				Slot:     piece.Slot,
				Category: piece.Category,
			})
			continue
		}
		if len(products) == 0 {
			slog.WarnContext(ctx, "look_generator: no product found",
				"look_id", look.ID, "slot", piece.Slot, "desc", piece.Description)
			pieceResults = append(pieceResults, session.LookPieceResult{
				Slot:     piece.Slot,
				Category: piece.Category,
			})
			continue
		}
		product := products[0]

		// Download garment image
		garmentBytes, err := downloadImage(ctx, product.ImageURL)
		if err != nil {
			slog.WarnContext(ctx, "look_generator: garment download failed",
				"error", err, "look_id", look.ID, "slot", piece.Slot)
			pieceResults = append(pieceResults, session.LookPieceResult{
				Slot:            piece.Slot,
				ProductID:       product.ID,
				ProductName:     product.Name,
				ProductImageURL: product.ImageURL,
				Category:        piece.Category,
			})
			continue
		}

		// VTON generation
		vtonResult, err := g.VTONProvider.Generate(ctx, VTONRequest{
			PersonImage:  currentPersonBytes,
			GarmentImage: garmentBytes,
			GarmentDesc:  piece.Description,
			Category:     piece.Category,
		})
		if err != nil {
			slog.WarnContext(ctx, "look_generator: VTON failed",
				"error", err, "look_id", look.ID, "slot", piece.Slot)
			pieceResults = append(pieceResults, session.LookPieceResult{
				Slot:            piece.Slot,
				ProductID:       product.ID,
				ProductName:     product.Name,
				ProductImageURL: product.ImageURL,
				Category:        piece.Category,
			})
			continue
		}

		// Upload result to R2
		resultKey := fmt.Sprintf("sessions/%s/look_%s_%s_%d.jpg",
			sessionID, look.ID, piece.Slot, time.Now().UnixMilli())
		uploadOut, err := g.ImageStore.Upload(ctx, storage.UploadInput{
			Key:         resultKey,
			Body:        bytes.NewReader(vtonResult.ImageBytes),
			ContentType: vtonResult.MimeType,
		})
		if err != nil {
			slog.WarnContext(ctx, "look_generator: upload failed",
				"error", err, "look_id", look.ID, "slot", piece.Slot)
			pieceResults = append(pieceResults, session.LookPieceResult{
				Slot:            piece.Slot,
				ProductID:       product.ID,
				ProductName:     product.Name,
				ProductImageURL: product.ImageURL,
				Category:        piece.Category,
				GenerationMs:    vtonResult.GenerationMs,
			})
			continue
		}

		// Chain: use this try-on result as the person image for the next piece
		currentPersonBytes = vtonResult.ImageBytes
		lastTryOnURL = uploadOut.URL

		pieceResults = append(pieceResults, session.LookPieceResult{
			Slot:            piece.Slot,
			ProductID:       product.ID,
			ProductName:     product.Name,
			ProductImageURL: product.ImageURL,
			TryOnImageURL:   uploadOut.URL,
			Category:        piece.Category,
			GenerationMs:    vtonResult.GenerationMs,
		})

		slog.InfoContext(ctx, "look_generator: piece done",
			"look_id", look.ID, "slot", piece.Slot,
			"product", product.Name, "elapsed", time.Since(pieceStart))
	}

	elapsed := time.Since(start)

	result := session.LookResult{
		LookID:        look.ID,
		Status:        session.LookStatusReady,
		Pieces:        pieceResults,
		FinalImageURL: lastTryOnURL,
		GenerationMs:  elapsed.Milliseconds(),
		CreatedAt:     time.Now(),
	}

	// Check if any piece failed completely (no tryon image)
	allFailed := true
	for _, pr := range pieceResults {
		if pr.TryOnImageURL != "" {
			allFailed = false
			break
		}
	}
	if allFailed {
		result.Status = session.LookStatusFailed
		result.ErrorMessage = "all pieces failed VTON generation"
	}

	g.updateLookResult(ctx, sessionID, result, mu)

	slog.InfoContext(ctx, "look_generator: look done",
		"look_id", look.ID, "status", result.Status,
		"elapsed", elapsed, "session_id", sessionID)
}

func (g *LookGenerator) updateLookStatus(ctx context.Context, sessionID, lookID string, status session.LookStatus, errMsg string, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()

	state, err := g.Store.Get(sessionID)
	if err != nil {
		slog.ErrorContext(ctx, "look_generator: failed to load session for status update", "error", err)
		return
	}

	found := false
	for i := range state.LookResults {
		if state.LookResults[i].LookID == lookID {
			state.LookResults[i].Status = status
			if errMsg != "" {
				state.LookResults[i].ErrorMessage = errMsg
			}
			found = true
			break
		}
	}
	if !found {
		state.LookResults = append(state.LookResults, session.LookResult{
			LookID:    lookID,
			Status:    status,
			CreatedAt: time.Now(),
		})
	}

	if err := g.Store.Save(state); err != nil {
		slog.ErrorContext(ctx, "look_generator: failed to save status update", "error", err)
	}
}

func (g *LookGenerator) updateLookResult(ctx context.Context, sessionID string, result session.LookResult, mu *sync.Mutex) {
	mu.Lock()
	defer mu.Unlock()

	state, err := g.Store.Get(sessionID)
	if err != nil {
		slog.ErrorContext(ctx, "look_generator: failed to load session for result update", "error", err)
		return
	}

	found := false
	for i := range state.LookResults {
		if state.LookResults[i].LookID == result.LookID {
			state.LookResults[i] = result
			found = true
			break
		}
	}
	if !found {
		state.LookResults = append(state.LookResults, result)
	}

	if err := g.Store.Save(state); err != nil {
		slog.ErrorContext(ctx, "look_generator: failed to save result update", "error", err)
	}
}

// orderPieces sorts pieces so upper_body comes before lower_body (for VTON chaining).
func orderPieces(pieces []session.LookPiece) []session.LookPiece {
	ordered := make([]session.LookPiece, 0, len(pieces))
	// Upper body first
	for _, p := range pieces {
		if p.Slot == "upper_body" {
			ordered = append(ordered, p)
		}
	}
	// Then lower body
	for _, p := range pieces {
		if p.Slot == "lower_body" {
			ordered = append(ordered, p)
		}
	}
	// Any remaining slots
	for _, p := range pieces {
		if p.Slot != "upper_body" && p.Slot != "lower_body" {
			ordered = append(ordered, p)
		}
	}
	return ordered
}

// downloadImage fetches an image from a URL. Duplicated from tryon_handler to keep within package.
func downloadImage(ctx context.Context, url string) ([]byte, error) {
	dlCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image fetch returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading image body: %w", err)
	}
	return data, nil
}
