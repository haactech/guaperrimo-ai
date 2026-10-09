// Package shopping finds real, purchasable products and physical stores near
// the user. It is the agent's only source of products.
package shopping

import "context"

// Product is a purchasable article found online.
type Product struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Store        string    `json:"store"` // merchant name as reported by the search engine
	Price        float64   `json:"price"`
	Currency     string    `json:"currency"`
	Link         string    `json:"link"`
	Thumbnail    string    `json:"thumbnail,omitempty"`
	LargeImage   string    `json:"large_image,omitempty"`  // high-resolution product photo, resolved lazily
	SourceID     string    `json:"source_id,omitempty"`    // search engine product id, for detail lookups
	Availability string    `json:"availability,omitempty"` // e.g. "En stock para compras en línea"
	Delivery     string    `json:"delivery,omitempty"`
	Rating       float64   `json:"rating,omitempty"`
	Reviews      int       `json:"reviews,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	NearbyStore  *StoreRef `json:"nearby_store,omitempty"` // set when the merchant has a physical store near the user
}

// StoreRef points at a physical store found earlier in the session.
type StoreRef struct {
	PlaceID   string `json:"place_id"`
	Name      string `json:"name"`
	DistanceM int    `json:"distance_m"`
}

// Store is a physical shop near the user.
type Store struct {
	PlaceID   string  `json:"place_id"`
	Name      string  `json:"name"`
	Address   string  `json:"address"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	DistanceM int     `json:"distance_m"`
	Rating    float64 `json:"rating,omitempty"`
	Reviews   int     `json:"reviews,omitempty"`
	OpenState string  `json:"open_state,omitempty"`
	Hours     string  `json:"hours,omitempty"`
	Website   string  `json:"website,omitempty"`
	Phone     string  `json:"phone,omitempty"`
	Category  string  `json:"category,omitempty"`
	Thumbnail string  `json:"thumbnail,omitempty"`
}

// Geo is a resolved location.
type Geo struct {
	Lat   float64 `json:"lat"`
	Lng   float64 `json:"lng"`
	Label string  `json:"label"`
}

// ProductQuery describes a product search.
type ProductQuery struct {
	Query    string
	MaxPrice float64 // 0 = no cap
	Location string  // free-text search origin, e.g. "Mexico City, Mexico"
	Limit    int
}

// StoreQuery describes a nearby-store search around a point.
type StoreQuery struct {
	Query   string
	Lat     float64
	Lng     float64
	RadiusM int // 0 = no filter
	Limit   int
}

// Provider is implemented by search backends (SerpAPI, fake).
type Provider interface {
	SearchProducts(ctx context.Context, q ProductQuery) ([]Product, error)
	FindStores(ctx context.Context, q StoreQuery) ([]Store, error)
	Geocode(ctx context.Context, query string) (*Geo, error)
	Name() string
}

// ProductDetails is what a detail lookup adds on top of a search result.
type ProductDetails struct {
	Images       []string `json:"images"`                  // larger photos, best first
	MerchantLink string   `json:"merchant_link,omitempty"` // the store's own product page
	Availability string   `json:"availability,omitempty"`
	Store        string   `json:"store,omitempty"`
}

// DetailResolver fetches product details by the engine's product id.
type DetailResolver interface {
	ProductDetails(ctx context.Context, sourceID string) (*ProductDetails, error)
}
