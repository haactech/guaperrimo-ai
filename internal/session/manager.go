package session

import (
	"context"
	"time"
)

// StyleProfile represents the user's style preferences during a session
type StyleProfile struct {
	ID                  string            `json:"id"`
	CurrentStyle        []string          `json:"current_style"`
	TargetStyle         []string          `json:"target_style"`
	ColorPreferences    ColorPreferences  `json:"color_preferences"`
	FitPreference       string            `json:"fit_preference"`
	Occasions           []string          `json:"occasions"`
	Budget              Budget            `json:"budget"`
	Restrictions        []string          `json:"restrictions"`
	TransformationLevel string            `json:"transformation_level"` // gradual, moderate, dramatic
	ConversationHistory []ConversationMsg `json:"conversation_history"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

type ColorPreferences struct {
	Likes    []string `json:"likes"`
	Dislikes []string `json:"dislikes"`
}

type Budget struct {
	Min      float64 `json:"min"`
	Max      float64 `json:"max"`
	Currency string  `json:"currency"`
}

type ConversationMsg struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Manager defines the interface for session management
type Manager interface {
	// Create initializes a new session and returns its ID
	Create(ctx context.Context) (string, error)

	// Get retrieves a session by ID
	Get(ctx context.Context, sessionID string) (*StyleProfile, error)

	// Update saves changes to a session
	Update(ctx context.Context, sessionID string, profile *StyleProfile) error

	// AddMessage appends a message to the conversation history
	AddMessage(ctx context.Context, sessionID string, msg ConversationMsg) error

	// Delete removes a session
	Delete(ctx context.Context, sessionID string) error
}
