package shopping

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

const shoppingFixture = `{"shopping_results":[
 {"position":1,"title":"Camisa de lino azul marino","product_id":"1","product_link":"https://www.google.com/shopping/product/1","source":"Zara MX","price":"$899.00","extracted_price":899.0,"thumbnail":"https://img/1.jpg","delivery":"Envío gratis","extensions":["Nearby, 2 km"]},
 {"position":2,"title":"Camisa cara","product_link":"https://www.google.com/shopping/product/2","link":"https://tienda.mx/2","source":"Liverpool","price":"$2,499.00","extracted_price":2499.0,"thumbnail":"https://img/2.jpg"},
 {"position":3,"title":"Camisa importada","product_link":"https://www.google.com/shopping/product/3","source":"Amazon.com","price":"US$40.00","extracted_price":740.0}
]}`

const mapsFixture = `{"local_results":[
 {"position":1,"title":"Zara Parque Delta","place_id":"pd1","address":"Av. Cuauhtémoc 462","rating":4.1,"reviews":2000,"type":"Tienda de ropa","open_state":"Abierto","gps_coordinates":{"latitude":19.4040,"longitude":-99.1560}},
 {"position":2,"title":"H&M Reforma","place_id":"pd2","address":"Reforma 222","rating":4.0,"reviews":900,"type":"Tienda de ropa","gps_coordinates":{"latitude":19.4290,"longitude":-99.1600}},
 {"position":3,"title":"Liverpool Perisur","place_id":"pd3","address":"Periférico Sur","gps_coordinates":{"latitude":19.3040,"longitude":-99.1900}}
]}`

const geocodeFixture = `{"place_results":{"title":"Roma Norte","address":"Ciudad de México, CDMX","gps_coordinates":{"latitude":19.4194,"longitude":-99.1616}}}`

func newSerpServer(t *testing.T, got *[]url.Values) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		*got = append(*got, q)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case q.Get("engine") == "google_shopping":
			_, _ = w.Write([]byte(shoppingFixture))
		case q.Get("engine") == "google_maps" && q.Get("ll") != "":
			_, _ = w.Write([]byte(mapsFixture))
		case q.Get("engine") == "google_maps":
			_, _ = w.Write([]byte(geocodeFixture))
		default:
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"bad engine"}`))
		}
	}))
}

func TestSearchProductsMapsAndFilters(t *testing.T) {
	var got []url.Values
	srv := newSerpServer(t, &got)
	defer srv.Close()
	s := NewSerpAPI("key", "Mexico").WithBaseURL(srv.URL)

	products, err := s.SearchProducts(context.Background(), ProductQuery{Query: "camisa lino hombre", MaxPrice: 1000, Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	q := got[0]
	if q.Get("engine") != "google_shopping" || q.Get("gl") != "mx" || q.Get("hl") != "es" || q.Get("location") != "Mexico" || q.Get("api_key") != "key" {
		t.Fatalf("unexpected params: %v", q)
	}
	if q.Has("max_price") {
		t.Fatalf("max_price must not be sent to SerpAPI (it empties the results); filter client-side")
	}
	if len(products) != 2 {
		t.Fatalf("expected the 2499 item filtered out, got %d products", len(products))
	}
	p := products[0]
	if p.Store != "Zara MX" || p.Price != 899 || p.Currency != "MXN" || p.Link != "https://www.google.com/shopping/product/1" || p.Thumbnail != "https://img/1.jpg" || p.Delivery != "Envío gratis" {
		t.Errorf("mapping: %+v", p)
	}
	if len(p.Tags) != 1 || p.Tags[0] != "Nearby, 2 km" {
		t.Errorf("extensions not kept: %v", p.Tags)
	}
	if products[1].Currency != "USD" {
		t.Errorf("US$ price should be USD, got %s", products[1].Currency)
	}
	if p.ID == "" || p.ID == products[1].ID {
		t.Errorf("ids must be stable and unique: %s %s", p.ID, products[1].ID)
	}
}

func TestFindStoresSortsAndFilters(t *testing.T) {
	var got []url.Values
	srv := newSerpServer(t, &got)
	defer srv.Close()
	s := NewSerpAPI("key", "Mexico").WithBaseURL(srv.URL)

	stores, err := s.FindStores(context.Background(), StoreQuery{Query: "tienda de ropa hombre", Lat: 19.4194, Lng: -99.1616, RadiusM: 3000})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got[0].Get("ll") == "" || got[0].Get("type") != "search" {
		t.Fatalf("maps params: %v", got[0])
	}
	if len(stores) != 2 {
		t.Fatalf("expected Perisur (13 km) filtered out, got %d", len(stores))
	}
	if stores[0].PlaceID != "pd2" || stores[1].PlaceID != "pd1" {
		t.Errorf("not sorted by distance: %s, %s", stores[0].PlaceID, stores[1].PlaceID)
	}
	if stores[0].DistanceM <= 0 || stores[0].DistanceM > 1200 {
		t.Errorf("distance for H&M Reforma = %d", stores[0].DistanceM)
	}
	if stores[1].OpenState != "Abierto" || stores[1].Address == "" {
		t.Errorf("place mapping: %+v", stores[1])
	}
}

func TestGeocodeUsesPlaceResults(t *testing.T) {
	var got []url.Values
	srv := newSerpServer(t, &got)
	defer srv.Close()
	s := NewSerpAPI("key", "Mexico").WithBaseURL(srv.URL)

	geo, err := s.Geocode(context.Background(), "Roma Norte, CDMX")
	if err != nil {
		t.Fatalf("geocode: %v", err)
	}
	if geo.Lat != 19.4194 || geo.Lng != -99.1616 || geo.Label != "Roma Norte, Ciudad de México, CDMX" {
		t.Errorf("geo: %+v", geo)
	}
}

func TestFakeIsDeterministic(t *testing.T) {
	f := Fake{}
	a, _ := f.SearchProducts(context.Background(), ProductQuery{Query: "camisa hombre", Limit: 3})
	b, _ := f.SearchProducts(context.Background(), ProductQuery{Query: "camisa hombre", Limit: 3})
	if len(a) != 3 || a[0].ID != b[0].ID {
		t.Fatalf("fake products not deterministic: %+v", a)
	}
	stores, _ := f.FindStores(context.Background(), StoreQuery{Lat: 19.4, Lng: -99.1, RadiusM: 1500})
	if len(stores) < 2 {
		t.Fatalf("fake stores within 1.5 km: %d", len(stores))
	}
	AnnotateNearby(a, stores)
	if a[0].NearbyStore == nil {
		t.Errorf("Zara MX should match the fake Zara store")
	}
}

func TestExcludedMerchantsAndJunkPrices(t *testing.T) {
	if !excludedMerchant("eBay - tienda_mx") || excludedMerchant("Liverpool") {
		t.Fatal("merchant exclusion list misbehaves")
	}
	p := mapShoppingItem(serpShoppingItem{Title: "x", Source: "eBay", ExtractedPrice: 45})
	if p.Price != 45 || p.Store != "eBay" {
		t.Fatal("mapping should be untouched; filtering happens in SearchProducts")
	}
}

func TestProductDetailsParsesThumbnailsAndStore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("engine") != "google_product" || r.URL.Query().Get("product_id") != "123" {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`{"product_results":{"title":"Camisa","thumbnails":["https://img/a.jpg","https://img/b.jpg"],"stores":[{"name":"Massimo Dutti","link":"https://www.massimodutti.com/mx/x","details_and_offers":["En stock para compras en línea","Entrega gratuita"]}]}}`))
	}))
	defer srv.Close()
	s := NewSerpAPI("key", "Mexico").WithBaseURL(srv.URL)
	d, err := s.ProductDetails(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Images) != 2 || d.MerchantLink != "https://www.massimodutti.com/mx/x" || d.Store != "Massimo Dutti" || d.Availability != "En stock para compras en línea · Entrega gratuita" {
		t.Fatalf("details: %+v", d)
	}
}
