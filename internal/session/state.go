package session

import "time"

// Phase represents the current phase of the conversational flow.
type Phase string

const (
	PhaseCapture        Phase = "capture"
	PhaseDiscovery      Phase = "discovery"
	PhaseDiagnosis      Phase = "diagnosis"
	PhaseRecommendation Phase = "recommendation"
)

// SessionState tracks the full state of a conversational session.
type SessionState struct {
	ID                string            `json:"id"`
	Phase             Phase             `json:"phase"`
	Turn              int               `json:"turn"`
	OutfitAnalysis    any               `json:"outfit_analysis"` // *vision.OutfitAnalysis stored as any to avoid circular imports
	Responses         []UserResponse    `json:"responses"`
	AssistantMessages []string          `json:"assistant_messages"` // what the bot said each turn
	CoveredFacts      map[string]string `json:"covered_facts"`     // category → summary of what we learned
	Diagnosis         *StyleDiagnosis   `json:"diagnosis,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
}

// RequiredCategories are the minimum needed before advancing to diagnosis.
var RequiredCategories = []string{"occasion", "intention"}

// OptionalCategories provide richer context but aren't blocking.
var AllCategories = []string{"occasion", "intention", "exploration", "pain_points", "aspirational", "constraints", "budget"}

// MaxDiscoveryTurns is the hard cap on discovery questions.
const MaxDiscoveryTurns = 5

// ReadyForDiagnosis returns true when we have occasion + intention + at least 1 more fact.
func (s *SessionState) ReadyForDiagnosis() bool {
	if s.CoveredFacts == nil {
		return false
	}
	_, hasOccasion := s.CoveredFacts["occasion"]
	_, hasIntention := s.CoveredFacts["intention"]
	if !hasOccasion || !hasIntention {
		return false
	}
	return len(s.CoveredFacts) >= 3
}

// UserResponse captures a single user answer during the discovery phase.
type UserResponse struct {
	QuestionID string    `json:"question_id"`
	Category   string    `json:"category"`
	InputMode  string    `json:"input_mode"` // "button" | "voice"
	Value      string    `json:"value"`
	Timestamp  time.Time `json:"timestamp"`
}

// StyleDiagnosis is the structured output of Phase 3 (diagnosis).
type StyleDiagnosis struct {
	CurrentAssessment CurrentAssessment `json:"current_assessment"`
	UserProfile       UserProfile       `json:"user_profile"`
	GapRanking        []GapRanking      `json:"gap_ranking"`
}

type CurrentAssessment struct {
	Strengths    []string `json:"strengths"`
	Gaps         []string `json:"gaps"`
	StyleDistance string   `json:"style_distance"` // "baja", "media", "alta"
}

type UserProfile struct {
	Occasion              string   `json:"occasion"`
	DesiredProjection     string   `json:"desired_projection"`
	Approach              string   `json:"approach"` // "refinar" | "explorar"
	PainPoints            []string `json:"pain_points"`
	AspirationalReference string   `json:"aspirational_reference"`
	Constraints           []string `json:"constraints"`
}

type GapRanking struct {
	Area     string `json:"area"`
	Priority int    `json:"priority"`
	Current  int    `json:"current"` // 1-10
	Target   int    `json:"target"`  // 1-10
	Note     string `json:"note"`
}

// PersonalizedAdvice is the output of Phase 4 (recommendation).
type PersonalizedAdvice struct {
	SpokenSummary   string           `json:"spoken_summary"`
	PriorityActions []PriorityAction `json:"priority_actions"`
}

type PriorityAction struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Impact      string `json:"impact"` // "alto", "medio", "bajo"
	Effort      string `json:"effort"` // "alto", "medio", "bajo"
}
