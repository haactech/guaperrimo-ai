package agent

import "context"

// Progress stages reported while a turn runs. The set is closed: clients map
// each one to a message.
const (
	StageAnalyzingPhoto    = "analyzing_photo"
	StageFindingStores     = "finding_stores"
	StageGeocoding         = "geocoding"
	StageThinking          = "thinking"
	StageSearchingProducts = "searching_products"
	StageProposingPreview  = "proposing_preview"
	StageComposing         = "composing"
)

type progressKey struct{}

// ProgressFunc receives the stage the turn just entered and an optional detail.
// It may be called from several goroutines at once.
type ProgressFunc func(stage, detail string)

// WithProgress returns a context whose turn reports its progress to fn.
func WithProgress(ctx context.Context, fn func(stage, detail string)) context.Context {
	if fn == nil {
		return ctx
	}
	return context.WithValue(ctx, progressKey{}, ProgressFunc(fn))
}

// report is a no-op when ctx carries no reporter.
func report(ctx context.Context, stage, detail string) {
	if fn, ok := ctx.Value(progressKey{}).(ProgressFunc); ok {
		fn(stage, detail)
	}
}
