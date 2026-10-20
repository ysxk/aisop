package product

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCategoryInputAndFilter(t *testing.T) {
	const id = "5b2c4455-fba9-4a79-a3e3-f04a701171d6"
	for _, tc := range []struct {
		value string
		valid bool
	}{{"", true}, {id, true}, {strings.ToUpper(id), true}, {"invalid", false}, {"none", false}} {
		in := productInput{CategoryID: new(tc.value)}
		err := validateProduct(in, false)
		if (err == nil) != tc.valid {
			t.Fatalf("category %q: %v", tc.value, err)
		}
		if tc.valid && tc.value != "" && *in.CategoryID != id {
			t.Fatal("UUID not normalized")
		}
	}
	for _, query := range []string{"?category_id=", "?category_id=invalid", "?category_id=none&category_id=" + id} {
		if _, err := parseFilter(httptest.NewRequest("GET", "/"+query, nil), false); err == nil {
			t.Fatalf("invalid filter accepted: %s", query)
		}
	}
	for _, tc := range []struct {
		query string
		want  *string
	}{{"", nil}, {"?category_id=none", new("")}, {"?category_id=" + strings.ToUpper(id), new(id)}} {
		filter, err := parseFilter(httptest.NewRequest("GET", "/"+tc.query, nil), false)
		if err != nil {
			t.Fatal(err)
		}
		if filter.Status != "published" {
			t.Fatal("public status bypass")
		}
		if (filter.CategoryID == nil) != (tc.want == nil) {
			t.Fatal("filter presence")
		}
		if tc.want != nil && *filter.CategoryID != *tc.want {
			t.Fatal("filter value")
		}
	}
	sql, args := productWhere(listFilter{Status: "published", CategoryID: new(id)})
	if sql != " WHERE status = $1 AND category_id = $2" || len(args) != 2 || args[1] != id {
		t.Fatal(sql, args)
	}
	sql, args = productWhere(listFilter{CategoryID: new("")})
	if sql != " WHERE category_id IS NULL" || len(args) != 0 {
		t.Fatal(sql, args)
	}
}
