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

// FactMap is a typed map of discovery facts maintained by the LLM.
type FactMap struct {
	Occasion          *string  `json:"occasion,omitempty"`
	Intention         *string  `json:"intention,omitempty"`
	Approach          *string  `json:"approach,omitempty"`
	PainPoints        []string `json:"pain_points,omitempty"`
	AspirationalRef   *string  `json:"aspirational_ref,omitempty"`
	Constraints       []string `json:"constraints,omitempty"`
	Budget            *string  `json:"budget,omitempty"`
	AdditionalContext *string  `json:"additional_context,omitempty"`
}

// MaxAgenticTurns is the safety-net cap; the LLM doesn't know about it.
const MaxAgenticTurns = 12

// CountCoveredFacts returns how many fields in the FactMap are populated.
func CountCoveredFacts(fm *FactMap) int {
	if fm == nil {
		return 0
	}
	count := 0
	if fm.Occasion != nil {
		count++
	}
	if fm.Intention != nil {
		count++
	}
	if fm.Approach != nil {
		count++
	}
	if len(fm.PainPoints) > 0 {
		count++
	}
	if fm.AspirationalRef != nil {
		count++
	}
	if len(fm.Constraints) > 0 {
		count++
	}
	if fm.Budget != nil {
		count++
	}
	if fm.AdditionalContext != nil {
		count++
	}
	return count
}

// ListMissingCriticalFacts returns the names of critical facts not yet populated.
func ListMissingCriticalFacts(fm *FactMap) []string {
	var missing []string
	if fm == nil || fm.Occasion == nil {
		missing = append(missing, "occasion")
	}
	if fm == nil || fm.Intention == nil {
		missing = append(missing, "intention")
	}
	return missing
}

// ConversationMetrics tracks behavioral signals during discovery.
type ConversationMetrics struct {
	ConsecutiveShortResponses int  `json:"consecutive_short_responses"`
	ExitSignalCount           int  `json:"exit_signal_count"`
	LastResponseWordCount     int  `json:"last_response_word_count"`
	RepeatedQuestionDetected  bool `json:"repeated_question_detected"`
}

// SessionState tracks the full state of a conversational session.
type SessionState struct {
	ID                   string              `json:"id"`
	Phase                Phase               `json:"phase"`
	Turn                 int                 `json:"turn"`
	OutfitAnalysis       any                 `json:"outfit_analysis"`  // *vision.OutfitAnalysis stored as any to avoid circular imports
	ImageInsights        any                 `json:"image_insights"`   // *vision.ImageInsights stored as any to avoid circular imports
	Responses            []UserResponse      `json:"responses"`
	AssistantMessages    []string            `json:"assistant_messages"` // what the bot said each turn
	FactMap              *FactMap            `json:"fact_map"`
	Metrics              ConversationMetrics `json:"metrics"`
	LastBotMessage       string              `json:"last_bot_message"`
	CoveredCompensations []string            `json:"covered_compensations"` // blind spots already addressed
	Diagnosis            *StyleDiagnosis     `json:"diagnosis,omitempty"`
	StyleProfile         *UserStyleProfile   `json:"style_profile,omitempty"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
}

// IsCompensationCovered returns true if the blind spot has already been addressed.
func (s *SessionState) IsCompensationCovered(blindSpot string) bool {
	for _, c := range s.CoveredCompensations {
		if c == blindSpot {
			return true
		}
	}
	return false
}

// MarkCompensationCovered records a blind spot as addressed.
func (s *SessionState) MarkCompensationCovered(blindSpot string) {
	if !s.IsCompensationCovered(blindSpot) {
		s.CoveredCompensations = append(s.CoveredCompensations, blindSpot)
	}
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
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Impact      string   `json:"impact"`                        // "alto", "medio", "bajo"
	Effort      string   `json:"effort"`                        // "alto", "medio", "bajo"
	ProductIDs  []string `json:"product_ids,omitempty"`
}
