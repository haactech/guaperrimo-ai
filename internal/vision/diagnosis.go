package vision

import (
	"context"
	"encoding/json"
	"fmt"

	"stylerag/internal/llm"
	"stylerag/internal/session"
)

// DiagnosisGenerator runs the Phase 3 diagnosis LLM call.
type DiagnosisGenerator struct {
	router *llm.Router
}

func NewDiagnosisGenerator(router *llm.Router) *DiagnosisGenerator {
	return &DiagnosisGenerator{router: router}
}

func (d *DiagnosisGenerator) Generate(ctx context.Context, analysis *OutfitAnalysis, responses []session.UserResponse, profile *session.UserStyleProfile) (*session.StyleDiagnosis, error) {
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		return nil, fmt.Errorf("diagnosis: marshal analysis: %w", err)
	}

	responsesJSON, err := json.Marshal(responses)
	if err != nil {
		return nil, fmt.Errorf("diagnosis: marshal responses: %w", err)
	}

	profileJSON, err := json.Marshal(profile)
	if err != nil {
		return nil, fmt.Errorf("diagnosis: marshal profile: %w", err)
	}

	prompt := fmt.Sprintf(diagnosisPrompt, string(analysisJSON), string(responsesJSON), string(profileJSON))

	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: prompt},
		},
		MaxTokens:   8192,
		Temperature: 0.3,
	}

	resp, err := d.router.Complete(ctx, llm.TurnTypeDiagnosis, req)
	if err != nil {
		return nil, fmt.Errorf("diagnosis: LLM call failed: %w", err)
	}

	var diagnosis session.StyleDiagnosis
	if err := json.Unmarshal([]byte(cleanJSON(resp.Content)), &diagnosis); err != nil {
		return nil, fmt.Errorf("diagnosis: failed to parse response: %w\nraw: %s", err, resp.Content)
	}

	// Post-processing: validate and compute scores
	diagnosis.Profile.Scores = ValidateScores(diagnosis.Profile.Scores)
	overallScore, overallGrade := CalculateOverallScore(diagnosis.Profile.Scores)
	diagnosis.Profile.OverallScore = overallScore
	diagnosis.Profile.OverallGrade = overallGrade

	// Compute Gap = Target - Current for each gap item
	for i := range diagnosis.Profile.GapAnalysis {
		diagnosis.Profile.GapAnalysis[i].Gap = diagnosis.Profile.GapAnalysis[i].Target - diagnosis.Profile.GapAnalysis[i].Current
	}

	// Merge identity fields from Phase 1 (already in the partial profile)
	if profile != nil {
		diagnosis.Profile.ColorSeason = profile.ColorSeason
		diagnosis.Profile.ColorSeasonConf = profile.ColorSeasonConf
		diagnosis.Profile.KibbeFamily = profile.KibbeFamily
		diagnosis.Profile.KibbeFamilyConf = profile.KibbeFamilyConf
		diagnosis.Profile.CurrentArchetype = profile.CurrentArchetype

		// Merge context fields from Phase 2
		diagnosis.Profile.Occasion = profile.Occasion
		diagnosis.Profile.DesiredProjection = profile.DesiredProjection
		diagnosis.Profile.Approach = profile.Approach
		diagnosis.Profile.PainPoints = profile.PainPoints
		diagnosis.Profile.AspirationalRef = profile.AspirationalRef
		diagnosis.Profile.Constraints = profile.Constraints
	}

	return &diagnosis, nil
}
