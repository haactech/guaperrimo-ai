package tryon

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"stylerag/internal/session"
)

// LookGenerator renders the agent's looks through the shared Renderer so
// matrix swipes and look cards reuse the same images.
type LookGenerator struct {
	Store    session.Store
	Renderer *Renderer
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
	var wg sync.WaitGroup
	for _, look := range st.Looks {
		wg.Add(1)
		go func(l session.Look) {
			defer wg.Done()
			g.Renderer.sem <- struct{}{}
			defer func() { <-g.Renderer.sem }()
			g.generateLook(ctx, sessionID, l, st)
		}(look)
	}
	wg.Wait()
	slog.InfoContext(ctx, "looks: all done", "session_id", sessionID, "count", len(st.Looks))
}

func (g *LookGenerator) generateLook(ctx context.Context, sessionID string, look session.Look, st *session.State) {
	start := time.Now()
	pieces := comboFromLook(look, st)
	g.setStatus(ctx, sessionID, look.ID, session.LookStatusGenerating)

	result := session.LookResult{LookID: look.ID, Status: session.LookStatusReady, CreatedAt: time.Now()}
	if len(pieces) == 0 {
		result.Status = session.LookStatusFailed
		result.ErrorMessage = "el look no tiene prendas con imagen"
		g.setResult(ctx, sessionID, result)
		return
	}

	renderCtx, cancel := context.WithTimeout(ctx, g.Renderer.Timeout*time.Duration(len(pieces))+30*time.Second)
	defer cancel()
	final, err := g.Renderer.Render(renderCtx, sessionID, pieces)

	cur, _ := g.Store.Get(ctx, sessionID)
	for i, piece := range pieces {
		pr := session.LookPieceResult{Slot: piece.Slot, ProductID: piece.ProductID}
		if p, ok := st.Products[piece.ProductID]; ok {
			pr.ProductName, pr.ProductImageURL = p.Title, p.Thumbnail
		}
		if cur != nil {
			if rd, ok := cur.Renders[session.ComboKey(pieces[:i+1])]; ok && rd.Status == session.LookStatusReady {
				pr.TryOnImageURL, pr.GenerationMs = rd.ImageURL, rd.GenerationMs
			}
		}
		result.Pieces = append(result.Pieces, pr)
	}
	result.FinalImageURL = final.ImageURL
	result.GenerationMs = time.Since(start).Milliseconds()
	if err != nil || final.Status != session.LookStatusReady {
		// Keep whatever prefix rendered; mark failed only when nothing did.
		if final.ImageURL == "" {
			for i := len(result.Pieces) - 1; i >= 0; i-- {
				if result.Pieces[i].TryOnImageURL != "" {
					result.FinalImageURL = result.Pieces[i].TryOnImageURL
					break
				}
			}
		}
		if result.FinalImageURL == "" {
			result.Status = session.LookStatusFailed
			result.ErrorMessage = "no se pudo generar ninguna prenda del look"
		}
	}
	g.setResult(ctx, sessionID, result)
	slog.InfoContext(ctx, "looks: look done", "session_id", sessionID, "look_id", look.ID, "status", result.Status, "elapsed", time.Since(start))
}

// comboFromLook orders the look's renderable pieces for chaining.
func comboFromLook(look session.Look, st *session.State) []session.ComboPiece {
	var out []session.ComboPiece
	for _, p := range look.Pieces {
		prod, ok := st.Products[p.ProductID]
		if !ok || (prod.Thumbnail == "" && prod.LargeImage == "") || session.SlotRank(p.Slot) >= len(session.SlotOrder) {
			continue
		}
		out = append(out, session.ComboPiece{Slot: p.Slot, ProductID: p.ProductID})
	}
	sort.SliceStable(out, func(i, j int) bool { return session.SlotRank(out[i].Slot) < session.SlotRank(out[j].Slot) })
	return out
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
