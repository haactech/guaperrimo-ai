package tryon

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/storage"
)

type memImages struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memImages) Upload(_ context.Context, in storage.UploadInput) (*storage.UploadOutput, error) {
	data, err := io.ReadAll(in.Body)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.objects[in.Key] = data
	m.mu.Unlock()
	return &storage.UploadOutput{URL: m.URL(in.Key)}, nil
}

func (m *memImages) Download(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.objects[key]
	if !ok {
		return nil, errors.New("not found: " + key)
	}
	return d, nil
}

func (m *memImages) ListKeys(_ context.Context, prefix string) ([]string, error) { return nil, nil }
func (m *memImages) URL(key string) string                                       { return "https://img.test/" + key }

type fakeVTON struct {
	mu    sync.Mutex
	calls []VTONRequest
	fail  map[string]bool
	delay time.Duration
}

func (f *fakeVTON) Name() string { return "fake" }

func (f *fakeVTON) Generate(_ context.Context, req VTONRequest) (*VTONResult, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	time.Sleep(f.delay)
	if f.fail[req.GarmentDesc] {
		return nil, errors.New("vton exploded")
	}
	out := string(req.PersonImage) + "+" + string(req.GarmentImage)
	return &VTONResult{ImageBytes: []byte(out), MimeType: "image/jpeg", GenerationMs: 1, ProviderName: "fake"}, nil
}

func (f *fakeVTON) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type fixture struct {
	store  *session.MemoryStore
	images *memImages
	vton   *fakeVTON
	r      *Renderer
	srv    *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("garment-" + strings.TrimPrefix(r.URL.Path, "/img/")))
	}))
	t.Cleanup(srv.Close)

	store := session.NewMemoryStore(time.Hour)
	t.Cleanup(store.Stop)
	images := &memImages{objects: map[string][]byte{"sessions/r1/welcome.jpg": []byte("person")}}
	vton := &fakeVTON{fail: map[string]bool{}}

	st := session.New("r1")
	st.ImageKey = "sessions/r1/welcome.jpg"
	for id, title := range map[string]string{"s1": "Camisa", "s2": "Polo", "p1": "Pantalón", "p2": "Chino", "z1": "Zapatos", "z2": "Tenis"} {
		st.Products[id] = shopping.Product{ID: id, Title: title, Price: 100, Thumbnail: srv.URL + "/img/" + id}
	}
	if err := store.Save(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	r := NewRenderer(store, images, vton, nil, srv.Client(), 3, 2, 5*time.Second)
	return &fixture{store: store, images: images, vton: vton, r: r, srv: srv}
}

func combo(ids ...string) []session.ComboPiece {
	slots := []string{"upper_body", "lower_body", "footwear"}
	out := make([]session.ComboPiece, len(ids))
	for i, id := range ids {
		out[i] = session.ComboPiece{Slot: slots[i], ProductID: id}
	}
	return out
}

func TestRenderChainsAndReusesPrefixes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	rd, err := f.r.Render(ctx, "r1", combo("s1", "p1", "z1"))
	if err != nil || rd.Status != session.LookStatusReady || rd.Key != "s1|p1|z1" || rd.ImageURL == "" {
		t.Fatalf("render: %+v %v", rd, err)
	}
	if f.vton.count() != 3 {
		t.Fatalf("expected 3 vton calls, got %d", f.vton.count())
	}
	// the second garment renders on top of the first result, not the base photo
	if string(f.vton.calls[1].PersonImage) != "person+garment-s1" || string(f.vton.calls[2].PersonImage) != "person+garment-s1+garment-p1" {
		t.Fatalf("chaining broken: %q / %q", f.vton.calls[1].PersonImage, f.vton.calls[2].PersonImage)
	}
	st, _ := f.store.Get(ctx, "r1")
	for _, k := range []string{"s1", "s1|p1", "s1|p1|z1"} {
		if st.Renders[k].Status != session.LookStatusReady {
			t.Errorf("prefix %s not ready: %+v", k, st.Renders[k])
		}
	}

	if _, err := f.r.Render(ctx, "r1", combo("s1", "p1", "z1")); err != nil || f.vton.count() != 3 {
		t.Fatalf("same combo must be free (calls=%d err=%v)", f.vton.count(), err)
	}
	if _, err := f.r.Render(ctx, "r1", combo("s1", "p2", "z1")); err != nil || f.vton.count() != 5 {
		t.Fatalf("swapping pants should cost 2 calls, calls=%d err=%v", f.vton.count(), err)
	}
	if _, err := f.r.Render(ctx, "r1", combo("s1", "p1", "z2")); err != nil || f.vton.count() != 6 {
		t.Fatalf("swapping shoes should cost 1 call, calls=%d err=%v", f.vton.count(), err)
	}
}

func TestConcurrentDuplicateRendersCollapse(t *testing.T) {
	f := newFixture(t)
	f.vton.delay = 40 * time.Millisecond
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.r.Render(context.Background(), "r1", combo("s1", "p1", "z1")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if f.vton.count() != 3 {
		t.Fatalf("duplicate in-flight renders should share work, got %d vton calls", f.vton.count())
	}
}

func TestRenderFailureMarksTheBrokenPrefix(t *testing.T) {
	f := newFixture(t)
	f.vton.fail["Pantalón"] = true
	_, err := f.r.Render(context.Background(), "r1", combo("s1", "p1", "z1"))
	if err == nil {
		t.Fatal("expected failure")
	}
	st, _ := f.store.Get(context.Background(), "r1")
	if st.Renders["s1"].Status != session.LookStatusReady || st.Renders["s1|p1"].Status != session.LookStatusFailed || st.Renders["s1|p1"].Error == "" {
		t.Fatalf("prefix states: %+v", st.Renders)
	}
	if full := st.Renders["s1|p1|z1"]; full.Status != session.LookStatusFailed || full.Error == "" {
		t.Fatalf("the full combination must be marked failed so the app can show it: %+v", full)
	}
	if f.vton.count() != 2 {
		t.Fatalf("expected 2 vton calls, got %d", f.vton.count())
	}
}

func TestRequestRendersInBackground(t *testing.T) {
	f := newFixture(t)
	f.r.Request("r1", combo("s1", "p1", "z1"), true)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := f.store.Get(context.Background(), "r1")
		if rd, ok := st.Renders["s1|p1|z1"]; ok && rd.Status == session.LookStatusReady {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("background render never completed")
}

func TestLookGeneratorUsesRenderer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	_ = f.store.Update(ctx, "r1", func(st *session.State) error {
		st.Looks = []session.Look{{ID: "look_1", Name: "Boda", Pieces: []session.LookPiece{
			{Slot: "footwear", ProductID: "z1"}, {Slot: "upper_body", ProductID: "s1"}, {Slot: "lower_body", ProductID: "p1"},
		}}}
		st.LookResults = []session.LookResult{{LookID: "look_1", Status: session.LookStatusPending}}
		return nil
	})
	g := &LookGenerator{Store: f.store, Renderer: f.r}
	g.GenerateAll(ctx, "r1")

	st, _ := f.store.Get(ctx, "r1")
	lr := st.LookResults[0]
	if lr.Status != session.LookStatusReady || lr.FinalImageURL == "" || len(lr.Pieces) != 3 {
		t.Fatalf("look result: %+v", lr)
	}
	if lr.Pieces[0].Slot != "upper_body" || lr.Pieces[0].TryOnImageURL == "" || lr.Pieces[2].Slot != "footwear" {
		t.Fatalf("pieces should be in chain order with their render urls: %+v", lr.Pieces)
	}
	if f.vton.count() != 3 {
		t.Fatalf("look should reuse the renderer (3 calls), got %d", f.vton.count())
	}
}

func TestExplainVTONError(t *testing.T) {
	if got := explainVTONError(errors.New(`vertex API returned 403: {"error":{"status":"PERMISSION_DENIED","details":[{"reason":"BILLING_DISABLED"}]}}`)); !strings.Contains(got, "facturación") {
		t.Fatalf("billing error not explained: %s", got)
	}
	if got := explainVTONError(errors.New("boom")); got != "la generación falló" {
		t.Fatalf("default reason: %s", got)
	}
}
