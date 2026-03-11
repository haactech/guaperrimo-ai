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
	StyleProfile      *UserStyleProfile `json:"style_profile,omitempty"`
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
	Profile UserStyleProfile `json:"profile"`
}

// UserStyleProfile is the unified style evaluation across all phases.
type UserStyleProfile struct {
	// Identity — populated by Phase 1 (image analysis)
	ColorSeason      string `json:"color_season"`
	ColorSeasonConf  string `json:"color_season_conf"`
	KibbeFamily      string `json:"kibbe_family"`
	KibbeFamilyConf  string `json:"kibbe_family_conf"`
	CurrentArchetype string `json:"current_archetype"`

	// Context — populated by Phase 2 (discovery)
	DesiredArchetype  string   `json:"desired_archetype"`
	Occasion          string   `json:"occasion"`
	DesiredProjection string   `json:"desired_projection"`
	Approach          string   `json:"approach"`
	PainPoints        []string `json:"pain_points"`
	AspirationalRef   string   `json:"aspirational_ref"`
	Constraints       []string `json:"constraints"`

	// Evaluation — populated by Phase 3 (diagnosis)
	Strengths    []string    `json:"strengths"`
	Gaps         []string    `json:"gaps"`
	StyleDistance string     `json:"style_distance"`
	Scores       StyleScores `json:"scores"`
	GapAnalysis  []GapItem   `json:"gap_analysis"`
	OverallScore float64     `json:"overall_score"`
	OverallGrade string      `json:"overall_grade"`
}

// StyleScores holds 6 evaluation dimensions, each scored 1-10.
type StyleScores struct {
	ColorHarmony   int `json:"color_harmony"`
	Fit            int `json:"fit"`
	Proportion     int `json:"proportion"`
	LineHarmony    int `json:"line_harmony"`
	StyleCoherence int `json:"style_coherence"`
	OccasionMatch  int `json:"occasion_match"`
}

// GapItem represents a single dimension gap between current and target scores.
type GapItem struct {
	Dimension  string `json:"dimension"`
	Current    int    `json:"current"`
	Target     int    `json:"target"`
	Gap        int    `json:"gap"`
	Priority   int    `json:"priority"`
	Actionable string `json:"actionable"`
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
