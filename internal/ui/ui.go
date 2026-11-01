// Package ui defines the server-driven UI components the app renders next to
// the stylist's message (contract ui_version 1). Components are discriminated
// by "type"; the server never emits a type outside this package, because the
// app ignores unknown ones.
package ui

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Version is the value of "ui_version" in chat responses.
const Version = 1

// Component types.
const (
	TypeText        = "text"
	TypeChips       = "chips"
	TypeSlider      = "slider"
	TypePalette     = "palette"
	TypeSteps       = "steps"
	TypeProductCard = "product_card"
	TypeBudgetBar   = "budget_bar"
)

// Text roles.
const (
	RoleStylist = "stylist"
	RoleHeading = "heading"
	RoleBody    = "body"
)

// Slider profile fields the server applies deterministically.
const (
	FieldBudgetMXN = "budget_mxn"
	FieldRadiusM   = "radius_m"
)

// MaxModelComponents caps how many components the model may attach.
const MaxModelComponents = 6

const maxChipOptions = 6

var hexPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Option is one chip.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Swatch is one colour of a palette.
type Swatch struct {
	Hex  string `json:"hex"`
	Name string `json:"name,omitempty"`
}

// Step is one item of a steps component.
type Step struct {
	Title      string   `json:"title"`
	Detail     string   `json:"detail,omitempty"`
	ProductIDs []string `json:"product_ids,omitempty"`
}

// Nearby is the physical store that carries a product.
type Nearby struct {
	Name      string `json:"name"`
	DistanceM int    `json:"distance_m"`
}

// Component is the union of every component type; only the fields of its Type
// are used and serialized (see MarshalJSON). The same JSON is used to persist
// it in the session, so the tags match the wire names.
type Component struct {
	Type string `json:"type"`

	// text
	Role string `json:"role,omitempty"`
	Text string `json:"text,omitempty"`

	// chips, slider
	ID string `json:"id,omitempty"`

	// chips
	Select     string   `json:"select,omitempty"`
	AllowVoice bool     `json:"allow_voice,omitempty"`
	Options    []Option `json:"options,omitempty"`

	// slider, budget_bar
	Label string `json:"label,omitempty"`

	// slider
	Min          float64 `json:"min,omitempty"`
	Max          float64 `json:"max,omitempty"`
	SliderStep   float64 `json:"step,omitempty"`
	Default      float64 `json:"default,omitempty"`
	Unit         string  `json:"unit,omitempty"`
	ProfileField string  `json:"profile_field,omitempty"`

	// palette
	Swatches []Swatch `json:"swatches,omitempty"`
	Avoid    []Swatch `json:"avoid,omitempty"`
	Note     string   `json:"note,omitempty"`

	// steps
	Items []Step `json:"items,omitempty"`

	// product_card
	ProductID string  `json:"product_id,omitempty"`
	Title     string  `json:"title,omitempty"`
	PriceMXN  float64 `json:"price_mxn,omitempty"`
	Store     string  `json:"store,omitempty"`
	ImageURL  string  `json:"image_url,omitempty"`
	Link      string  `json:"link,omitempty"`
	Nearby    *Nearby `json:"nearby,omitempty"`

	// budget_bar
	TotalMXN  float64 `json:"total_mxn,omitempty"`
	BudgetMXN float64 `json:"budget_mxn,omitempty"`
}

// MarshalJSON emits exactly the fields of the component's type. Numbers that
// are required by the contract (slider range, prices, totals) are always
// written, even when zero.
func (c Component) MarshalJSON() ([]byte, error) {
	switch c.Type {
	case TypeText:
		return json.Marshal(struct {
			Type string `json:"type"`
			Role string `json:"role"`
			Text string `json:"text"`
		}{c.Type, c.Role, c.Text})
	case TypeChips:
		opts := c.Options
		if opts == nil {
			opts = []Option{}
		}
		return json.Marshal(struct {
			Type       string   `json:"type"`
			ID         string   `json:"id"`
			Select     string   `json:"select"`
			AllowVoice bool     `json:"allow_voice"`
			Options    []Option `json:"options"`
		}{c.Type, c.ID, c.Select, c.AllowVoice, opts})
	case TypeSlider:
		return json.Marshal(struct {
			Type         string  `json:"type"`
			ID           string  `json:"id"`
			Label        string  `json:"label,omitempty"`
			Min          float64 `json:"min"`
			Max          float64 `json:"max"`
			Step         float64 `json:"step"`
			Default      float64 `json:"default"`
			Unit         string  `json:"unit,omitempty"`
			ProfileField string  `json:"profile_field,omitempty"`
		}{c.Type, c.ID, c.Label, c.Min, c.Max, c.SliderStep, c.Default, c.Unit, c.ProfileField})
	case TypePalette:
		sw := c.Swatches
		if sw == nil {
			sw = []Swatch{}
		}
		return json.Marshal(struct {
			Type     string   `json:"type"`
			Swatches []Swatch `json:"swatches"`
			Avoid    []Swatch `json:"avoid,omitempty"`
			Note     string   `json:"note,omitempty"`
		}{c.Type, sw, c.Avoid, c.Note})
	case TypeSteps:
		items := c.Items
		if items == nil {
			items = []Step{}
		}
		return json.Marshal(struct {
			Type  string `json:"type"`
			Items []Step `json:"items"`
		}{c.Type, items})
	case TypeProductCard:
		return json.Marshal(struct {
			Type      string  `json:"type"`
			ProductID string  `json:"product_id"`
			Title     string  `json:"title"`
			PriceMXN  float64 `json:"price_mxn"`
			Store     string  `json:"store,omitempty"`
			ImageURL  string  `json:"image_url,omitempty"`
			Link      string  `json:"link,omitempty"`
			Nearby    *Nearby `json:"nearby,omitempty"`
		}{c.Type, c.ProductID, c.Title, c.PriceMXN, c.Store, c.ImageURL, c.Link, c.Nearby})
	case TypeBudgetBar:
		return json.Marshal(struct {
			Type      string  `json:"type"`
			TotalMXN  float64 `json:"total_mxn"`
			BudgetMXN float64 `json:"budget_mxn,omitempty"`
			Label     string  `json:"label,omitempty"`
		}{c.Type, c.TotalMXN, c.BudgetMXN, c.Label})
	}
	return nil, fmt.Errorf("ui: unknown component type %q", c.Type)
}

// UnmarshalJSON reads a persisted component. Slider and numeric fields that
// MarshalJSON always writes are read back by name.
func (c *Component) UnmarshalJSON(b []byte) error {
	type plain Component // same tags, no custom methods
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*c = Component(p)
	return nil
}

// --- constructors used by the server ---

// Text is a line of text.
func Text(role, text string) Component {
	return Component{Type: TypeText, Role: role, Text: text}
}

// Chips is a single-choice set of buttons.
func Chips(id string, allowVoice bool, opts []Option) Component {
	return Component{Type: TypeChips, ID: id, Select: "single", AllowVoice: allowVoice, Options: opts}
}

// Slider is a numeric control; profileField may be "".
func Slider(id, label string, min, max, step, def float64, unit, profileField string) Component {
	return Component{Type: TypeSlider, ID: id, Label: label, Min: min, Max: max, SliderStep: step, Default: def, Unit: unit, ProfileField: profileField}
}

// Palette is a colour palette with optional colours to avoid.
func Palette(swatches, avoid []Swatch, note string) Component {
	return Component{Type: TypePalette, Swatches: swatches, Avoid: avoid, Note: note}
}

// Steps is an ordered list of actions.
func Steps(items []Step) Component { return Component{Type: TypeSteps, Items: items} }

// ProductCard shows one product.
func ProductCard(productID, title string, priceMXN float64, store, imageURL, link string, nearby *Nearby) Component {
	return Component{Type: TypeProductCard, ProductID: productID, Title: title, PriceMXN: priceMXN, Store: store, ImageURL: imageURL, Link: link, Nearby: nearby}
}

// BudgetBar compares the total with the budget (budgetMXN <= 0 omits it).
func BudgetBar(totalMXN, budgetMXN float64, label string) Component {
	if budgetMXN < 0 {
		budgetMXN = 0
	}
	return Component{Type: TypeBudgetBar, TotalMXN: totalMXN, BudgetMXN: budgetMXN, Label: label}
}

// --- parsing model input ---

// rawComponent is the lenient shape of a component as the model writes it.
type rawComponent struct {
	Type         string   `json:"type"`
	Role         string   `json:"role"`
	Text         string   `json:"text"`
	ID           string   `json:"id"`
	AllowVoice   *bool    `json:"allow_voice"`
	Options      []Option `json:"options"`
	Label        string   `json:"label"`
	Min          *float64 `json:"min"`
	Max          *float64 `json:"max"`
	Step         *float64 `json:"step"`
	Default      *float64 `json:"default"`
	Unit         string   `json:"unit"`
	ProfileField string   `json:"profile_field"`
	Swatches     []Swatch `json:"swatches"`
	Avoid        []Swatch `json:"avoid"`
	Note         string   `json:"note"`
}

// Parse validates the components the model attached to ask_user. It returns
// the valid ones (text, chips, slider and palette only, at most
// MaxModelComponents, at most one chips and one slider) and a human-readable
// reason for each one dropped. Parse never fails: bad input yields no
// components.
func Parse(raw json.RawMessage) ([]Component, []string) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, []string{"ui is not an array of components"}
	}
	var out []Component
	var dropped []string
	haveChips, haveSlider := false, false
	for i, it := range items {
		if len(out) >= MaxModelComponents {
			dropped = append(dropped, fmt.Sprintf("ui[%d]: more than %d components", i, MaxModelComponents))
			continue
		}
		var rc rawComponent
		if err := json.Unmarshal(it, &rc); err != nil {
			dropped = append(dropped, fmt.Sprintf("ui[%d]: not an object", i))
			continue
		}
		var c Component
		var reason string
		switch rc.Type {
		case TypeText:
			c, reason = parseText(rc)
		case TypeChips:
			if haveChips {
				reason = "only one chips component per turn"
			} else if c, reason = parseChips(rc); reason == "" {
				haveChips = true
			}
		case TypeSlider:
			if haveSlider {
				reason = "only one slider per turn"
			} else if c, reason = parseSlider(rc); reason == "" {
				haveSlider = true
			}
		case TypePalette:
			c, reason = parsePalette(rc)
		default:
			reason = fmt.Sprintf("type %q is not allowed here", rc.Type)
		}
		if reason != "" {
			dropped = append(dropped, fmt.Sprintf("ui[%d] (%s): %s", i, rc.Type, reason))
			continue
		}
		out = append(out, c)
	}
	return out, dropped
}

func parseText(rc rawComponent) (Component, string) {
	text := strings.TrimSpace(rc.Text)
	if text == "" {
		return Component{}, "empty text"
	}
	role := strings.TrimSpace(rc.Role)
	switch role {
	case "":
		role = RoleStylist
	case RoleStylist, RoleHeading, RoleBody:
	default:
		return Component{}, fmt.Sprintf("unknown role %q", role)
	}
	return Text(role, text), ""
}

func parseChips(rc rawComponent) (Component, string) {
	id := strings.TrimSpace(rc.ID)
	if id == "" {
		return Component{}, "missing id"
	}
	var opts []Option
	seen := map[string]bool{}
	for _, o := range rc.Options {
		o.ID, o.Label = strings.TrimSpace(o.ID), strings.TrimSpace(o.Label)
		if o.ID == "" || o.Label == "" || seen[o.ID] {
			continue
		}
		seen[o.ID] = true
		opts = append(opts, o)
	}
	if len(opts) < 2 {
		return Component{}, "needs at least 2 options with id and label"
	}
	if len(opts) > maxChipOptions {
		opts = opts[:maxChipOptions]
	}
	voice := true
	if rc.AllowVoice != nil {
		voice = *rc.AllowVoice
	}
	return Chips(id, voice, opts), ""
}

func parseSlider(rc rawComponent) (Component, string) {
	id := strings.TrimSpace(rc.ID)
	if id == "" {
		return Component{}, "missing id"
	}
	if rc.Min == nil || rc.Max == nil || rc.Step == nil || rc.Default == nil {
		return Component{}, "min, max, step and default are required"
	}
	min, max, step, def := *rc.Min, *rc.Max, *rc.Step, *rc.Default
	if !(min < max) {
		return Component{}, "min must be lower than max"
	}
	if !(step > 0) {
		return Component{}, "step must be positive"
	}
	if def < min || def > max {
		return Component{}, "default outside min..max"
	}
	field := strings.TrimSpace(rc.ProfileField)
	if field != FieldBudgetMXN && field != FieldRadiusM {
		field = "" // unknown fields are ignored, the control still works
	}
	return Slider(id, strings.TrimSpace(rc.Label), min, max, step, def, strings.TrimSpace(rc.Unit), field), ""
}

func parsePalette(rc rawComponent) (Component, string) {
	sw := validSwatches(rc.Swatches)
	if len(sw) == 0 {
		return Component{}, "needs at least 1 swatch with a #RRGGBB hex"
	}
	return Palette(sw, validSwatches(rc.Avoid), strings.TrimSpace(rc.Note)), ""
}

func validSwatches(in []Swatch) []Swatch {
	var out []Swatch
	for _, s := range in {
		s.Hex, s.Name = strings.TrimSpace(s.Hex), strings.TrimSpace(s.Name)
		if hexPattern.MatchString(s.Hex) {
			out = append(out, s)
		}
	}
	return out
}

// FindChips returns the first chips component, if any.
func FindChips(cs []Component) *Component { return find(cs, TypeChips) }

// FindSlider returns the first slider component, if any.
func FindSlider(cs []Component) *Component { return find(cs, TypeSlider) }

// FindByID returns the chips or slider component with that id.
func FindByID(cs []Component, id string) *Component {
	for i := range cs {
		if cs[i].ID == id && (cs[i].Type == TypeChips || cs[i].Type == TypeSlider) {
			return &cs[i]
		}
	}
	return nil
}

func find(cs []Component, typ string) *Component {
	for i := range cs {
		if cs[i].Type == typ {
			return &cs[i]
		}
	}
	return nil
}
