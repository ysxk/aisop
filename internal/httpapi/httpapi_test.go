package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{{`{"stock":0}`, true}, {`{}`, true}, {`{"stock":null}`, false}, {`{"unknown":1}`, false}, {`{"stock":1} {}`, false}, {`null`, false}, {`[]`, false}, {`{"stock":1.2}`, false}, {`{"stock":9223372036854775808}`, false}} {
		t.Run(tc.body, func(t *testing.T) {
			var dst struct {
				Stock *int64 `json:"stock"`
			}
			r := httptest.NewRequest("PATCH", "/", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			err := Decode(httptest.NewRecorder(), r, &dst)
			if (err == nil) != tc.valid {
				t.Fatalf("got %v", err)
			}
			if tc.body == `{"stock":0}` && (dst.Stock == nil || *dst.Stock != 0) {
				t.Fatal("zero lost")
			}
		})
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{}`))
	if err := Decode(httptest.NewRecorder(), r, new(any)); err == nil {
		t.Fatal("missing content type")
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat(" ", 1<<20)+"{}"))
	r.Header.Set("Content-Type", "application/json")
	if err := Decode(httptest.NewRecorder(), r, new(any)); err.(*Error).Status != 413 {
		t.Fatal(err)
	}
}
func TestPagination(t *testing.T) {
	for _, query := range []string{"?page=0", "?limit=101", "?page=x", "?page=1&page=2", "?page=1000001", "?limit="} {
		if _, err := Pagination(httptest.NewRequest("GET", "/"+query, nil)); err == nil {
			t.Fatal(query)
		}
	}
	p, err := Pagination(httptest.NewRequest("GET", "/?page=2&limit=7", nil))
	if err != nil || p.Offset() != 7 {
		t.Fatal(p, err)
	}
}
