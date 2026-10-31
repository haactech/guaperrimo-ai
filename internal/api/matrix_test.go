package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"stylerag/internal/session"
	"stylerag/internal/shopping"
	"stylerag/internal/tryon"
)

type apiFakeVTON struct{}

func (apiFakeVTON) Name() string { return "fake" }
func (apiFakeVTON) Generate(_ context.Context, req tryon.VTONRequest) (*tryon.VTONResult, error) {
	if len(req.GarmentImage) == 0 {
		return nil, errors.New("no garment")
	}
	return &tryon.VTONResult{ImageBytes: []byte("img"), MimeType: "image/jpeg", GenerationMs: 1, ProviderName: "fake"}, nil
}

func finishedSession(t *testing.T, imgServer string) (*session.MemoryStore, *memImages) {
	t.Helper()
	store := session.NewMemoryStore(time.Hour)
	t.Cleanup(store.Stop)
	images := &memImages{objects: map[string][]byte{"sessions/sess_mx_1/welcome_1.jpg": []byte("person")}}

	st := session.New("sess_mx_1")
	st.Phase = session.PhaseDone
	st.ImageKey = "sessions/sess_mx_1/welcome_1.jpg"
	st.ImageURL = images.URL(st.ImageKey)
	st.Stores = []shopping.Store{{PlaceID: "pl1", Name: "Zara Reforma", DistanceM: 400}}
	for id, title := range map[string]string{"s1": "Camisa lino", "s2": "Camisa oxford", "p1": "Chino beige", "z1": "Saco ligero"} {
		p := shopping.Product{ID: id, Title: title, Store: "Zara MX", Price: 500, Currency: "MXN", Thumbnail: imgServer + "/img/" + id}
		if id == "s1" {
			p.NearbyStore = &shopping.StoreRef{PlaceID: "pl1", Name: "Zara Reforma", DistanceM: 400}
		}
		st.Products[id] = p
	}
	st.Recommendation = &session.Recommendation{
		Summary: "ok",
		ShoppingList: []session.ShoppingItem{
			{Slot: "upper_body", Description: "Camisa", Why: "alarga el torso", ProductIDs: []string{"s1", "s2"}, Priority: 1},
			{Slot: "lower_body", Description: "Chino", Why: "neutro", ProductIDs: []string{"p1"}, Priority: 2},
			{Slot: "outerwear", Description: "Saco ligero", ProductIDs: []string{"z1"}, Priority: 3},
		},
	}
	st.Matrix = session.BuildMatrix(st, 3)
	if err := store.Save(context.Background(), st); err != nil {
		t.Fatal(err)
	}
	return store, images
}

func matrixServer(t *testing.T, withRenderer bool) (*httptest.Server, *session.MemoryStore) {
	t.Helper()
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("garment"))
	}))
	t.Cleanup(imgSrv.Close)
	store, images := finishedSession(t, imgSrv.URL)
	deps := &MatrixDeps{Store: store}
	if withRenderer {
		deps.Renderer = tryon.NewRenderer(store, images, apiFakeVTON{}, nil, imgSrv.Client(), 3, 2, 5*time.Second)
	}
	srv := httptest.NewServer(NewRouter(Deps{Images: images, Store: store, Chat: &ChatDeps{Store: store, Images: images}, Matrix: deps}))
	t.Cleanup(srv.Close)
	return srv, store
}

func TestMatrixRenderAndSave(t *testing.T) {
	srv, _ := matrixServer(t, true)
	base := srv.URL + "/session/sess_mx_1"

	code, body := getJSON(t, base+"/matrix")
	if code != 200 || body["tryon_available"] != true {
		t.Fatalf("matrix: %d %v", code, body)
	}
	slots := body["slots"].([]any)
	if len(slots) != 2 || slots[0].(map[string]any)["label"] != "Arriba" {
		t.Fatalf("slots: %v", slots)
	}
	upper := slots[0].(map[string]any)["options"].([]any)
	for _, sl := range slots {
		if sl.(map[string]any)["slot"] == "lower_body" {
			t.Fatalf("lower_body must not be in the grid: %v", slots)
		}
	}
	if len(upper) != 2 || upper[0].(map[string]any)["why"] != "alarga el torso" || upper[0].(map[string]any)["title"] != "Camisa lino" {
		t.Fatalf("upper options should carry product fields and why: %v", upper)
	}
	def := body["default"].(map[string]any)
	if def["upper_body"] != "s1" || def["outerwear"] != "z1" {
		t.Fatalf("default: %v", def)
	}

	sel := map[string]any{"selection": map[string]string{"upper_body": "s2", "lower_body": "p1", "outerwear": "z1"}}
	code, body = postJSON(t, base+"/matrix/render", sel, nil)
	if code != 202 || body["status"] == "ready" || body["key"] != "s2|z1" {
		t.Fatalf("first render request: %d %v", code, body)
	}
	var ready bool
	for i := 0; i < 100 && !ready; i++ {
		time.Sleep(20 * time.Millisecond)
		code, body = postJSON(t, base+"/matrix/render", sel, nil)
		ready = code == 200 && body["status"] == "ready" && strings.HasPrefix(body["image_url"].(string), "https://img.test/")
	}
	if !ready {
		t.Fatalf("render never became ready: %d %v", code, body)
	}

	// no speculative renders: only the requested combination exists (each image is billed)
	time.Sleep(200 * time.Millisecond)
	_, body = getJSON(t, base+"/matrix")
	if renders := body["renders"].([]any); len(renders) != 1 {
		t.Fatalf("only the requested combination may be rendered, got %d: %v", len(renders), renders)
	}

	code, body = postJSON(t, base+"/saved-looks", map[string]any{"selection": sel["selection"], "note": "para la boda"}, nil)
	if code != 200 || body["total_mxn"] != float64(1000) || body["image_url"] == "" || body["note"] != "para la boda" {
		t.Fatalf("save look: %d %v", code, body)
	}
	items := body["items"].([]any)
	if len(items) != 2 || items[0].(map[string]any)["why"] != "alarga el torso" {
		t.Fatalf("saved items: %v", items)
	}
	code, body = getJSON(t, base+"/saved-looks")
	if code != 200 || len(body["saved_looks"].([]any)) != 1 {
		t.Fatalf("list saved: %d %v", code, body)
	}

	code, _ = postJSON(t, base+"/matrix/render", map[string]any{"selection": map[string]string{"upper_body": "nope"}}, nil)
	if code != 400 {
		t.Fatalf("invalid selection should be 400, got %d", code)
	}
}

func TestMatrixWithoutTryOn(t *testing.T) {
	srv, _ := matrixServer(t, false)
	base := srv.URL + "/session/sess_mx_1"
	code, body := getJSON(t, base+"/matrix")
	if code != 200 || body["tryon_available"] != false || len(body["slots"].([]any)) != 2 {
		t.Fatalf("grid must work without try-on: %d %v", code, body)
	}
	code, _ = postJSON(t, base+"/matrix/render", map[string]any{"selection": map[string]string{}}, nil)
	if code != 503 {
		t.Fatalf("render without try-on should be 503, got %d", code)
	}
	code, body = postJSON(t, base+"/saved-looks", map[string]any{"selection": map[string]string{}}, nil)
	if code != 200 || len(body["stores"].([]any)) != 1 {
		t.Fatalf("saving the default look should work and list the nearby store: %d %v", code, body)
	}
}

type slotCounter struct {
	name string
	mu   sync.Mutex
	got  []string
}

func (c *slotCounter) Name() string { return c.name }
func (c *slotCounter) Generate(ctx context.Context, req tryon.VTONRequest) (*tryon.VTONResult, error) {
	c.mu.Lock()
	c.got = append(c.got, req.Category)
	c.mu.Unlock()
	return apiFakeVTON{}.Generate(ctx, req)
}

func (c *slotCounter) calls() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.got...)
}

func TestCompleteLookRendersOnlyPantsWithTheirProvider(t *testing.T) {
	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("garment"))
	}))
	t.Cleanup(imgSrv.Close)
	store, images := finishedSession(t, imgSrv.URL)
	cheap, pricey := &slotCounter{name: "fashn"}, &slotCounter{name: "google_vertex"}
	router := &tryon.SlotRouter{Default: cheap, BySlot: map[string]tryon.VTONProvider{
		session.BottomSlot: tryon.NewBudgetedVTON(pricey, 1, 3),
	}}
	deps := &MatrixDeps{Store: store, Renderer: tryon.NewRenderer(store, images, router, nil, imgSrv.Client(), 3, 2, 5*time.Second), CompleteLooks: true}
	srv := httptest.NewServer(NewRouter(Deps{Images: images, Store: store, Chat: &ChatDeps{Store: store, Images: images}, Matrix: deps}))
	t.Cleanup(srv.Close)
	base := srv.URL + "/session/sess_mx_1"

	_, body := getJSON(t, base+"/matrix")
	bottoms, ok := body["bottoms"].(map[string]any)
	if !ok || bottoms["label"] != "Abajo" || len(bottoms["options"].([]any)) != 1 {
		t.Fatalf("bottoms row: %v", body["bottoms"])
	}

	waitReady := func(path string, req map[string]any, key string) {
		t.Helper()
		for i := 0; i < 100; i++ {
			code, body := postJSON(t, base+path, req, nil)
			if code == 200 && body["status"] == "ready" && body["key"] == key {
				return
			}
			if body["status"] == "failed" {
				t.Fatalf("%s failed: %v", path, body)
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("%s never became ready", path)
	}
	sel := map[string]any{"selection": map[string]string{"upper_body": "s2"}}
	waitReady("/matrix/render", sel, "s2|z1")
	waitReady("/matrix/complete", sel, "s2|z1|p1")

	if got := cheap.calls(); len(got) != 2 || got[0] != "upper_body" || got[1] != "outerwear" {
		t.Fatalf("grid provider calls = %v, want upper_body, outerwear", got)
	}
	if got := pricey.calls(); len(got) != 1 || got[0] != "lower_body" {
		t.Fatalf("pants provider calls = %v, want one lower_body (grid render reused)", got)
	}

	_, body = getJSON(t, base+"/matrix")
	if c := body["complete_renders"].([]any); len(c) != 1 || c[0].(map[string]any)["selection"].(map[string]any)["lower_body"] != "p1" {
		t.Fatalf("complete_renders: %v", c)
	}
	if r := body["renders"].([]any); len(r) != 1 {
		t.Fatalf("grid renders must not include the complete look: %v", r)
	}

	// Another top means another pants image: over the session budget of 1.
	other := map[string]any{"selection": map[string]string{"upper_body": "s1"}}
	// Poll GET like the app does: a POST would retry the failed render.
	postJSON(t, base+"/matrix/complete", other, nil)
	var failed map[string]any
	for i := 0; i < 100 && failed == nil; i++ {
		time.Sleep(20 * time.Millisecond)
		_, body = getJSON(t, base+"/matrix")
		for _, c := range body["complete_renders"].([]any) {
			if c := c.(map[string]any); c["key"] == "s1|z1|p1" && c["status"] == "failed" {
				failed = c
			}
		}
	}
	if failed == nil || !strings.Contains(failed["error"].(string), "límite") {
		t.Fatalf("second complete look should hit the pants budget: %v", body["complete_renders"])
	}
	if got := pricey.calls(); len(got) != 1 {
		t.Fatalf("budget must stop the call before the provider, got %v", got)
	}

	code, _ := postJSON(t, base+"/matrix/complete", map[string]any{"selection": map[string]string{}, "lower_body": "s1"}, nil)
	if code != 400 {
		t.Fatalf("a shirt as lower_body should be 400, got %d", code)
	}
}

func TestCompleteLookDisabled(t *testing.T) {
	srv, _ := matrixServer(t, true)
	base := srv.URL + "/session/sess_mx_1"
	_, body := getJSON(t, base+"/matrix")
	if _, ok := body["bottoms"]; ok {
		t.Fatalf("bottoms must be hidden when complete looks are off: %v", body["bottoms"])
	}
	if code, _ := postJSON(t, base+"/matrix/complete", map[string]any{"selection": map[string]string{}}, nil); code != 503 {
		t.Fatalf("complete without a pants provider should be 503, got %d", code)
	}
}
