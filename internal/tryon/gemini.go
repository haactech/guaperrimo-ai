package tryon

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// GeminiVTON implements VTONProvider with Gemini image editing ("Nano
// Banana"). It is a general image model, not a try-on model: it is far
// cheaper than Vertex try-on and can dress the legs on a mid-thigh photo,
// but it may drift the garment color or the face, so results must be
// compared before trusting it with real products.
type GeminiVTON struct {
	APIKey  string
	Model   string // gemini-2.5-flash-image, gemini-3-pro-image-preview, …
	BaseURL string // defaults to the public Gemini API; tests override it
	// AspectRatio forces the output shape ("9:16"); empty matches the person
	// photo, so the render drops into the app without reframing.
	AspectRatio string
	HTTPClient  *http.Client
}

// OutfitGarment is one product photo for a single-call outfit render.
type OutfitGarment struct {
	Image []byte
	Slot  string // upper_body | outerwear | lower_body | accessory
	Desc  string
}

func (g *GeminiVTON) Name() string { return "gemini:" + g.model() }

func (g *GeminiVTON) model() string {
	if g.Model == "" {
		return "gemini-3-pro-image"
	}
	return g.Model
}

func (g *GeminiVTON) Generate(ctx context.Context, req VTONRequest) (*VTONResult, error) {
	return g.GenerateOutfit(ctx, req.PersonImage, []OutfitGarment{{Image: req.GarmentImage, Slot: req.Category, Desc: req.GarmentDesc}})
}

// GenerateOutfit dresses the person in every garment with one billed image.
func (g *GeminiVTON) GenerateOutfit(ctx context.Context, person []byte, garments []OutfitGarment) (*VTONResult, error) {
	if len(garments) == 0 {
		return nil, fmt.Errorf("no garments")
	}
	start := time.Now()

	parts := []map[string]any{{"text": outfitPrompt(garments)}, inlineImage(person)}
	for _, gm := range garments {
		parts = append(parts, inlineImage(gm.Image))
	}
	gen := map[string]any{"responseModalities": []string{"TEXT", "IMAGE"}}
	ratio := g.AspectRatio
	if ratio == "" {
		ratio = nearestAspect(person)
	}
	if ratio != "" {
		gen["imageConfig"] = map[string]any{"aspectRatio": ratio}
	}
	body, err := json.Marshal(map[string]any{
		"contents":         []map[string]any{{"role": "user", "parts": parts}},
		"generationConfig": gen,
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	base := g.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com/v1beta"
	}
	url := fmt.Sprintf("%s/models/%s:generateContent", strings.TrimRight(base, "/"), g.model())
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("x-goog-api-key", g.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")

	client := g.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("calling gemini API: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	var result struct {
		Candidates []struct {
			FinishReason string `json:"finishReason"`
			Content      struct {
				Parts []struct {
					Text       string `json:"text"`
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}
	if r := result.PromptFeedback.BlockReason; r != "" {
		return nil, fmt.Errorf("gemini blocked the request: %s", r)
	}
	var text []string
	for _, c := range result.Candidates {
		for _, p := range c.Content.Parts {
			if p.InlineData != nil && p.InlineData.Data != "" {
				img, err := base64.StdEncoding.DecodeString(p.InlineData.Data)
				if err != nil {
					return nil, fmt.Errorf("decoding image: %w", err)
				}
				return &VTONResult{
					ImageBytes:   img,
					MimeType:     p.InlineData.MimeType,
					GenerationMs: time.Since(start).Milliseconds(),
					ProviderName: g.Name(),
				}, nil
			}
			if p.Text != "" {
				text = append(text, p.Text)
			}
		}
		if c.FinishReason != "" && c.FinishReason != "STOP" {
			text = append(text, "finish_reason="+c.FinishReason)
		}
	}
	return nil, fmt.Errorf("gemini returned no image: %s", truncate(strings.Join(text, " "), 300))
}

var slotWording = map[string]string{
	"upper_body": "the shirt or top (it replaces his current top)",
	"outerwear":  "the jacket or overshirt (worn open over the top)",
	"lower_body": "the pants (they replace his current pants or shorts on every visible part of the legs)",
	"accessory":  "the accessory",
}

// outfitPrompt asks for a faithful edit: same person, pose, framing and
// light, with each product copied exactly. Garment fidelity matters more
// than looks here: the user goes to a real store to buy what he sees.
// Product photos come from store listings, so they may show the garment
// from the back, on a model with other clothes, or at low resolution.
func outfitPrompt(garments []OutfitGarment) string {
	var b strings.Builder
	b.WriteString("Virtual try-on photo edit. Image 1 is a photo of a man. ")
	for i, gm := range garments {
		w := slotWording[gm.Slot]
		if w == "" {
			w = "the garment"
		}
		fmt.Fprintf(&b, "Image %d is a store product photo of %s", i+2, w)
		if gm.Desc != "" {
			fmt.Fprintf(&b, ", listed as \"%s\"", gm.Desc)
		}
		b.WriteString(". ")
	}
	b.WriteString("Edit image 1 so the man is wearing these products. Every listed product must be visible in the result.\n")
	b.WriteString("Keep: exactly the same person, face, hair, skin tone, body shape, pose, expression, background, lighting, camera angle and framing as image 1. Do not crop, zoom out or extend the scene. Tattoos stay only on skin that is still visible; skin covered by the new clothes is hidden, so never draw a tattoo on fabric and never add tattoos.\n")
	b.WriteString("Fidelity: copy each product's real color and shade as seen in its photo, only adapted to the lighting of image 1 (do not darken it or shift its hue). Copy fabric texture, pattern scale and direction (stripes, checks, prints), buttons, collar, pockets, garment length and sleeve length (a long-sleeve product gets full-length sleeves even if his current top has short sleeves). Plain fabric stays plain: never invent patterns. ")
	b.WriteString("Image 1 shows his front. If a product photo shows the back of the garment, you see him from the front, so a print, logo or text that is on the back of the garment is hidden: the front of that garment is plain unless the product photo shows otherwise.\n")
	b.WriteString("Product photos: take only the named garment from each one; ignore the model, mannequin, other clothes and background in it. Do not add any garment that is not in the product photos (no extra undershirt or t-shirt); clothes from image 1 that are not replaced stay as they are, except what a replaced garment covers.\n")
	b.WriteString("Wear each top the way its product photo shows it (tucked or untucked); a realistic fit with natural wrinkles for his body. Return only the edited photo.")
	return b.String()
}

// geminiAspects are the output shapes the image models accept.
var geminiAspects = []string{"1:1", "2:3", "3:2", "3:4", "4:3", "4:5", "5:4", "9:16", "16:9", "21:9"}

// nearestAspect returns the accepted shape closest to the photo, or "" when
// the photo cannot be read (the model then picks its own).
func nearestAspect(img []byte) string {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(img))
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		return ""
	}
	want := math.Log(float64(cfg.Width) / float64(cfg.Height))
	best, bestDiff := "", math.Inf(1)
	for _, a := range geminiAspects {
		var w, h float64
		fmt.Sscanf(a, "%f:%f", &w, &h)
		if d := math.Abs(math.Log(w/h) - want); d < bestDiff {
			best, bestDiff = a, d
		}
	}
	return best
}

func inlineImage(data []byte) map[string]any {
	mime := http.DetectContentType(data)
	if !strings.HasPrefix(mime, "image/") {
		mime = "image/jpeg"
	}
	return map[string]any{"inline_data": map[string]string{
		"mime_type": mime,
		"data":      base64.StdEncoding.EncodeToString(data),
	}}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
