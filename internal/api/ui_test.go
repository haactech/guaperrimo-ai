package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"stylerag/internal/agent"
	"stylerag/internal/llm"
	"stylerag/internal/session"
	"stylerag/internal/shopping"
)

func TestChatResponseCarriesUIFromAskUser(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("a1", "ask_user", map[string]any{
			"message": "Qué buena base. ¿Para qué ocasión?", "input_mode": "voice",
			"ui": []any{
				map[string]any{"type": "text", "role": "body", "text": "Elige una"},
				map[string]any{"type": "chips", "id": "occasion", "options": []map[string]string{{"id": "boda_dia", "label": "Boda de día"}, {"id": "cita", "label": "Cita"}}},
				map[string]any{"type": "product_card", "product_id": "p_1", "title": "no", "price_mxn": 1},
			},
		})),
		calls(call("a2", "ask_user", map[string]any{"message": "¿Cuánto quieres gastar?", "input_mode": "voice",
			"ui": []any{map[string]any{"type": "slider", "id": "budget", "label": "Presupuesto", "min": 500, "max": 10000, "step": 100, "default": 2500, "unit": "MXN", "profile_field": "budget_mxn"}}})),
		calls(call("a3", "ask_user", map[string]any{"message": "¿Algo más?", "input_mode": "voice"})),
	}}
	srv, store := newTestServer(t, model, "")
	base := srv.URL + "/session/sess_ui_001"
	uploadImage(t, base+"/image")

	loc := map[string]any{"lat": 19.4194, "lng": -99.1616}
	code, body := postJSON(t, base+"/chat", map[string]any{"type": "image", "location": loc}, nil)
	if code != 200 {
		t.Fatalf("%d %v", code, body)
	}
	if body["ui_version"] != float64(1) || body["speech"] != body["message"] || body["input_mode"] != "buttons" {
		t.Fatalf("envelope: %v", body)
	}
	uiList := body["ui"].([]any)
	if len(uiList) != 2 {
		t.Fatalf("invalid component must be dropped: %v", uiList)
	}
	chips := uiList[1].(map[string]any)
	if chips["type"] != "chips" || chips["select"] != "single" || chips["allow_voice"] != true {
		t.Errorf("chips: %v", chips)
	}
	if opts := body["options"].([]any); len(opts) != 2 || opts[0].(map[string]any)["id"] != "boda_dia" {
		t.Errorf("options derived from chips: %v", body["options"])
	}

	// A chip tap is a normal button_response resolved to the chip label.
	code, body = postJSON(t, base+"/chat", map[string]any{"type": "button_response", "option_id": "boda_dia"}, nil)
	if code != 200 || body["input_mode"] != "ui" {
		t.Fatalf("slider turn: %d %v", code, body)
	}
	if opts := body["options"].([]any); len(opts) != 0 {
		t.Errorf("options must stay an empty array: %v", body["options"])
	}
	slider := body["ui"].([]any)[1].(map[string]any) // after the question text
	if slider["profile_field"] != "budget_mxn" || slider["default"] != float64(2500) {
		t.Errorf("slider: %v", slider)
	}
	st, _ := store.Get(context.Background(), "sess_ui_001")
	if got := st.Transcript[len(st.Transcript)-2]; got.Role != "user" || got.Text != "Boda de día" {
		t.Errorf("chip label not resolved: %+v", got)
	}
	if len(st.LastAssistantUI()) != 2 {
		t.Errorf("assistant ui must be persisted: %+v", st.Transcript)
	}

	// Turns without components omit ui entirely. With the occasion (from the
	// chip) and the budget (from the slider) known, the server adds nothing.
	if st.Profile.Occasion != "Boda de día" {
		t.Errorf("the occasion chip must set the occasion: %q", st.Profile.Occasion)
	}
	code, body = postJSON(t, base+"/chat", map[string]any{"type": "ui_action", "component_id": "budget", "value": "3000"}, nil)
	raw, _ := json.Marshal(body)
	if code != 200 || body["ui_version"] != float64(1) || body["speech"] != "¿Algo más?" || body["ui"] != nil {
		t.Fatalf("plain turn: %d %s", code, raw)
	}
}

func TestUIActionAppliesProfileFieldDeterministically(t *testing.T) {
	slider := func(id, field string, min, max, step, def float64) map[string]any {
		return map[string]any{"type": "slider", "id": id, "label": "Presupuesto", "min": min, "max": max, "step": step, "default": def, "unit": "MXN", "profile_field": field}
	}
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("a1", "ask_user", map[string]any{"message": "¿Cuánto quieres gastar?", "input_mode": "voice", "ui": []any{slider("budget", "budget_mxn", 500, 10000, 100, 2500)}})),
		// the model does NOT call update_profile: the server must have set the budget already
		calls(call("a2", "ask_user", map[string]any{"message": "¿Qué tan lejos caminas?", "input_mode": "voice", "ui": []any{slider("radius", "radius_m", 300, 3000, 100, 1500)}})),
		calls(call("a3", "ask_user", map[string]any{"message": "Listo", "input_mode": "voice"})),
	}}
	srv, store := newTestServer(t, model, "")
	base := srv.URL + "/session/sess_ui_002"
	uploadImage(t, base+"/image")
	if code, body := postJSON(t, base+"/chat", map[string]any{"type": "image", "location": map[string]any{"lat": 19.4194, "lng": -99.1616}}, nil); code != 200 || body["input_mode"] != "ui" {
		t.Fatalf("%d %v", code, body)
	}

	for _, bad := range []map[string]any{
		{"type": "ui_action", "value": "2500"},
		{"type": "ui_action", "component_id": "budget"},
		{"type": "ui_action", "component_id": "nope", "value": "2500"},
		{"type": "ui_action", "component_id": "budget", "value": "mucho"},
	} {
		if code, body := postJSON(t, base+"/chat", bad, nil); code != 400 {
			t.Errorf("%v should be 400, got %d %v", bad, code, body)
		}
	}
	st, _ := store.Get(context.Background(), "sess_ui_002")
	if st.Turn != 1 || st.Profile.BudgetMXN != 0 {
		t.Fatalf("rejected actions must not touch the session: turn=%d budget=%v", st.Turn, st.Profile.BudgetMXN)
	}

	code, body := postJSON(t, base+"/chat", map[string]any{"type": "ui_action", "component_id": "budget", "value": "2700"}, nil)
	if code != 200 || body["input_mode"] != "ui" {
		t.Fatalf("%d %v", code, body)
	}
	st, _ = store.Get(context.Background(), "sess_ui_002")
	if st.Profile.BudgetMXN != 2700 {
		t.Errorf("budget = %v, want 2700", st.Profile.BudgetMXN)
	}
	if u := st.Transcript[len(st.Transcript)-2]; u.Role != "user" || u.InputMode != "ui" || u.Text != "Presupuesto: 2700 MXN" {
		t.Errorf("user turn: %+v", u)
	}
	if m := st.Messages[len(st.Messages)-3]; m.Content != "[ui] Presupuesto: 2700 MXN" {
		t.Errorf("the model must see the [ui] text, got %q", m.Content)
	}

	// out-of-range values are clamped; radius_m lands on the profile
	if code, _ := postJSON(t, base+"/chat", map[string]any{"type": "ui_action", "component_id": "radius", "value": "99999"}, nil); code != 200 {
		t.Fatal(code)
	}
	st, _ = store.Get(context.Background(), "sess_ui_002")
	if st.Profile.RadiusM != 3000 || st.Profile.BudgetMXN != 2700 {
		t.Errorf("profile after radius: %+v", st.Profile)
	}
}

func TestOneScreenAnswersOccasionAndBudgetTogether(t *testing.T) {
	model := &scripted{responses: []*llm.CompletionResponse{
		calls(call("a1", "ask_user", map[string]any{"message": "¿Para qué ocasión quieres verte bien?", "input_mode": "voice"})),
		calls(call("a2", "ask_user", map[string]any{"message": "Listo, ¿algo que no te guste usar?", "input_mode": "voice"})),
	}}
	store := session.NewMemoryStore(time.Hour)
	t.Cleanup(store.Stop)
	images := &memImages{}
	runner := agent.NewRunner(model, agent.Deps{Shopping: shopping.Fake{}, MinSearchTime: time.Second, RequirePreview: true}, 8, 4)
	srv := httptest.NewServer(NewRouter(Deps{Images: images, Store: store,
		Chat: &ChatDeps{Store: store, Images: images, Analyzer: fakeAnalyzer{}, Runner: runner, TurnTimeout: 10 * time.Second}}))
	t.Cleanup(srv.Close)
	base := srv.URL + "/session/sess_ui_003"
	uploadImage(t, base+"/image")

	code, body := postJSON(t, base+"/chat", map[string]any{"type": "image", "location": map[string]any{"lat": 19.4194, "lng": -99.1616}}, nil)
	if code != 200 || body["input_mode"] != "ui" {
		t.Fatalf("first screen: %d %v", code, body)
	}
	var hasChips, hasSlider bool
	for _, c := range body["ui"].([]any) {
		switch c.(map[string]any)["type"] {
		case "chips":
			hasChips = true
		case "slider":
			hasSlider = true
		}
	}
	if !hasChips || !hasSlider {
		t.Fatalf("occasion chips and budget slider on one screen: %v", body["ui"])
	}

	for _, bad := range []map[string]any{
		{"type": "ui_action", "values": map[string]string{"occasion": "nope", "budget": "3500"}},
		{"type": "ui_action", "values": map[string]string{"color": "x"}},
		{"type": "ui_action", "values": map[string]string{"occasion": "boda_dia", "budget": "mucho"}},
	} {
		if code, _ := postJSON(t, base+"/chat", bad, nil); code != 400 {
			t.Errorf("%v → %d, want 400", bad, code)
		}
	}

	code, body = postJSON(t, base+"/chat", map[string]any{"type": "ui_action", "values": map[string]string{"occasion": "boda_dia", "budget": "3500"}}, nil)
	if code != 200 {
		t.Fatalf("%d %v", code, body)
	}
	st, _ := store.Get(context.Background(), "sess_ui_003")
	if st.Profile.Occasion != "Boda de día" || st.Profile.BudgetMXN != 3500 {
		t.Fatalf("both answers must reach the profile: %+v", st.Profile)
	}
	user := st.Transcript[len(st.Transcript)-2]
	if user.Role != "user" || user.Text != "Ocasión: Boda de día. Presupuesto total: 3500 MXN" {
		t.Fatalf("the agent must read both answers: %+v", user)
	}
	if body["ui"] != nil {
		t.Errorf("with both answers no guided control is left: %v", body["ui"])
	}
}
