package llm

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// TurnType represents the type of conversation turn for routing decisions
type TurnType string

const (
	TurnTypeImageAnalysis      TurnType = "image_analysis"      // Requires vision - use potent
	TurnTypePreferenceGather   TurnType = "preference_gather"   // Simple collection - use economy
	TurnTypeStyleTranslation   TurnType = "style_translation"   // Critical step - use potent
	TurnTypeResultPresent      TurnType = "result_present"      // Empathy needed - use potent
	TurnTypeDiscoveryQuestion  TurnType = "discovery_question"  // Conversational question - use economy
	TurnTypeDiagnosis          TurnType = "diagnosis"           // Style diagnosis - use potent
	TurnTypeRecommendation     TurnType = "recommendation"      // Final advice - use economy
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
	case TurnTypePreferenceGather, TurnTypeDiscoveryQuestion, TurnTypeRecommendation:
		return r.economy
	default:
		return r.potent
	}
}

var tracer = otel.Tracer("stylerag/llm")

func (r *Router) Complete(ctx context.Context, turnType TurnType, req CompletionRequest) (*CompletionResponse, error) {
	tier := "potent"
	provider := r.SelectProvider(turnType)
	if provider == r.economy {
		tier = "economy"
	}

	ctx, span := tracer.Start(ctx, "llm.complete", trace.WithAttributes(
		attribute.String("llm.turn_type", string(turnType)),
		attribute.String("llm.model_tier", tier),
	))
	defer span.End()

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	span.SetAttributes(
		attribute.Int("llm.input_tokens", resp.Usage.InputTokens),
		attribute.Int("llm.output_tokens", resp.Usage.OutputTokens),
	)
	return resp, nil
}

func (r *Router) StreamComplete(ctx context.Context, turnType TurnType, req CompletionRequest) (<-chan StreamChunk, error) {
	provider := r.SelectProvider(turnType)
	return provider.StreamComplete(ctx, req)
}
