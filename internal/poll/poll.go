// Package poll lets the user ask people he trusts which of two looks suits
// him better. He shares a link (WhatsApp, usually); whoever opens it picks a
// look, may tick a few reasons and leave a short note, and the result comes
// back to the app. Votes need no account: the link is the only key, it
// expires, and the page never shows his name.
package poll

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"
)

// Limits that keep a shared link small and harmless.
const (
	TTL                = 72 * time.Hour
	MaxPollsPerSession = 5
	MaxVotesPerPoll    = 50
	MaxCommentLen      = 280
	MaxVoterNameLen    = 40
)

var (
	ErrNotFound     = errors.New("poll not found")
	ErrClosed       = errors.New("poll closed")
	ErrTooManyVotes = errors.New("poll has too many votes")
)

// Option is one of the two looks being compared.
type Option struct {
	LookID   string `json:"look_id"`
	Name     string `json:"name"`
	ImageKey string `json:"image_key"` // object key of the rendered look
}

// Poll is one "which one?" question about two looks.
type Poll struct {
	ID        string    `json:"id"` // public token in the shared link
	SessionID string    `json:"session_id"`
	Occasion  string    `json:"occasion,omitempty"` // shown to voters: "para una boda de día"
	Options   [2]Option `json:"options"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Open reports whether the poll still takes votes.
func (p *Poll) Open(now time.Time) bool { return now.Before(p.ExpiresAt) }

// Vote is one person's answer. A voter (one browser) has one vote per poll;
// voting again replaces it.
type Vote struct {
	PollID    string    `json:"poll_id"`
	VoterID   string    `json:"voter_id"`
	Choice    int       `json:"choice"` // index into Poll.Options
	Reasons   []string  `json:"reasons,omitempty"`
	Comment   string    `json:"comment,omitempty"`
	VoterName string    `json:"voter_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Reason is a one-tap explanation a voter can add to the look he picked.
type Reason struct {
	ID    string
	Label string
}

// Reasons are fixed so the app can count them; there is no free-text reason
// besides the optional comment.
var Reasons = []Reason{
	{"color", "El color le queda mejor"},
	{"occasion", "Va mejor con la ocasión"},
	{"fit", "Le queda mejor el corte"},
	{"elegant", "Se ve más elegante"},
	{"modern", "Se ve más actual"},
	{"himself", "Se ve más él"},
}

// ReasonLabel returns the Spanish label of a reason id, or "" if unknown.
func ReasonLabel(id string) string {
	for _, r := range Reasons {
		if r.ID == id {
			return r.Label
		}
	}
	return ""
}

// Store persists polls and their votes.
type Store interface {
	Create(ctx context.Context, p *Poll) error
	Get(ctx context.Context, id string) (*Poll, error)
	BySession(ctx context.Context, sessionID string) ([]Poll, error)
	// Vote inserts or replaces the voter's vote. It returns ErrTooManyVotes
	// when a new voter would exceed MaxVotesPerPoll.
	Vote(ctx context.Context, v Vote) error
	Votes(ctx context.Context, pollID string) ([]Vote, error)
}

// NewToken returns an unguessable id for a poll or a voter.
func NewToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Summary counts the votes of a poll per option.
type Summary struct {
	Total   int
	Votes   [2]int
	Reasons [2]map[string]int // reason id -> count, per option
}

// Summarize counts votes, ignoring reasons that are not in Reasons.
func Summarize(votes []Vote) Summary {
	s := Summary{Reasons: [2]map[string]int{{}, {}}}
	for _, v := range votes {
		if v.Choice < 0 || v.Choice > 1 {
			continue
		}
		s.Total++
		s.Votes[v.Choice]++
		for _, r := range v.Reasons {
			if ReasonLabel(r) != "" {
				s.Reasons[v.Choice][r]++
			}
		}
	}
	return s
}
