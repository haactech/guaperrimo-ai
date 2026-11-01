package api

import (
	"context"
	"encoding/json"
	"testing"

	"stylerag/internal/llm"
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
	slider := body["ui"].([]any)[0].(map[string]any)
	if slider["profile_field"] != "budget_mxn" || slider["default"] != float64(2500) {
		t.Errorf("slider: %v", slider)
	}
	st, _ := store.Get(context.Background(), "sess_ui_001")
	if got := st.Transcript[len(st.Transcript)-2]; got.Role != "user" || got.Text != "Boda de día" {
		t.Errorf("chip label not resolved: %+v", got)
	}
	if len(st.LastAssistantUI()) != 1 {
		t.Errorf("assistant ui must be persisted: %+v", st.Transcript)
	}

	// Turns without components omit ui entirely.
	code, body = postJSON(t, base+"/chat", map[string]any{"type": "text", "text": "hola"}, nil)
	raw, _ := json.Marshal(body)
	if code != 200 || body["ui_version"] != float64(1) || body["speech"] != "¿Algo más?" || body["ui"] != nil {
		t.Fatalf("plain turn: %d %s", code, raw)
	}
}
