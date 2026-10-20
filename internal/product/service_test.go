package product

import (
	"math"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func ptr[T any](value T) *T { return &value }

func TestValidateProduct(t *testing.T) {
	for _, tt := range []struct {
		name   string
		input  productInput
		create bool
		valid  bool
	}{
		{"required", productInput{}, true, false},
		{"missing price", productInput{SKU: ptr("sku"), Name: ptr("name")}, true, false},
		{"free product", productInput{SKU: ptr("sku"), Name: ptr("name"), PriceMinor: ptr(int64(0))}, true, true},
		{"empty patch", productInput{}, false, false},
		{"zero patch", productInput{PriceMinor: ptr(int64(0)), Stock: ptr(int64(0))}, false, true},
		{"clear description", productInput{Description: ptr("")}, false, true},
		{"negative price", productInput{PriceMinor: ptr(int64(-1))}, false, false},
		{"negative stock", productInput{Stock: ptr(int64(-1))}, false, false},
		{"large integers", productInput{Stock: ptr(int64(math.MaxInt64)), PriceMinor: ptr(int64(math.MaxInt64))}, false, true},
		{"blank sku", productInput{SKU: ptr(" \t")}, false, false},
		{"blank name", productInput{Name: ptr(" ")}, false, false},
		{"sku unicode boundary", productInput{SKU: ptr(strings.Repeat("中", 100))}, false, true},
		{"sku too long", productInput{SKU: ptr(strings.Repeat("中", 101))}, false, false},
		{"name boundary", productInput{Name: ptr(strings.Repeat("中", 200))}, false, true},
		{"name too long", productInput{Name: ptr(strings.Repeat("中", 201))}, false, false},
		{"description boundary", productInput{Description: ptr(strings.Repeat("中", 20000))}, false, true},
		{"description too long", productInput{Description: ptr(strings.Repeat("中", 20001))}, false, false},
		{"uppercase currency", productInput{Currency: ptr("USD")}, false, true},
		{"lowercase currency", productInput{Currency: ptr("cny")}, false, false},
		{"short currency", productInput{Currency: ptr("CN")}, false, false},
		{"nonletter currency", productInput{Currency: ptr("CN1")}, false, false},
		{"draft", productInput{Status: ptr("draft")}, false, true},
		{"published", productInput{Status: ptr("published")}, false, true},
		{"archived", productInput{Status: ptr("archived")}, false, true},
		{"invalid status", productInput{Status: ptr("deleted")}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateProduct(tt.input, tt.create); (err == nil) != tt.valid {
				t.Fatalf("validateProduct = %v, want valid=%v", err, tt.valid)
			}
		})
	}
}

func TestParseFilter(t *testing.T) {
	for _, tt := range []struct {
		query       string
		admin       bool
		valid       bool
		status      string
		page, limit int
	}{
		{"", false, true, "published", 1, 20},
		{"status=draft", false, true, "published", 1, 20},
		{"status=invalid&status=draft", false, true, "published", 1, 20},
		{"", true, true, "", 1, 20},
		{"status=archived&page=1000000&limit=100", true, true, "archived", 1000000, 100},
		{"status=", true, false, "", 0, 0},
		{"status=bad", true, false, "", 0, 0},
		{"status=draft&status=published", true, false, "", 0, 0},
		{"page=0", false, false, "", 0, 0},
		{"page=1000001", false, false, "", 0, 0},
		{"page=1&page=2", false, false, "", 0, 0},
		{"limit=101", false, false, "", 0, 0},
		{"limit=-1", false, false, "", 0, 0},
		{"q=x&q=y", false, false, "", 0, 0},
		{"q=" + url.QueryEscape(strings.Repeat("中", 200)), false, true, "published", 1, 20},
		{"q=" + url.QueryEscape(strings.Repeat("中", 201)), false, false, "", 0, 0},
	} {
		t.Run(tt.query, func(t *testing.T) {
			filter, err := parseFilter(httptest.NewRequest("GET", "/?"+tt.query, nil), tt.admin)
			if (err == nil) != tt.valid {
				t.Fatalf("parseFilter = %v, want valid=%v", err, tt.valid)
			}
			if tt.valid && (filter.Status != tt.status || filter.Page.Page != tt.page || filter.Limit != tt.limit) {
				t.Fatalf("unexpected filter: %+v", filter)
			}
		})
	}
}

func TestNormalizeAssociation(t *testing.T) {
	const id = "a9c4d18c-65de-4a48-9a40-8f3c3406d877"
	const other = "119ee6dc-57c4-4a5d-a79b-81291412e81b"
	for _, tt := range []struct {
		name  string
		in    associationInput
		valid bool
	}{
		{"missing", associationInput{}, false},
		{"clear", associationInput{MediaIDs: ptr([]string{})}, true},
		{"ids", associationInput{MediaIDs: ptr([]string{id, other})}, true},
		{"normalized cover", associationInput{MediaIDs: ptr([]string{strings.ToUpper(id)}), CoverMediaID: strings.ReplaceAll(id, "-", "")}, true},
		{"cover absent", associationInput{MediaIDs: ptr([]string{id}), CoverMediaID: other}, false},
		{"bad cover", associationInput{MediaIDs: ptr([]string{id}), CoverMediaID: "bad"}, false},
		{"bad id", associationInput{MediaIDs: ptr([]string{"bad"})}, false},
		{"duplicate", associationInput{MediaIDs: ptr([]string{id, id})}, false},
		{"normalized duplicate", associationInput{MediaIDs: ptr([]string{id, strings.ToUpper(id)})}, false},
		{"compact duplicate", associationInput{MediaIDs: ptr([]string{id, strings.ReplaceAll(id, "-", "")})}, false},
		{"too many", associationInput{MediaIDs: ptr(make([]string, 101))}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ids, cover, err := normalizeAssociation(tt.in)
			if (err == nil) != tt.valid {
				t.Fatalf("normalizeAssociation = %v, want valid=%v", err, tt.valid)
			}
			if tt.name == "normalized cover" && (ids[0] != id || cover != id) {
				t.Fatalf("not normalized: %v, %s", ids, cover)
			}
			if tt.name == "clear" && ids == nil {
				t.Fatal("clear must produce an empty array")
			}
		})
	}
}
