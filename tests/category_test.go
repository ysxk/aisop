package tests

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ysxk/aisop/internal/database"
	"github.com/ysxk/aisop/internal/testutil"
)

func TestCategoryLifecycle(t *testing.T) {
	f := setup(t)
	empty := f.json("GET", "/api/v1/categories", "", nil, 200)
	if empty.object["total"] != float64(0) || len(empty.object["items"].([]any)) != 0 {
		t.Fatal("empty categories")
	}
	for _, body := range []map[string]any{{}, {"name": " "}, {"name": nil}, {"name": "鞋", "unknown": 1}, {"name": "鞋", "sort_order": -1}} {
		f.json("POST", "/api/v1/admin/categories", f.token, body, 400)
	}
	a := f.json("POST", "/api/v1/admin/categories", f.token, map[string]any{"name": " 鞋靴 ", "description": "鞋靴说明", "sort_order": 20}, 201)
	aid := str(a, "id")
	ap := "/api/v1/admin/categories/" + aid
	if str(a, "name") != "鞋靴" || a.header.Get("Location") != ap {
		t.Fatal("category create response")
	}
	bid := str(f.json("POST", "/api/v1/admin/categories", f.token, map[string]any{"name": "服饰", "sort_order": 10}, 201), "id")
	f.json("POST", "/api/v1/admin/categories", f.token, map[string]any{"name": "鞋靴"}, 409)
	f.json("PATCH", "/api/v1/admin/categories/"+bid, f.token, map[string]any{"name": "鞋靴"}, 409)
	for _, prefix := range []string{"/api/v1/categories", "/api/v1/admin/categories"} {
		token := ""
		if strings.Contains(prefix, "/admin/") {
			token = f.token
		}
		page := f.json("GET", prefix+"?page=1&limit=1", token, nil, 200)
		if page.object["total"] != float64(2) || page.object["items"].([]any)[0].(map[string]any)["id"] != bid {
			t.Fatal("category sort/pagination")
		}
		page = f.json("GET", prefix+"?page=2&limit=1", token, nil, 200)
		if page.object["items"].([]any)[0].(map[string]any)["id"] != aid {
			t.Fatal("category second page")
		}
		f.json("GET", prefix+"/"+aid, token, nil, 200)
		f.json("GET", prefix+"?limit=101", token, nil, 400)
		f.json("GET", prefix+"/invalid", token, nil, 400)
		f.json("GET", prefix+"/"+uuid.NewString(), token, nil, 404)
	}
	for _, body := range []map[string]any{{}, {"name": ""}, {"description": nil}, {"sort_order": nil}, {"sort_order": 1.5}, {"name": strings.Repeat("鞋", 101)}} {
		f.json("PATCH", ap, f.token, body, 400)
	}
	changed := f.json("PATCH", ap, f.token, map[string]any{"name": "运动鞋", "description": "", "sort_order": 0}, 200)
	if changed.object["description"] != "" || changed.object["sort_order"] != float64(0) {
		t.Fatal("category zero patch")
	}
	missing := uuid.NewString()
	body := productBody("CATEGORY-PUBLISHED")
	body["category_id"] = aid
	body["status"] = "published"
	p := f.json("POST", "/api/v1/admin/products", f.token, body, 201)
	pid := str(p, "id")
	pp := "/api/v1/admin/products/" + pid
	if p.object["category_id"] != aid {
		t.Fatal("category missing on product")
	}
	body = productBody("CATEGORY-DRAFT")
	body["category_id"] = aid
	draft := str(f.json("POST", "/api/v1/admin/products", f.token, body, 201), "id")
	uncategorized := f.create("UNCATEGORIZED")
	f.json("PATCH", "/api/v1/admin/products/"+uncategorized, f.token, map[string]any{"status": "published"}, 200)
	for _, tc := range []struct {
		path, token string
		total       float64
	}{{"/api/v1/products?category_id=" + aid, "", 1}, {"/api/v1/products?category_id=" + aid + "&status=draft", "", 1}, {"/api/v1/admin/products?category_id=" + aid, f.token, 2}, {"/api/v1/admin/products?category_id=" + aid + "&status=draft&q=CATEGORY", f.token, 1}, {"/api/v1/products?category_id=none", "", 1}, {"/api/v1/admin/products?category_id=none", f.token, 1}, {"/api/v1/products?category_id=" + missing, "", 0}} {
		got := f.json("GET", tc.path, tc.token, nil, 200)
		if got.object["total"] != tc.total {
			t.Fatalf("filter %s: %v", tc.path, got.object)
		}
	}
	for _, prefix := range []string{"/api/v1/products", "/api/v1/admin/products"} {
		for _, query := range []string{"?category_id=invalid", "?category_id=", "?category_id=none&category_id=" + aid} {
			f.json("GET", prefix+query, f.token, nil, 400)
		}
	}
	for _, value := range []any{"invalid", nil} {
		f.json("PATCH", pp, f.token, map[string]any{"category_id": value}, 400)
	}
	f.json("PATCH", pp, f.token, map[string]any{"category_id": missing, "name": "must not persist"}, 409)
	unchanged := f.json("GET", pp, f.token, nil, 200)
	if unchanged.object["category_id"] != aid || unchanged.object["name"] != p.object["name"] {
		t.Fatal("invalid category patch not atomic")
	}
	body = productBody("BAD-CATEGORY")
	body["category_id"] = missing
	f.json("POST", "/api/v1/admin/products", f.token, body, 409)
	kept := f.json("PATCH", pp, f.token, map[string]any{"stock": 0}, 200)
	if kept.object["category_id"] != aid {
		t.Fatal("omitted category was cleared")
	}
	f.json("DELETE", ap, f.token, nil, 409)
	var wg sync.WaitGroup
	for _, body := range []map[string]any{{"category_id": bid}, {"name": "updated name"}} {
		wg.Add(1)
		go func(body map[string]any) { defer wg.Done(); f.json("PATCH", pp, f.token, body, 200) }(body)
	}
	wg.Wait()
	moved := f.json("GET", "/api/v1/products/"+pid, "", nil, 200)
	if moved.object["category_id"] != bid || moved.object["name"] != "updated name" {
		t.Fatal("concurrent category/name patch lost update")
	}
	f.json("DELETE", ap, f.token, nil, 409)
	f.json("PATCH", "/api/v1/admin/products/"+draft, f.token, map[string]any{"status": "archived"}, 200)
	f.json("DELETE", ap, f.token, nil, 409)
	cleared := f.json("PATCH", "/api/v1/admin/products/"+draft, f.token, map[string]any{"category_id": ""}, 200)
	if cleared.object["category_id"] != "" {
		t.Fatal("category not cleared")
	}
	f.json("DELETE", ap, f.token, nil, 204)
	f.json("GET", "/api/v1/categories/"+aid, "", nil, 404)
	f.json("DELETE", ap, f.token, nil, 404)
	f.json("DELETE", "/api/v1/admin/categories/"+bid, f.token, nil, 409)
	f.json("PATCH", pp, f.token, map[string]any{"category_id": ""}, 200)
	f.json("DELETE", "/api/v1/admin/categories/"+bid, f.token, nil, 204)
	got := f.json("GET", "/api/v1/products?category_id=none", "", nil, 200)
	if got.object["total"] != float64(2) {
		t.Fatal("category deletion changed products")
	}
}

func TestCategoryMigrationPreservesExistingProducts(t *testing.T) {
	pool := testutil.Database(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `ALTER TABLE products DROP COLUMN category_id; DROP TABLE categories; DELETE FROM schema_migrations WHERE name='003_categories.sql'`); err != nil {
		t.Fatal(err)
	}
	var id string
	var updated time.Time
	if err := pool.QueryRow(ctx, `INSERT INTO products(sku,name,description,price_minor,stock,status) VALUES ('LEGACY','原有商品','原有说明',12345,7,'published') RETURNING id::text,updated_at`).Scan(&id, &updated); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := database.Migrate(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	var name, description, status string
	var price, stock int64
	var categoryID *string
	var after time.Time
	if err := pool.QueryRow(ctx, `SELECT name,description,price_minor,stock,status,category_id::text,updated_at FROM products WHERE id=$1`, id).Scan(&name, &description, &price, &stock, &status, &categoryID, &after); err != nil {
		t.Fatal(err)
	}
	if name != "原有商品" || description != "原有说明" || price != 12345 || stock != 7 || status != "published" || categoryID != nil || !updated.Equal(after) {
		t.Fatal("migration changed existing product")
	}
}
