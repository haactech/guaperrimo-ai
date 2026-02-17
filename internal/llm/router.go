package llm

import (
	"context"
)

// TurnType represents the type of conversation turn for routing decisions
type TurnType string

const (
	TurnTypeImageAnalysis    TurnType = "image_analysis"    // Requires vision - use potent
	TurnTypePreferenceGather TurnType = "preference_gather" // Simple collection - use economy
	TurnTypeStyleTranslation TurnType = "style_translation" // Critical step - use potent
	TurnTypeResultPresent    TurnType = "result_present"    // Empathy needed - use potent
)

// Router selects the appropriate model tier based on turn type
type Router struct {
	potent  Provider
	economy Provider
}

func NewRouter(potent, economy Provider) *Router {
	return &Router{
		potent:  potent,
		economy: economy,
	}
}

func (r *Router) SelectProvider(turnType TurnType) Provider {
	switch turnType {
	case TurnTypePreferenceGather:
		return r.economy
	default:
		return r.potent
	}
}

func (r *Router) Complete(ctx context.Context, turnType TurnType, req CompletionRequest) (*CompletionResponse, error) {
	provider := r.SelectProvider(turnType)
	return provider.Complete(ctx, req)
}

func (r *Router) StreamComplete(ctx context.Context, turnType TurnType, req CompletionRequest) (<-chan StreamChunk, error) {
	provider := r.SelectProvider(turnType)
	return provider.StreamComplete(ctx, req)
}
