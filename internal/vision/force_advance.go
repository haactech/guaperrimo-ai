package vision

import "stylerag/internal/session"

// ForceAdvanceResult captures whether Go should override the LLM's decision.
type ForceAdvanceResult struct {
	ShouldForce bool
	Reason      string
}

// ShouldForceAdvance evaluates hard rules that the LLM doesn't respect consistently.
func ShouldForceAdvance(state *session.SessionState, userInput string) ForceAdvanceResult {
	fm := state.FactMap
	if fm == nil {
		return ForceAdvanceResult{false, ""}
	}

	hasCriticals := fm.Occasion != nil && fm.Intention != nil
	hasImportant := fm.Approach != nil || len(fm.PainPoints) > 0

	// Rule 1: Exit signal + criticals covered
	if ContainsExitSignal(userInput) && hasCriticals {
		return ForceAdvanceResult{true, "exit_signal_with_criticals"}
	}

	// Rule 2: 2+ accumulated exit signals + at least occasion
	if state.Metrics.ExitSignalCount >= 2 && fm.Occasion != nil {
		return ForceAdvanceResult{true, "multiple_exit_signals"}
	}

	// Rule 3: 3+ consecutive short responses + criticals covered
	if state.Metrics.ConsecutiveShortResponses >= 3 && hasCriticals {
		return ForceAdvanceResult{true, "consecutive_short_responses"}
	}

	// Rule 4: Criticals + important and 6+ turns
	if hasCriticals && hasImportant && state.Turn >= 6 {
		return ForceAdvanceResult{true, "sufficient_info_plus_turns"}
	}

	// Rule 5: 2+ high-severity image issues + criticals covered
	if insights, ok := state.ImageInsights.(*ImageInsights); ok && insights != nil {
		if insights.HighSeverityCount() >= 2 && hasCriticals {
			return ForceAdvanceResult{true, "high_severity_issues_with_criticals"}
		}
	}

	return ForceAdvanceResult{false, ""}
}
