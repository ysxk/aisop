package product

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ysxk/aisop/internal/httpapi"
)

func TestAdminRoutesRequireAuthentication(t *testing.T) {
	mux := http.NewServeMux()
	wrapped := 0
	Register(mux, nil, func(next http.Handler) http.Handler {
		wrapped++
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { httpapi.Fail(w, r, httpapi.Unauthorized()) })
	})
	if wrapped != 6 {
		t.Fatalf("wrapped %d routes, want 6", wrapped)
	}
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/admin/products"}, {"POST", "/api/v1/admin/products"},
		{"GET", "/api/v1/admin/products/bad"}, {"PATCH", "/api/v1/admin/products/bad"},
		{"DELETE", "/api/v1/admin/products/bad"}, {"PUT", "/api/v1/admin/products/bad/media"},
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(route.method, route.path, nil))
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d", route.method, route.path, recorder.Code)
		}
	}
	for _, path := range []string{"/api/v1/products?page=0", "/api/v1/products/bad"} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("public %s = %d", path, recorder.Code)
		}
	}
}

func TestInvalidPayloads(t *testing.T) {
	const productPath = "/api/v1/admin/products/a9c4d18c-65de-4a48-9a40-8f3c3406d877"
	mux := http.NewServeMux()
	Register(mux, nil, func(next http.Handler) http.Handler { return next })
	for _, tt := range []struct {
		name, method, path, body, contentType string
		status                                int
	}{
		{"required create fields", "POST", "/api/v1/admin/products", `{}`, "application/json", 400},
		{"required price", "POST", "/api/v1/admin/products", `{"sku":"s","name":"n"}`, "application/json", 400},
		{"null price", "POST", "/api/v1/admin/products", `{"sku":"s","name":"n","price_minor":null}`, "application/json", 400},
		{"empty patch", "PATCH", productPath, `{}`, "application/json", 400},
		{"null patch", "PATCH", productPath, `{"stock":null}`, "application/json", 400},
		{"unknown field", "PATCH", productPath, `{"id":"bad"}`, "application/json", 400},
		{"overflow", "PATCH", productPath, `{"price_minor":9223372036854775808}`, "application/json", 400},
		{"fraction", "PATCH", productPath, `{"price_minor":1.5}`, "application/json", 400},
		{"missing media array", "PUT", productPath + "/media", `{}`, "application/json", 400},
		{"null media array", "PUT", productPath + "/media", `{"media_ids":null}`, "application/json", 400},
		{"null cover", "PUT", productPath + "/media", `{"media_ids":[],"cover_media_id":null}`, "application/json", 400},
		{"null array element", "PUT", productPath + "/media", `{"media_ids":[null]}`, "application/json", 400},
		{"unsupported content type", "PATCH", productPath, `{}`, "text/plain", 415},
		{"multiple objects", "PATCH", productPath, `{} {}`, "application/json", 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			r.Header.Set("Content-Type", tt.contentType)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("status = %d, want %d; %s", w.Code, tt.status, w.Body.String())
			}
		})
	}
}

func TestProductPayloadPointers(t *testing.T) {
	r := httptest.NewRequest("PATCH", "/", strings.NewReader(`{"price_minor":0,"stock":0,"description":""}`))
	r.Header.Set("Content-Type", "application/json")
	var in productInput
	if err := httpapi.Decode(httptest.NewRecorder(), r, &in); err != nil {
		t.Fatal(err)
	}
	if in.PriceMinor == nil || *in.PriceMinor != 0 || in.Stock == nil || *in.Stock != 0 || in.Description == nil || *in.Description != "" || in.SKU != nil {
		t.Fatalf("pointer semantics lost: %+v", in)
	}
}

func TestEmptyMediaAndItemsAreArrays(t *testing.T) {
	for _, value := range []any{Product{Media: []Media{}}, listResult{Items: []Product{}}} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), ":[]") {
			t.Fatalf("empty array missing: %s", data)
		}
	}
}
