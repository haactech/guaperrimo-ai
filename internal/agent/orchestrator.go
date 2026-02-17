package agent

import (
	"context"

	"stylerag/internal/llm"
	"stylerag/internal/rag"
	"stylerag/internal/session"
	"stylerag/internal/vision"
)

// Orchestrator manages the conversation flow and tool execution
type Orchestrator struct {
	llmRouter *llm.Router
	rag       rag.Engine
	vision    vision.Analyzer
	sessions  session.Manager
	prompts   *PromptManager
}

type OrchestratorConfig struct {
	LLMRouter *llm.Router
	RAG       rag.Engine
	Vision    vision.Analyzer
	Sessions  session.Manager
	Prompts   *PromptManager
}

func NewOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	return &Orchestrator{
		llmRouter: cfg.LLMRouter,
		rag:       cfg.RAG,
		vision:    cfg.Vision,
		sessions:  cfg.Sessions,
		prompts:   cfg.Prompts,
	}
}

type ProcessRequest struct {
	SessionID string
	Message   string
	Image     []byte // Optional: user-uploaded outfit photo
}

type ProcessResponse struct {
	Message      string
	Products     []rag.Product
	TurnType     llm.TurnType
	TokensUsed   int
}

// Process handles an incoming user message and returns a response
func (o *Orchestrator) Process(ctx context.Context, req ProcessRequest) (*ProcessResponse, error) {
	// Get or create session
	profile, err := o.sessions.Get(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}

	// Determine turn type based on conversation state
	turnType := o.determineTurnType(profile, req)

	// If image provided, analyze it first
	if len(req.Image) > 0 {
		analysis, err := o.vision.AnalyzeOutfit(ctx, req.Image)
		if err != nil {
			return nil, err
		}
		profile.CurrentStyle = analysis.DetectedStyles
		if err := o.sessions.Update(ctx, req.SessionID, profile); err != nil {
			return nil, err
		}
	}

	// Build prompt with context
	messages := o.prompts.BuildMessages(profile, req.Message)

	// Get completion from appropriate model
	resp, err := o.llmRouter.Complete(ctx, turnType, llm.CompletionRequest{
		Messages:    messages,
		MaxTokens:   1024,
		Temperature: 0.7,
		Tools:       o.getAvailableTools(),
	})
	if err != nil {
		return nil, err
	}

	// Handle tool calls if any
	var products []rag.Product
	for _, tc := range resp.ToolCalls {
		switch tc.Name {
		case "search_catalog":
			products, err = o.handleSearchCatalog(ctx, tc.Arguments, profile)
			if err != nil {
				return nil, err
			}
		}
	}

	return &ProcessResponse{
		Message:    resp.Content,
		Products:   products,
		TurnType:   turnType,
		TokensUsed: resp.Usage.InputTokens + resp.Usage.OutputTokens,
	}, nil
}

func (o *Orchestrator) determineTurnType(profile *session.StyleProfile, req ProcessRequest) llm.TurnType {
	if len(req.Image) > 0 {
		return llm.TurnTypeImageAnalysis
	}
	if profile.TargetStyle == nil {
		return llm.TurnTypePreferenceGather
	}
	if profile.Budget.Max == 0 {
		return llm.TurnTypePreferenceGather
	}
	return llm.TurnTypeResultPresent
}

func (o *Orchestrator) getAvailableTools() []llm.Tool {
	return []llm.Tool{
		{
			Name:        "analyze_outfit",
			Description: "Analyze the user's current outfit from an uploaded photo",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"image": map[string]any{"type": "string", "description": "Base64 encoded image"},
				},
			},
		},
		{
			Name:        "search_catalog",
			Description: "Search the product catalog for items matching style criteria",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"style_tags":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"categories":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"price_max":    map[string]any{"type": "number"},
					"exclude_tags": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
			},
		},
		{
			Name:        "get_style_profile",
			Description: "Get the current user's style profile and preferences",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

func (o *Orchestrator) handleSearchCatalog(ctx context.Context, args string, profile *session.StyleProfile) ([]rag.Product, error) {
	// TODO: Parse args and build search query
	query := rag.SearchQuery{
		StyleTags: profile.TargetStyle,
		MaxPrice:  profile.Budget.Max,
		Limit:     5,
	}
	return o.rag.Search(ctx, query)
}
