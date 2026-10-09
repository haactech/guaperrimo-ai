package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	for id, title := range map[string]string{"s1": "Camisa lino", "s2": "Camisa oxford", "p1": "Chino beige", "z1": "Mocasín"} {
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
			{Slot: "footwear", Description: "Mocasín", ProductIDs: []string{"z1"}, Priority: 3},
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
	if len(slots) != 3 || slots[0].(map[string]any)["label"] != "Arriba" {
		t.Fatalf("slots: %v", slots)
	}
	upper := slots[0].(map[string]any)["options"].([]any)
	if len(upper) != 2 || upper[0].(map[string]any)["why"] != "alarga el torso" || upper[0].(map[string]any)["title"] != "Camisa lino" {
		t.Fatalf("upper options should carry product fields and why: %v", upper)
	}
	def := body["default"].(map[string]any)
	if def["upper_body"] != "s1" || def["footwear"] != "z1" {
		t.Fatalf("default: %v", def)
	}

	sel := map[string]any{"selection": map[string]string{"upper_body": "s2", "lower_body": "p1", "footwear": "z1"}}
	code, body = postJSON(t, base+"/matrix/render", sel, nil)
	if code != 202 || body["status"] == "ready" || body["key"] != "s2|p1|z1" {
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

	// the neighbour (s1|p1|z1) was prefetched
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, body = getJSON(t, base+"/matrix")
		renders := body["renders"].([]any)
		if len(renders) >= 2 || time.Now().After(deadline) {
			if len(renders) < 2 {
				t.Fatalf("expected the one-swipe neighbour to be prefetched, got %v", renders)
			}
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	code, body = postJSON(t, base+"/saved-looks", map[string]any{"selection": sel["selection"], "note": "para la boda"}, nil)
	if code != 200 || body["total_mxn"] != float64(1500) || body["image_url"] == "" || body["note"] != "para la boda" {
		t.Fatalf("save look: %d %v", code, body)
	}
	items := body["items"].([]any)
	if len(items) != 3 || items[1].(map[string]any)["why"] != "neutro" {
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
	if code != 200 || body["tryon_available"] != false || len(body["slots"].([]any)) != 3 {
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
