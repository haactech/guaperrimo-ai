package voice

import (
	"context"
)

// TranscriptionResult represents the result of speech-to-text
type TranscriptionResult struct {
	Text       string    `json:"text"`
	Confidence float64   `json:"confidence"`
	Segments   []Segment `json:"segments"`
}

// Segment represents a timed segment of transcription
type Segment struct {
	Text      string  `json:"text"`
	StartTime float64 `json:"start_time"`
	EndTime   float64 `json:"end_time"`
}

// Transcriber defines the interface for speech-to-text (Avalon API)
type Transcriber interface {
	// Transcribe converts audio data to text
	Transcribe(ctx context.Context, audioData []byte, language string) (*TranscriptionResult, error)
}
