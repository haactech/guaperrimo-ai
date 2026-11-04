package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"stylerag/internal/poll"
	"stylerag/internal/session"
	"stylerag/internal/storage"
)

type noImages struct{}

func (noImages) Upload(context.Context, storage.UploadInput) (*storage.UploadOutput, error) {
	return &storage.UploadOutput{}, nil
}
func (noImages) Download(context.Context, string) ([]byte, error)   { return nil, nil }
func (noImages) ListKeys(context.Context, string) ([]string, error) { return nil, nil }
func (noImages) URL(key string) string                              { return key }

// pollServer has a session with two rendered looks and a third one still
// generating. The API key is on, as in production.
func pollServer(t *testing.T) (*httptest.Server, poll.Store, *storage.SignedURLs) {
	t.Helper()
	store := session.NewMemoryStore(time.Hour)
	signed := storage.WithSignedURLs(noImages{}, "http://api.test", "secret", time.Hour)
	st := session.New("sess_123")
	st.Profile.Occasion = "Boda de día"
	st.Looks = []session.Look{{ID: "look_1", Name: "Clásico cálido"}, {ID: "look_2", Name: "Relajado"}, {ID: "look_3", Name: "Otro"}, {ID: "look_x", Name: "Ajeno"}}
	st.LookResults = []session.LookResult{
		{LookID: "look_1", Status: session.LookStatusReady, FinalImageURL: signed.URL("sessions/sess_123/render_a.jpg")},
		{LookID: "look_2", Status: session.LookStatusReady, FinalImageURL: signed.URL("sessions/sess_123/render_b.jpg")},
		{LookID: "look_3", Status: session.LookStatusGenerating},
		{LookID: "look_x", Status: session.LookStatusReady, FinalImageURL: signed.URL("sessions/other/render.jpg")},
	}
	if err := store.Save(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	polls := poll.NewMemoryStore()
	srv := httptest.NewServer(NewRouter(Deps{
		APIKey: "k", Store: store, Signed: signed,
		Polls: &PollDeps{Polls: polls, Store: store, Signed: signed, BaseURL: "https://hechura.test/"},
	}))
	t.Cleanup(srv.Close)
	return srv, polls, signed
}

func apiCall(t *testing.T, method, url, body string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewBufferString(body))
	req.Header.Set("X-API-Key", "k")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func createPoll(t *testing.T, srv *httptest.Server) PollDTO {
	t.Helper()
	code, body := apiCall(t, "POST", srv.URL+"/session/sess_123/polls", `{"look_ids":["look_1","look_2"]}`)
	if code != http.StatusCreated {
		t.Fatalf("create → %d %s", code, body)
	}
	var p PollDTO
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreatePollReturnsShareLink(t *testing.T) {
	srv, _, _ := pollServer(t)
	p := createPoll(t, srv)
	if !strings.HasPrefix(p.URL, "https://hechura.test/v/") || !p.Open || len(p.Options) != 2 || p.Options[0].Name != "Clásico cálido" {
		t.Fatalf("unexpected poll: %+v", p)
	}
	if p.ExpiresAt.Sub(p.CreatedAt) != poll.TTL {
		t.Fatalf("poll must last %v, got %v", poll.TTL, p.ExpiresAt.Sub(p.CreatedAt))
	}
	for _, body := range []string{
		`{"look_ids":["look_1"]}`,
		`{"look_ids":["look_1","look_1"]}`,
	} {
		if code, _ := apiCall(t, "POST", srv.URL+"/session/sess_123/polls", body); code != 400 {
			t.Errorf("%s → %d, want 400", body, code)
		}
	}
	// A look still rendering, or an image outside this session, cannot be shared.
	for _, body := range []string{`{"look_ids":["look_1","look_3"]}`, `{"look_ids":["look_1","look_x"]}`} {
		if code, _ := apiCall(t, "POST", srv.URL+"/session/sess_123/polls", body); code != 409 {
			t.Errorf("%s → %d, want 409", body, code)
		}
	}
	if code, _ := apiCall(t, "POST", srv.URL+"/session/nope_404/polls", `{"look_ids":["look_1","look_2"]}`); code != 404 {
		t.Errorf("unknown session → %d, want 404", code)
	}
}

func TestPollsPerSessionAreCapped(t *testing.T) {
	srv, _, _ := pollServer(t)
	for i := 0; i < poll.MaxPollsPerSession; i++ {
		createPoll(t, srv)
	}
	if code, _ := apiCall(t, "POST", srv.URL+"/session/sess_123/polls", `{"look_ids":["look_1","look_2"]}`); code != 429 {
		t.Fatalf("poll over the cap → %d, want 429", code)
	}
}

// voter is a browser: it keeps the cookie that makes its vote replaceable.
func voter(t *testing.T) *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func vote(t *testing.T, c *http.Client, pageURL string, form url.Values) (int, string) {
	t.Helper()
	resp, err := c.PostForm(pageURL, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestVotePageNeedsNoKeyAndCountsVotes(t *testing.T) {
	srv, _, _ := pollServer(t)
	p := createPoll(t, srv)
	page := srv.URL + "/v/" + p.ID

	ana := voter(t)
	resp, err := ana.Get(page)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "¿Con cuál se ve mejor?") || !strings.Contains(string(body), "Boda de día") {
		t.Fatalf("page → %d\n%.400s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Robots-Tag") == "" || resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("the page must stay out of search engines and referrers")
	}
	if !strings.Contains(string(body), "/images/sessions/sess_123/render_a.jpg?exp=") {
		t.Fatal("the page must show the look through a signed image link")
	}

	code, html := vote(t, ana, page, url.Values{"choice": {"a"}, "reason": {"color", "color", "bogus"}, "comment": {"  El café\nte queda <b>increíble</b>  "}, "name": {"Ana"}})
	if code != 200 || !strings.Contains(html, "¡Gracias!") || !strings.Contains(html, "Tu voto es el primero.") {
		t.Fatalf("after voting → %d\n%.600s", code, html)
	}
	if strings.Contains(html, "<b>increíble") {
		t.Fatal("comments must be escaped")
	}
	// Ana changes her mind: her vote is replaced, not added.
	vote(t, ana, page, url.Values{"choice": {"b"}, "name": {"Ana"}}) // the form keeps her name
	beto := voter(t)
	_, html = vote(t, beto, page, url.Values{"choice": {"b"}, "reason": {"fit"}})
	if !strings.Contains(html, "Las 2 personas que han votado eligieron el mismo look.") {
		t.Fatalf("expected both votes on B\n%.800s", html)
	}

	code, raw := apiCall(t, "GET", srv.URL+"/session/sess_123/polls", "")
	var list struct{ Polls []PollDTO }
	if err := json.Unmarshal(raw, &list); err != nil || code != 200 || len(list.Polls) != 1 {
		t.Fatalf("list → %d %s", code, raw)
	}
	got := list.Polls[0]
	if got.TotalVotes != 2 || got.Options[0].VoteCount != 0 || got.Options[1].VoteCount != 2 {
		t.Fatalf("counts: %+v", got)
	}
	if len(got.Options[1].Reasons) != 1 || got.Options[1].Reasons[0].Label != "Le queda mejor el corte" {
		t.Fatalf("reasons: %+v", got.Options[1].Reasons)
	}
	// Ana's second vote has no comment: the newer vote replaces the older.
	if len(got.Votes) != 2 || got.Votes[1].VoterName != "Ana" || got.Votes[1].Comment != "" {
		t.Fatalf("votes: %+v", got.Votes)
	}
}

func TestVoteCleansCommentAndRequiresChoice(t *testing.T) {
	srv, polls, _ := pollServer(t)
	p := createPoll(t, srv)
	page := srv.URL + "/v/" + p.ID
	code, html := vote(t, voter(t), page, url.Values{"comment": {"sin elegir"}})
	if code != 400 || !strings.Contains(html, "Elige uno de los dos looks.") || !strings.Contains(html, "sin elegir") {
		t.Fatalf("missing choice → %d, the form must come back with the comment\n%.500s", code, html)
	}
	vote(t, voter(t), page, url.Values{"choice": {"a"}, "comment": {"  El café\nte queda\tincreíble  " + strings.Repeat("!", 400)}})
	votes, _ := polls.Votes(context.Background(), p.ID)
	if len(votes) != 1 || !strings.HasPrefix(votes[0].Comment, "El café te queda increíble !") || len([]rune(votes[0].Comment)) != poll.MaxCommentLen {
		t.Fatalf("comment not cleaned: %q", votes[0].Comment)
	}
}

func TestClosedAndUnknownPolls(t *testing.T) {
	srv, polls, _ := pollServer(t)
	old := &poll.Poll{ID: poll.NewToken(), SessionID: "sess_123", CreatedAt: time.Now().Add(-80 * time.Hour), ExpiresAt: time.Now().Add(-8 * time.Hour)}
	if err := polls.Create(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	resp, _ := http.Get(srv.URL + "/v/" + old.ID)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone || !strings.Contains(string(body), "ya cerró") || strings.Contains(string(body), "/images/") {
		t.Fatalf("closed poll → %d, must show no photos\n%.300s", resp.StatusCode, body)
	}
	if code, _ := vote(t, voter(t), srv.URL+"/v/"+old.ID, url.Values{"choice": {"a"}}); code != http.StatusGone {
		t.Fatalf("vote on a closed poll → %d, want 410", code)
	}
	for _, path := range []string{"/v/AAAAAAAAAAAAAAAAAAAAAA", "/v/short", "/v/../session"} {
		if resp, err := http.Get(srv.URL + path); err == nil {
			resp.Body.Close()
			if resp.StatusCode != 404 && resp.StatusCode != 401 {
				t.Errorf("%s → %d, want 404", path, resp.StatusCode)
			}
		}
	}
	// The app's endpoints still need the key.
	if resp, _ := http.Get(srv.URL + "/session/sess_123/polls"); resp.StatusCode != 401 {
		t.Fatalf("list without key → %d, want 401", resp.StatusCode)
	}
}

func TestVotesPerPollAreCapped(t *testing.T) {
	polls := poll.NewMemoryStore()
	ctx := context.Background()
	p := &poll.Poll{ID: "p", ExpiresAt: time.Now().Add(time.Hour)}
	polls.Create(ctx, p)
	for i := 0; i < poll.MaxVotesPerPoll; i++ {
		if err := polls.Vote(ctx, poll.Vote{PollID: "p", VoterID: poll.NewToken()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := polls.Vote(ctx, poll.Vote{PollID: "p", VoterID: "new"}); err != poll.ErrTooManyVotes {
		t.Fatalf("over the cap: %v", err)
	}
	votes, _ := polls.Votes(ctx, "p")
	if err := polls.Vote(ctx, poll.Vote{PollID: "p", VoterID: votes[0].VoterID, Choice: 1}); err != nil {
		t.Fatalf("an existing voter can still change his vote: %v", err)
	}
}
