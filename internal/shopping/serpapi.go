package shopping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// SerpAPI implements Provider on top of serpapi.com (Google Shopping + Google Maps).
type SerpAPI struct {
	apiKey          string
	baseURL         string
	gl              string
	hl              string
	defaultLocation string
	http            *http.Client
}

// NewSerpAPI builds a client for Mexican, Spanish-language results.
func NewSerpAPI(apiKey, defaultLocation string) *SerpAPI {
	if defaultLocation == "" {
		defaultLocation = "Mexico"
	}
	return &SerpAPI{
		apiKey:          apiKey,
		baseURL:         "https://serpapi.com",
		gl:              "mx",
		hl:              "es",
		defaultLocation: defaultLocation,
		http:            &http.Client{Timeout: 20 * time.Second},
	}
}

// WithBaseURL overrides the endpoint (tests).
func (s *SerpAPI) WithBaseURL(u string) *SerpAPI {
	s.baseURL = strings.TrimRight(u, "/")
	return s
}

func (s *SerpAPI) Name() string { return "serpapi" }

// --- wire types ---

type serpShoppingResponse struct {
	ShoppingResults []serpShoppingItem `json:"shopping_results"`
	Error           string             `json:"error"`
}

type serpShoppingItem struct {
	Title          string   `json:"title"`
	ProductID      string   `json:"product_id"`
	ProductLink    string   `json:"product_link"`
	Link           string   `json:"link"`
	Source         string   `json:"source"`
	Price          string   `json:"price"`
	ExtractedPrice float64  `json:"extracted_price"`
	Rating         float64  `json:"rating"`
	Reviews        int      `json:"reviews"`
	Thumbnail      string   `json:"thumbnail"`
	Delivery       string   `json:"delivery"`
	Extensions     []string `json:"extensions"`
}

type serpMapsResponse struct {
	LocalResults []serpPlace `json:"local_results"`
	PlaceResults *serpPlace  `json:"place_results"`
	Error        string      `json:"error"`
}

type serpPlace struct {
	Title     string  `json:"title"`
	PlaceID   string  `json:"place_id"`
	Address   string  `json:"address"`
	Rating    float64 `json:"rating"`
	Reviews   int     `json:"reviews"`
	Type      string  `json:"type"`
	OpenState string  `json:"open_state"`
	Hours     string  `json:"hours"`
	Website   string  `json:"website"`
	Phone     string  `json:"phone"`
	Thumbnail string  `json:"thumbnail"`
	GPS       struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"gps_coordinates"`
}

func (s *SerpAPI) get(ctx context.Context, params url.Values, out any) error {
	params.Set("api_key", s.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/search.json?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("serpapi: build request: %w", err)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("serpapi: request failed: %w", stripURL(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("serpapi: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("serpapi: status %d: %s", resp.StatusCode, truncate(raw, 300))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("serpapi: parse response: %w", err)
	}
	return nil
}

// SearchProducts queries Google Shopping.
func (s *SerpAPI) SearchProducts(ctx context.Context, q ProductQuery) ([]Product, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 8
	}
	params := url.Values{}
	params.Set("engine", "google_shopping")
	params.Set("q", q.Query)
	params.Set("gl", s.gl)
	params.Set("hl", s.hl)
	loc := q.Location
	if loc == "" {
		loc = s.defaultLocation
	}
	params.Set("location", loc)
	// Do not send max_price: Google Shopping via SerpAPI returns an empty
	// result set when it is present. Prices are filtered client-side below.

	var resp serpShoppingResponse
	if err := s.get(ctx, params, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" && len(resp.ShoppingResults) == 0 {
		// SerpAPI reports "Google hasn't returned any results" as an error string.
		if strings.Contains(strings.ToLower(resp.Error), "hasn't returned any results") {
			return []Product{}, nil
		}
		return nil, fmt.Errorf("serpapi: %s", resp.Error)
	}

	products := make([]Product, 0, len(resp.ShoppingResults))
	for _, item := range resp.ShoppingResults {
		if item.Title == "" || item.ExtractedPrice < minPlausiblePriceMXN || excludedMerchant(item.Source) {
			continue
		}
		if q.MaxPrice > 0 && item.ExtractedPrice > q.MaxPrice {
			continue
		}
		products = append(products, mapShoppingItem(item))
		if len(products) >= limit {
			break
		}
	}
	slog.InfoContext(ctx, "serpapi: shopping", "query", q.Query, "max_price", q.MaxPrice, "raw", len(resp.ShoppingResults), "kept", len(products))
	return products, nil
}

func mapShoppingItem(item serpShoppingItem) Product {
	link := item.Link
	if link == "" {
		link = item.ProductLink
	}
	currency := "MXN"
	if strings.Contains(strings.ToUpper(item.Price), "US") {
		currency = "USD"
	}
	idSource := link
	if idSource == "" {
		idSource = item.Title + "|" + item.Source
	}
	return Product{
		ID:        "p_" + shortHash(idSource),
		SourceID:  item.ProductID,
		Title:     item.Title,
		Store:     item.Source,
		Price:     item.ExtractedPrice,
		Currency:  currency,
		Link:      link,
		Thumbnail: item.Thumbnail,
		Delivery:  item.Delivery,
		Rating:    item.Rating,
		Reviews:   item.Reviews,
		Tags:      item.Extensions,
	}
}

// FindStores queries Google Maps around a point and sorts by distance.
func (s *SerpAPI) FindStores(ctx context.Context, q StoreQuery) ([]Store, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}
	params := url.Values{}
	params.Set("engine", "google_maps")
	params.Set("type", "search")
	params.Set("q", q.Query)
	params.Set("ll", fmt.Sprintf("@%f,%f,%dz", q.Lat, q.Lng, zoomForRadius(q.RadiusM)))
	params.Set("hl", s.hl)
	params.Set("gl", s.gl)

	var resp serpMapsResponse
	if err := s.get(ctx, params, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" && len(resp.LocalResults) == 0 {
		if strings.Contains(strings.ToLower(resp.Error), "hasn't returned any results") {
			return []Store{}, nil
		}
		return nil, fmt.Errorf("serpapi: %s", resp.Error)
	}

	stores := make([]Store, 0, len(resp.LocalResults))
	for _, p := range resp.LocalResults {
		if p.Title == "" || (p.GPS.Latitude == 0 && p.GPS.Longitude == 0) {
			continue
		}
		st := mapPlace(p)
		st.DistanceM = DistanceM(q.Lat, q.Lng, st.Lat, st.Lng)
		if q.RadiusM > 0 && st.DistanceM > q.RadiusM {
			continue
		}
		stores = append(stores, st)
	}
	sort.Slice(stores, func(i, j int) bool { return stores[i].DistanceM < stores[j].DistanceM })
	if len(stores) > limit {
		stores = stores[:limit]
	}
	return stores, nil
}

func mapPlace(p serpPlace) Store {
	return Store{
		PlaceID:   p.PlaceID,
		Name:      p.Title,
		Address:   p.Address,
		Lat:       p.GPS.Latitude,
		Lng:       p.GPS.Longitude,
		Rating:    p.Rating,
		Reviews:   p.Reviews,
		OpenState: p.OpenState,
		Hours:     p.Hours,
		Website:   p.Website,
		Phone:     p.Phone,
		Category:  p.Type,
		Thumbnail: p.Thumbnail,
	}
}

// Geocode resolves a free-text place ("Roma Norte, CDMX") to coordinates.
func (s *SerpAPI) Geocode(ctx context.Context, query string) (*Geo, error) {
	params := url.Values{}
	params.Set("engine", "google_maps")
	params.Set("type", "search")
	params.Set("q", query)
	params.Set("hl", s.hl)
	params.Set("gl", s.gl)

	var resp serpMapsResponse
	if err := s.get(ctx, params, &resp); err != nil {
		return nil, err
	}
	var place *serpPlace
	switch {
	case resp.PlaceResults != nil && (resp.PlaceResults.GPS.Latitude != 0 || resp.PlaceResults.GPS.Longitude != 0):
		place = resp.PlaceResults
	case len(resp.LocalResults) > 0:
		place = &resp.LocalResults[0]
	default:
		return nil, fmt.Errorf("serpapi: no location found for %q", query)
	}
	label := place.Title
	if place.Address != "" {
		label = place.Title + ", " + place.Address
	}
	return &Geo{Lat: place.GPS.Latitude, Lng: place.GPS.Longitude, Label: label}, nil
}

func zoomForRadius(radiusM int) int {
	switch {
	case radiusM <= 0:
		return 14
	case radiusM <= 1000:
		return 16
	case radiusM <= 2500:
		return 15
	case radiusM <= 6000:
		return 14
	default:
		return 13
	}
}

func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// stripURL drops the request URL from transport errors so the api_key query
// parameter never reaches logs.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// minPlausiblePriceMXN filters out mis-parsed or junk listings.
const minPlausiblePriceMXN = 60

// excludedMerchants are cross-border or second-hand marketplaces whose
// listings are not something the user can buy nearby or trust on fit.
var excludedMerchants = []string{"ebay", "aliexpress", "wish", "alibaba", "dhgate"}

func excludedMerchant(source string) bool {
	src := strings.ToLower(source)
	for _, m := range excludedMerchants {
		if strings.Contains(src, m) {
			return true
		}
	}
	return false
}

type serpProductResponse struct {
	ProductResults struct {
		Title      string   `json:"title"`
		Thumbnails []string `json:"thumbnails"`
		Stores     []struct {
			Name             string   `json:"name"`
			Link             string   `json:"link"`
			DetailsAndOffers []string `json:"details_and_offers"`
		} `json:"stores"`
	} `json:"product_results"`
	Error string `json:"error"`
}

// ProductDetails calls the Google Product API for larger photos and the
// merchant's own link. Costs one SerpAPI search per product.
func (s *SerpAPI) ProductDetails(ctx context.Context, sourceID string) (*ProductDetails, error) {
	if strings.TrimSpace(sourceID) == "" {
		return nil, fmt.Errorf("serpapi: empty product id")
	}
	params := url.Values{}
	params.Set("engine", "google_product")
	params.Set("product_id", sourceID)
	params.Set("gl", s.gl)
	params.Set("hl", s.hl)
	var resp serpProductResponse
	if err := s.get(ctx, params, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" && len(resp.ProductResults.Thumbnails) == 0 {
		return nil, fmt.Errorf("serpapi: %s", resp.Error)
	}
	d := &ProductDetails{Images: resp.ProductResults.Thumbnails}
	if len(resp.ProductResults.Stores) > 0 {
		st := resp.ProductResults.Stores[0]
		d.MerchantLink = st.Link
		d.Store = st.Name
		d.Availability = strings.Join(st.DetailsAndOffers, " · ")
	}
	slog.InfoContext(ctx, "serpapi: product details", "product_id", sourceID, "images", len(d.Images), "merchant_link", d.MerchantLink != "")
	return d, nil
}
