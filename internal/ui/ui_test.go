package ui

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMarshalFieldNames(t *testing.T) {
	cases := []struct {
		name string
		c    Component
		want string
	}{
		{"text", Text(RoleStylist, "Hola"), `{"type":"text","role":"stylist","text":"Hola"}`},
		{"chips", Chips("occasion", true, []Option{{"boda_dia", "Boda de día"}, {"cita", "Cita"}}),
			`{"type":"chips","id":"occasion","select":"single","allow_voice":true,"options":[{"id":"boda_dia","label":"Boda de día"},{"id":"cita","label":"Cita"}]}`},
		{"slider", Slider("budget", "Presupuesto", 500, 10000, 100, 2500, "MXN", "budget_mxn"),
			`{"type":"slider","id":"budget","label":"Presupuesto","min":500,"max":10000,"step":100,"default":2500,"unit":"MXN","profile_field":"budget_mxn"}`},
		{"slider zero default keeps numbers", Slider("x", "", 0, 10, 1, 0, "", ""),
			`{"type":"slider","id":"x","min":0,"max":10,"step":1,"default":0}`},
		{"palette", Palette([]Swatch{{"#1F2A44", "Azul marino"}}, []Swatch{{"#000000", "Negro"}}, "nota"),
			`{"type":"palette","swatches":[{"hex":"#1F2A44","name":"Azul marino"}],"avoid":[{"hex":"#000000","name":"Negro"}],"note":"nota"}`},
		{"palette minimal", Palette([]Swatch{{"#1F2A44", ""}}, nil, ""), `{"type":"palette","swatches":[{"hex":"#1F2A44"}]}`},
		{"steps", Steps([]Step{{"T", "D", []string{"p_1"}}, {Title: "solo"}}),
			`{"type":"steps","items":[{"title":"T","detail":"D","product_ids":["p_1"]},{"title":"solo"}]}`},
		{"product_card", ProductCard("p_1", "Camisa", 740, "H&M", "https://i", "https://l", &Nearby{"H&M Reforma", 450}),
			`{"type":"product_card","product_id":"p_1","title":"Camisa","price_mxn":740,"store":"H\u0026M","image_url":"https://i","link":"https://l","nearby":{"name":"H\u0026M Reforma","distance_m":450}}`},
		{"product_card minimal", ProductCard("p_1", "Camisa", 740, "", "", "", nil), `{"type":"product_card","product_id":"p_1","title":"Camisa","price_mxn":740}`},
		{"budget_bar", BudgetBar(2247, 2500, "Total aproximado"), `{"type":"budget_bar","total_mxn":2247,"budget_mxn":2500,"label":"Total aproximado"}`},
		{"budget_bar no budget", BudgetBar(2247, 0, "Total aproximado"), `{"type":"budget_bar","total_mxn":2247,"label":"Total aproximado"}`},
	}
	for _, tc := range cases {
		if got := mustJSON(t, tc.c); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.name, got, tc.want)
		}
	}
	if _, err := json.Marshal(Component{Type: "weird"}); err == nil {
		t.Error("unknown types must not be emitted")
	}
}

func TestRoundTripThroughStorage(t *testing.T) {
	in := []Component{
		Text(RoleBody, "x"),
		Chips("store", true, []Option{{"a", "A"}, {"b", "B"}}),
		Slider("budget", "Presupuesto", 0, 10, 1, 0, "MXN", "budget_mxn"),
		Palette([]Swatch{{"#112233", "n"}}, nil, ""),
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out []Component
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Errorf("round trip changed components:\n in %+v\nout %+v", in, out)
	}
}

func TestParseValidatesAndDrops(t *testing.T) {
	raw := `[
	 {"type":"text","text":"  Mira esto  "},
	 {"type":"text","role":"shout","text":"x"},
	 {"type":"text","text":" "},
	 {"type":"chips","id":"occ","options":[{"id":"a","label":"A"},{"id":"b","label":"B"},{"id":"","label":"sin id"}]},
	 {"type":"chips","id":"solo","options":[{"id":"a","label":"A"}]},
	 {"type":"chips","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]},
	 {"type":"slider","id":"budget","label":"Presupuesto","min":500,"max":10000,"step":100,"default":2500,"unit":"MXN","profile_field":"budget_mxn"},
	 {"type":"slider","id":"bad","min":10,"max":5,"step":1,"default":7},
	 {"type":"slider","id":"bad2","min":0,"max":5,"step":0,"default":1},
	 {"type":"slider","id":"bad3","min":0,"max":5,"step":1,"default":9},
	 {"type":"palette","swatches":[{"hex":"azul","name":"x"},{"hex":"#1f2a44","name":"Azul"}],"avoid":[{"hex":"zzz"}]},
	 {"type":"palette","swatches":[{"hex":"nope"}]},
	 {"type":"product_card","product_id":"p_1","title":"t","price_mxn":1},
	 {"type":"nonsense"}
	]`
	got, dropped := Parse(json.RawMessage(raw))
	var types []string
	for _, c := range got {
		types = append(types, c.Type)
	}
	if want := "text,chips,slider,palette"; strings.Join(types, ",") != want {
		t.Fatalf("types = %v, want %s", types, want)
	}
	if got[0].Role != RoleStylist || got[0].Text != "Mira esto" {
		t.Errorf("text defaults: %+v", got[0])
	}
	if len(got[1].Options) != 2 || !got[1].AllowVoice || got[1].Select != "single" {
		t.Errorf("chips: %+v", got[1])
	}
	if got[2].ProfileField != "budget_mxn" || got[2].Default != 2500 {
		t.Errorf("slider: %+v", got[2])
	}
	if len(got[3].Swatches) != 1 || got[3].Swatches[0].Hex != "#1f2a44" || len(got[3].Avoid) != 0 {
		t.Errorf("palette: %+v", got[3])
	}
	if len(dropped) != 10 {
		t.Errorf("dropped %d, want 10: %v", len(dropped), dropped)
	}
}

func TestParseLimitsAndGarbage(t *testing.T) {
	var parts []string
	for i := 0; i < 8; i++ {
		parts = append(parts, `{"type":"text","text":"t"}`)
	}
	got, dropped := Parse(json.RawMessage("[" + strings.Join(parts, ",") + "]"))
	if len(got) != MaxModelComponents || len(dropped) != 2 {
		t.Errorf("cap: %d kept, %d dropped", len(got), len(dropped))
	}
	// duplicates of chips/slider are dropped
	got, _ = Parse(json.RawMessage(`[{"type":"slider","id":"a","min":0,"max":1,"step":1,"default":0},{"type":"slider","id":"b","min":0,"max":1,"step":1,"default":0}]`))
	if len(got) != 1 {
		t.Errorf("one slider max: %+v", got)
	}
	for _, raw := range []string{``, `null`, `"x"`, `{"type":"text"}`, `[1,2]`} {
		if got, _ := Parse(json.RawMessage(raw)); len(got) != 0 {
			t.Errorf("Parse(%q) = %+v", raw, got)
		}
	}
	slider, _ := Parse(json.RawMessage(`[{"type":"slider","id":"a","min":0,"max":9,"step":1,"default":3,"profile_field":"shoe_size"}]`))
	if len(slider) != 1 || slider[0].ProfileField != "" {
		t.Errorf("unknown profile_field must be cleared: %+v", slider)
	}
}
