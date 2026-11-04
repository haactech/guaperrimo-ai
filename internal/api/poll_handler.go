package api

import (
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"stylerag/internal/poll"
	"stylerag/internal/session"
	"stylerag/internal/storage"
)

// PollDeps wires "ask someone": the app creates a poll between two rendered
// looks and shares its link; voters answer on a public page, /v/{token}.
type PollDeps struct {
	Polls   poll.Store
	Store   session.Store
	Signed  *storage.SignedURLs
	BaseURL string // public base of the shared links
}

var pollTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,40}$`)

// CreatePollRequest is the input for POST /session/{id}/polls.
type CreatePollRequest struct {
	LookIDs []string `json:"look_ids"` // exactly two rendered looks
}

// PollDTO is a poll with its results, for the app.
type PollDTO struct {
	ID         string          `json:"id"`
	URL        string          `json:"url"`
	Open       bool            `json:"open"`
	CreatedAt  time.Time       `json:"created_at"`
	ExpiresAt  time.Time       `json:"expires_at"`
	TotalVotes int             `json:"total_votes"`
	Options    []PollOptionDTO `json:"options"`
	Votes      []PollVoteDTO   `json:"votes"` // newest first
}

// PollOptionDTO is one look of the poll and how many picked it.
type PollOptionDTO struct {
	LookID    string          `json:"look_id"`
	Name      string          `json:"name"`
	ImageURL  string          `json:"image_url"`
	VoteCount int             `json:"vote_count"`
	Reasons   []PollReasonDTO `json:"reasons"` // most chosen first
}

// PollReasonDTO counts one reason given for a look.
type PollReasonDTO struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// PollVoteDTO is one answer as the user sees it; voters stay anonymous
// unless they typed a name.
type PollVoteDTO struct {
	Choice    int       `json:"choice"`
	VoterName string    `json:"voter_name,omitempty"`
	Comment   string    `json:"comment,omitempty"`
	Reasons   []string  `json:"reasons"` // labels
	CreatedAt time.Time `json:"created_at"`
}

func createPollHandler(d *PollDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if !sessionIDPattern.MatchString(sessionID) {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		var req CreatePollRequest
		if err := decodeJSON(w, r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		if len(req.LookIDs) != 2 || req.LookIDs[0] == req.LookIDs[1] {
			writeError(w, http.StatusBadRequest, "look_ids must name two different looks")
			return
		}
		st, err := d.Store.Get(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusNotFound, "session not found or expired")
			return
		}
		existing, err := d.Polls.BySession(r.Context(), sessionID)
		if err != nil {
			slog.ErrorContext(r.Context(), "polls: list failed", "session_id", sessionID, "error", err)
			writeError(w, http.StatusInternalServerError, "could not create poll")
			return
		}
		if len(existing) >= poll.MaxPollsPerSession {
			writeError(w, http.StatusTooManyRequests, "too many polls for this session")
			return
		}
		var opts [2]poll.Option
		for i, id := range req.LookIDs {
			opt, ok := pollOption(st, id)
			if !ok {
				writeError(w, http.StatusConflict, "look "+id+" is not rendered yet")
				return
			}
			opts[i] = opt
		}
		now := time.Now()
		p := &poll.Poll{
			ID: poll.NewToken(), SessionID: sessionID, Occasion: strings.TrimSpace(st.Profile.Occasion),
			Options: opts, CreatedAt: now, ExpiresAt: now.Add(poll.TTL),
		}
		if err := d.Polls.Create(r.Context(), p); err != nil {
			slog.ErrorContext(r.Context(), "polls: create failed", "session_id", sessionID, "error", err)
			writeError(w, http.StatusInternalServerError, "could not create poll")
			return
		}
		slog.InfoContext(r.Context(), "polls: created", "session_id", sessionID, "poll_id", p.ID)
		writeJSON(w, http.StatusCreated, d.pollDTO(p, nil))
	}
}

// pollOption finds a rendered look of this session. Only images under the
// session's own prefix can be shared.
func pollOption(st *session.State, lookID string) (poll.Option, bool) {
	name := ""
	for _, l := range st.Looks {
		if l.ID == lookID {
			name = l.Name
		}
	}
	for _, lr := range st.LookResults {
		if lr.LookID != lookID || lr.Status != session.LookStatusReady {
			continue
		}
		key := storage.KeyFromURL(lr.FinalImageURL)
		if !strings.HasPrefix(key, "sessions/"+st.ID+"/") {
			return poll.Option{}, false
		}
		return poll.Option{LookID: lookID, Name: name, ImageKey: key}, true
	}
	return poll.Option{}, false
}

func listPollsHandler(d *PollDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")
		if !sessionIDPattern.MatchString(sessionID) {
			writeError(w, http.StatusBadRequest, "invalid session id")
			return
		}
		polls, err := d.Polls.BySession(r.Context(), sessionID)
		if err != nil {
			slog.ErrorContext(r.Context(), "polls: list failed", "session_id", sessionID, "error", err)
			writeError(w, http.StatusInternalServerError, "could not load polls")
			return
		}
		out := make([]PollDTO, 0, len(polls))
		for i := range polls {
			votes, err := d.Polls.Votes(r.Context(), polls[i].ID)
			if err != nil {
				slog.ErrorContext(r.Context(), "polls: votes failed", "poll_id", polls[i].ID, "error", err)
				writeError(w, http.StatusInternalServerError, "could not load polls")
				return
			}
			out = append(out, d.pollDTO(&polls[i], votes))
		}
		writeJSON(w, http.StatusOK, map[string]any{"polls": out})
	}
}

func (d *PollDeps) shareURL(id string) string {
	return strings.TrimRight(d.BaseURL, "/") + "/v/" + id
}

func (d *PollDeps) pollDTO(p *poll.Poll, votes []poll.Vote) PollDTO {
	sum := poll.Summarize(votes)
	dto := PollDTO{
		ID: p.ID, URL: d.shareURL(p.ID), Open: p.Open(time.Now()), CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt,
		TotalVotes: sum.Total, Options: []PollOptionDTO{}, Votes: []PollVoteDTO{},
	}
	for i, o := range p.Options {
		opt := PollOptionDTO{LookID: o.LookID, Name: o.Name, ImageURL: d.Signed.URL(o.ImageKey), VoteCount: sum.Votes[i], Reasons: []PollReasonDTO{}}
		for _, r := range poll.Reasons { // fixed order, then most chosen first
			if n := sum.Reasons[i][r.ID]; n > 0 {
				opt.Reasons = append(opt.Reasons, PollReasonDTO{ID: r.ID, Label: r.Label, Count: n})
			}
		}
		sortReasons(opt.Reasons)
		dto.Options = append(dto.Options, opt)
	}
	for i := len(votes) - 1; i >= 0; i-- {
		v := votes[i]
		labels := []string{}
		for _, r := range v.Reasons {
			if l := poll.ReasonLabel(r); l != "" {
				labels = append(labels, l)
			}
		}
		dto.Votes = append(dto.Votes, PollVoteDTO{Choice: v.Choice, VoterName: v.VoterName, Comment: v.Comment, Reasons: labels, CreatedAt: v.CreatedAt})
	}
	return dto
}

func sortReasons(rs []PollReasonDTO) {
	for i := 1; i < len(rs); i++ { // stable insertion sort: a handful of items
		for j := i; j > 0 && rs[j].Count > rs[j-1].Count; j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

// ── Public vote page ────────────────────────────────────────────────────────

//go:embed templates/vote.html
var votePageHTML string

var votePage = template.Must(template.New("vote").Parse(votePageHTML))

type votePageData struct {
	State    string // form | thanks | closed | missing
	Action   string
	Occasion string
	Options  []voteOptionView
	Reasons  []voteReasonView
	Comment  string
	Name     string
	Total    int
	Choice   int    // the voter's choice, -1 if none
	Same     int    // voters who picked what he picked, himself included
	Verdict  string // how his pick compares with everyone else's
	Expires  string
	Error    string
}

type voteOptionView struct {
	Index    int
	Letter   string
	Value    string
	Name     string
	ImageURL string
	Votes    int
	Percent  int
	Checked  bool
}

type voteReasonView struct {
	ID      string
	Label   string
	Checked bool
}

func voteCookieName(pollID string) string { return "hv_" + pollID[:8] }

func votePageHandler(d *PollDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := d.loadPoll(w, r)
		if !ok {
			return
		}
		votes, err := d.Polls.Votes(r.Context(), p.ID)
		if err != nil {
			slog.ErrorContext(r.Context(), "polls: votes failed", "poll_id", p.ID, "error", err)
			http.Error(w, "No pudimos cargar la votación. Intenta de nuevo.", http.StatusInternalServerError)
			return
		}
		var mine *poll.Vote
		if c, err := r.Cookie(voteCookieName(p.ID)); err == nil {
			for i := range votes {
				if votes[i].VoterID == c.Value {
					mine = &votes[i]
				}
			}
		}
		state := "form"
		if mine != nil && r.URL.Query().Get("cambiar") == "" {
			state = "thanks"
		}
		renderVotePage(w, http.StatusOK, d.pageData(p, votes, mine, state, ""))
	}
}

func voteSubmitHandler(d *PollDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := d.loadPoll(w, r)
		if !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "No pudimos leer tu voto. Intenta de nuevo.", http.StatusBadRequest)
			return
		}
		v := poll.Vote{PollID: p.ID, Choice: -1, CreatedAt: time.Now()}
		switch r.PostForm.Get("choice") {
		case "a":
			v.Choice = 0
		case "b":
			v.Choice = 1
		}
		seen := map[string]bool{}
		for _, id := range r.PostForm["reason"] {
			if poll.ReasonLabel(id) != "" && !seen[id] {
				seen[id] = true
				v.Reasons = append(v.Reasons, id)
			}
		}
		v.Comment = cleanText(r.PostForm.Get("comment"), poll.MaxCommentLen)
		v.VoterName = cleanText(r.PostForm.Get("name"), poll.MaxVoterNameLen)
		if v.Choice < 0 {
			votes, _ := d.Polls.Votes(r.Context(), p.ID)
			data := d.pageData(p, votes, &v, "form", "Elige uno de los dos looks.")
			renderVotePage(w, http.StatusBadRequest, data)
			return
		}

		// One vote per browser: the cookie names the voter; voting again
		// replaces the earlier vote.
		if c, err := r.Cookie(voteCookieName(p.ID)); err == nil && pollTokenPattern.MatchString(c.Value) {
			v.VoterID = c.Value
		} else {
			v.VoterID = poll.NewToken()
		}
		if err := d.Polls.Vote(r.Context(), v); err != nil {
			if errors.Is(err, poll.ErrTooManyVotes) {
				http.Error(w, "Esta votación ya recibió todas las respuestas que admite. ¡Gracias!", http.StatusConflict)
				return
			}
			slog.ErrorContext(r.Context(), "polls: vote failed", "poll_id", p.ID, "error", err)
			http.Error(w, "No pudimos guardar tu voto. Intenta de nuevo.", http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name: voteCookieName(p.ID), Value: v.VoterID, Path: "/v/" + p.ID, Expires: p.ExpiresAt,
			HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
		})
		slog.InfoContext(r.Context(), "polls: vote", "poll_id", p.ID, "choice", v.Choice, "reasons", len(v.Reasons), "comment", v.Comment != "")
		http.Redirect(w, r, "/v/"+p.ID, http.StatusSeeOther)
	}
}

// loadPoll answers the request itself when the poll is missing or closed.
func (d *PollDeps) loadPoll(w http.ResponseWriter, r *http.Request) (*poll.Poll, bool) {
	token := r.PathValue("token")
	if !pollTokenPattern.MatchString(token) {
		renderVotePage(w, http.StatusNotFound, votePageData{State: "missing"})
		return nil, false
	}
	p, err := d.Polls.Get(r.Context(), token)
	if err != nil {
		if !errors.Is(err, poll.ErrNotFound) {
			slog.ErrorContext(r.Context(), "polls: get failed", "error", err)
		}
		renderVotePage(w, http.StatusNotFound, votePageData{State: "missing"})
		return nil, false
	}
	if !p.Open(time.Now()) {
		renderVotePage(w, http.StatusGone, votePageData{State: "closed"})
		return nil, false
	}
	return p, true
}

func (d *PollDeps) pageData(p *poll.Poll, votes []poll.Vote, mine *poll.Vote, state, errMsg string) votePageData {
	sum := poll.Summarize(votes)
	data := votePageData{
		State: state, Action: "/v/" + p.ID, Occasion: p.Occasion, Total: sum.Total, Choice: -1,
		Expires: spanishDate(p.ExpiresAt), Error: errMsg,
	}
	picked := map[string]bool{}
	if mine != nil {
		data.Choice = mine.Choice
		data.Comment, data.Name = mine.Comment, mine.VoterName
		for _, r := range mine.Reasons {
			picked[r] = true
		}
		if mine.Choice >= 0 {
			data.Same = sum.Votes[mine.Choice]
			data.Verdict = verdict(data.Same, sum.Total)
		}
	}
	for i, o := range p.Options {
		pct := 0
		if sum.Total > 0 {
			pct = sum.Votes[i] * 100 / sum.Total
		}
		data.Options = append(data.Options, voteOptionView{
			Index: i, Letter: string(rune('A' + i)), Value: string(rune('a' + i)), Name: o.Name,
			ImageURL: d.Signed.URLUntil(o.ImageKey, p.ExpiresAt), Votes: sum.Votes[i], Percent: pct,
			Checked: mine != nil && mine.Choice == i,
		})
	}
	for _, r := range poll.Reasons {
		data.Reasons = append(data.Reasons, voteReasonView{ID: r.ID, Label: r.Label, Checked: picked[r.ID]})
	}
	return data
}

func verdict(same, total int) string {
	switch {
	case total <= 1:
		return "Tu voto es el primero."
	case same == total:
		return fmt.Sprintf("Las %d personas que han votado eligieron el mismo look.", total)
	default:
		return fmt.Sprintf("%d de %d personas eligieron el mismo look que tú.", same, total)
	}
}

func renderVotePage(w http.ResponseWriter, status int, data votePageData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	// The page shows someone's photo: keep it out of search engines and
	// referrers, and let it load nothing but its own images.
	h.Set("X-Robots-Tag", "noindex, nofollow")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if err := votePage.Execute(w, data); err != nil {
		slog.Error("polls: render page failed", "error", err)
	}
}

// cleanText trims, drops control characters and caps the length in runes.
func cleanText(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

var spanishMonths = [...]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}

var mexicoCity = func() *time.Location {
	if loc, err := time.LoadLocation("America/Mexico_City"); err == nil {
		return loc
	}
	return time.FixedZone("CST", -6*60*60)
}()

func spanishDate(t time.Time) string {
	t = t.In(mexicoCity)
	return t.Format("2") + " de " + spanishMonths[t.Month()-1]
}
