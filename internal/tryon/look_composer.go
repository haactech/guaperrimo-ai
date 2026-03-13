package tryon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"stylerag/internal/llm"
	"stylerag/internal/session"
)

// LookComposer uses an LLM to compose outfit looks from the user's style profile.
type LookComposer struct {
	router    *llm.Router
	lookCount int
}

func NewLookComposer(router *llm.Router, lookCount int) *LookComposer {
	return &LookComposer{router: router, lookCount: lookCount}
}

// ComposeLooks generates N outfit look definitions using the economy LLM.
func (c *LookComposer) ComposeLooks(ctx context.Context, profile *session.UserStyleProfile, actions []session.PriorityAction) ([]session.Look, error) {
	profileJSON, _ := json.Marshal(profile)
	actionsJSON, _ := json.Marshal(actions)

	prompt := fmt.Sprintf(lookCompositionPrompt, c.lookCount, string(profileJSON), string(actionsJSON))

	resp, err := c.router.Complete(ctx, llm.TurnTypeLookComposition, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: prompt},
			{Role: llm.RoleUser, Content: "Genera los looks."},
		},
		MaxTokens: 4096,
	})
	if err != nil {
		return nil, fmt.Errorf("look composition LLM call failed: %w", err)
	}

	cleaned := cleanJSON(resp.Content)
	var parsed struct {
		Looks []session.Look `json:"looks"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		slog.ErrorContext(ctx, "look_composer: failed to parse response", "raw", resp.Content, "error", err)
		return nil, fmt.Errorf("parsing look composition response: %w", err)
	}

	// Assign IDs if missing
	for i := range parsed.Looks {
		if parsed.Looks[i].ID == "" {
			parsed.Looks[i].ID = fmt.Sprintf("look_%d", i+1)
		}
	}

	return parsed.Looks, nil
}

// cleanJSON strips markdown code fences and repairs unescaped interior quotes.
// Duplicated from internal/vision to avoid cross-package dependency.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i != -1 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i != -1 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	return s
}
