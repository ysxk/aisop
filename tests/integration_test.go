package tests

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysxk/aisop/api"
	"github.com/ysxk/aisop/internal/app"
	"github.com/ysxk/aisop/internal/config"
	"github.com/ysxk/aisop/internal/testutil"
)

const password = "test-password-only-123"

type fixture struct {
	t      *testing.T
	pool   *pgxpool.Pool
	config config.Config
	app    *app.App
	server *httptest.Server
	spec   *openapi3.T
	token  string
}
type response struct {
	body   []byte
	header http.Header
	object map[string]any
}

func setup(t *testing.T) *fixture {
	t.Helper()
	pool := testutil.Database(t)
	spec, err := openapi3.NewLoader().LoadFromData(api.OpenAPI)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t, pool: pool, spec: spec, config: config.Config{MediaDir: t.TempDir(), SessionTTL: time.Hour, ImageMaxBytes: 1024, VideoMaxBytes: 2048, AllowedOrigins: []string{"https://shop.example"}}}
	f.start()
	t.Cleanup(func() { f.server.Close(); f.app.Close() })
	if err = f.app.Auth.Bootstrap(context.Background(), "admin", password); err != nil {
		t.Fatal(err)
	}
	if err = f.app.Auth.Bootstrap(context.Background(), "another-admin", password); err == nil {
		t.Fatal("bootstrap overwrote singleton")
	}
	f.token = f.login(password)
	return f
}
func (f *fixture) start() {
	f.t.Helper()
	a, err := app.New(f.config, f.pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		f.t.Fatal(err)
	}
	f.app = a
	f.server = httptest.NewServer(a.Handler)
}
func (f *fixture) login(pass string) string {
	f.t.Helper()
	r := f.json("POST", "/api/v1/admin/auth/login", "", map[string]any{"username": "admin", "password": pass}, 200)
	return r.object["token"].(string)
}
func (f *fixture) json(method, path, token string, body any, status int) response {
	f.t.Helper()
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
	}
	return f.request(method, path, token, bytes.NewReader(data), map[string]string{"Content-Type": "application/json"}, status)
}
func (f *fixture) request(method, path, token string, body io.Reader, headers map[string]string, status int) response {
	f.t.Helper()
	req, err := http.NewRequest(method, f.server.URL+path, body)
	if err != nil {
		f.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := f.server.Client().Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		f.t.Fatal(err)
	}
	if res.StatusCode != status {
		f.t.Fatalf("%s %s: got %d want %d: %s", method, path, res.StatusCode, status, data)
	}
	if res.Header.Get("X-Request-ID") == "" {
		f.t.Fatal("request ID missing")
	}
	r := response{body: data, header: res.Header}
	if method != "HEAD" && status != 204 && status != 304 && strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		if err = json.Unmarshal(data, &r.object); err != nil {
			f.t.Fatal(err)
		}
		if status >= 400 {
			e, ok := r.object["error"].(map[string]any)
			if !ok || e["request_id"] != res.Header.Get("X-Request-ID") {
				f.t.Fatal("invalid error envelope")
			}
		}
		f.validateResponse(method, req.URL.Path, status, r.object)
	}
	return r
}
func (f *fixture) validateResponse(method, path string, status int, value any) {
	f.t.Helper()
	for pattern, item := range f.spec.Paths.Map() {
		a, b := strings.Split(pattern, "/"), strings.Split(path, "/")
		if len(a) != len(b) {
			continue
		}
		match := true
		for i := range a {
			if a[i] != b[i] && a[i] != "{id}" {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		op := item.GetOperation(method)
		if op == nil {
			continue
		}
		ref := op.Responses.Value(strconv.Itoa(status))
		if ref == nil {
			ref = op.Responses.Default()
		}
		if ref == nil {
			f.t.Fatalf("undocumented status %s %s %d", method, path, status)
		}
		if media := ref.Value.Content.Get("application/json"); media != nil && media.Schema != nil {
			if err := media.Schema.Value.VisitJSON(value); err != nil {
				f.t.Fatalf("response violates OpenAPI %s %s: %v", method, path, err)
			}
		}
		return
	}
}
func (f *fixture) upload(kind, name, token string, data []byte, status int) response {
	f.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		f.t.Fatal(err)
	}
	part.Write(data)
	mw.Close()
	return f.request("POST", "/api/v1/admin/media/"+kind, token, &b, map[string]string{"Content-Type": mw.FormDataContentType()}, status)
}
func imageData(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func videoData() []byte {
	return append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'm', 'p', '4', '2', 0, 0, 0, 0, 'm', 'p', '4', '2', 'i', 's', 'o', 'm'}, bytes.Repeat([]byte{0}, 128)...)
}
func productBody(sku string) map[string]any {
	return map[string]any{"sku": sku, "name": "商品 " + sku, "price_minor": 19900, "stock": 10, "description": "说明"}
}
func str(r response, key string) string { return r.object[key].(string) }
func (f *fixture) create(sku string) string {
	return str(f.json("POST", "/api/v1/admin/products", f.token, productBody(sku), 201), "id")
}

func TestProductMediaLifecycle(t *testing.T) {
	f := setup(t)
	f.json("GET", "/healthz", "", nil, 200)
	f.json("GET", "/readyz", "", nil, 200)
	spec := f.request("GET", "/openapi.yaml", "", nil, nil, 200)
	if !bytes.Equal(spec.body, api.OpenAPI) {
		t.Fatal("embedded spec differs")
	}
	f.json("GET", "/api/v1/admin/products", "", nil, 401)
	f.json("GET", "/api/v1/admin/products", "invalid", nil, 401)
	p := f.create("SKU-100%")
	adminPath := "/api/v1/admin/products/" + p
	publicPath := "/api/v1/products/" + p
	f.json("GET", publicPath, "", nil, 404)
	listing := f.json("GET", "/api/v1/products?status=draft", "", nil, 200)
	if listing.object["total"] != float64(0) {
		t.Fatal("draft leaked")
	}
	f.json("POST", "/api/v1/admin/products", f.token, productBody("SKU-100%"), 409)
	f.create("SKU-100-other")
	search := f.json("GET", "/api/v1/admin/products?q="+url.QueryEscape("100%"), f.token, nil, 200)
	if search.object["total"] != float64(1) {
		t.Fatal("search treats % as wildcard")
	}
	page := f.json("GET", "/api/v1/admin/products?limit=1&page=2", f.token, nil, 200)
	if len(page.object["items"].([]any)) != 1 || page.object["total"] != float64(2) {
		t.Fatal("pagination")
	}
	zero := f.json("PATCH", adminPath, f.token, map[string]any{"price_minor": 0, "stock": 0, "description": ""}, 200)
	if zero.object["price_minor"] != float64(0) || zero.object["stock"] != float64(0) || zero.object["description"] != "" {
		t.Fatal("zero values lost")
	}
	for _, body := range []map[string]any{{}, {"price_minor": -1}, {"stock": -1}, {"stock": nil}, {"status": "bad"}, {"currency": "cny"}, {"name": " "}, {"unknown": 1}, {"description": "bad\x00"}} {
		f.json("PATCH", adminPath, f.token, body, 400)
	}
	for _, query := range []string{"?page=0", "?limit=101", "?status=invalid"} {
		f.json("GET", "/api/v1/admin/products"+query, f.token, nil, 400)
	}
	f.json("GET", "/api/v1/products/not-a-uuid", "", nil, 400)
	f.json("GET", "/api/v1/products/"+uuid.NewString(), "", nil, 404)
	png := imageData(t)
	mp4 := videoData()
	f.upload("images", "fake.png", f.token, []byte("<html>not image</html>"), 415)
	f.upload("images", "too-big.png", f.token, bytes.Repeat([]byte{'x'}, 1025), 413)
	f.upload("videos", "too-big.mp4", f.token, bytes.Repeat([]byte{'x'}, 2049), 413)
	f.upload("videos", "not-video.mp4", f.token, png, 415)
	f.upload("images", "image.png", "", png, 401)
	imageResp := f.upload("images", "../../photo.png", f.token, png, 201)
	imageID := str(imageResp, "id")
	videoID := str(f.upload("videos", "clip.mp4", f.token, mp4, 201), "id")
	if imageResp.object["original_name"] != "photo.png" {
		t.Fatal("unsafe filename")
	}
	if _, ok := imageResp.object["storage_key"]; ok {
		t.Fatal("storage key exposed")
	}
	imageURL := "/api/v1/media/" + imageID + "/content"
	videoURL := "/api/v1/media/" + videoID + "/content"
	f.json("GET", imageURL, "", nil, 404)
	f.request("GET", "/api/v1/admin/media/"+imageID+"/content", f.token, nil, nil, 200)
	f.request("HEAD", "/api/v1/admin/media/"+videoID+"/content", f.token, nil, nil, 200)
	f.json("PATCH", "/api/v1/admin/media/"+imageID, f.token, map[string]any{"display_name": "商品正面", "alt_text": "正面图"}, 200)
	f.json("GET", "/api/v1/admin/media/"+imageID, f.token, nil, 200)
	ml := f.json("GET", "/api/v1/admin/media?kind=video&limit=1", f.token, nil, 200)
	if ml.object["total"] != float64(1) {
		t.Fatal("media filter")
	}
	for _, body := range []map[string]any{{}, {"display_name": " "}, {"alt_text": nil}, {"unknown": true}} {
		f.json("PATCH", "/api/v1/admin/media/"+imageID, f.token, body, 400)
	}
	links := map[string]any{"media_ids": []string{videoID, imageID}, "cover_media_id": imageID}
	linked := f.json("PUT", adminPath+"/media", f.token, links, 200)
	media := linked.object["media"].([]any)
	if len(media) != 2 || media[0].(map[string]any)["id"] != videoID || media[1].(map[string]any)["is_cover"] != true {
		t.Fatal("media order/cover")
	}
	f.json("GET", videoURL, "", nil, 404)
	for _, body := range []map[string]any{{"media_ids": []string{imageID, uuid.NewString()}}, {"media_ids": []string{videoID}, "cover_media_id": videoID}, {"media_ids": []string{imageID, strings.ToUpper(imageID)}}, {"media_ids": []string{imageID}, "cover_media_id": videoID}, {"media_ids": nil}, {}} {
		f.json("PUT", adminPath+"/media", f.token, body, 400)
	}
	unchanged := f.json("GET", adminPath, f.token, nil, 200)
	if len(unchanged.object["media"].([]any)) != 2 {
		t.Fatal("failed association was not atomic")
	}
	f.json("DELETE", "/api/v1/admin/media/"+imageID, f.token, nil, 409)
	f.json("PATCH", adminPath, f.token, map[string]any{"status": "published"}, 200)
	published := f.json("GET", publicPath, "", nil, 200)
	if published.object["media"].([]any)[0].(map[string]any)["content_url"] != videoURL {
		t.Fatal("public content URL")
	}
	content := f.request("GET", imageURL, "", nil, nil, 200)
	if !bytes.Equal(content.body, png) || content.header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(content.header.Get("Cache-Control"), "must-revalidate") {
		t.Fatal("content headers/body")
	}
	head := f.request("HEAD", videoURL, "", nil, nil, 200)
	if len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(mp4)) {
		t.Fatal("HEAD")
	}
	partial := f.request("GET", videoURL, "", nil, map[string]string{"Range": "bytes=0-15"}, 206)
	if !bytes.Equal(partial.body, mp4[:16]) || partial.header.Get("Content-Range") != "bytes 0-15/152" {
		t.Fatal("Range response")
	}
	invalid := f.request("GET", videoURL, "", nil, map[string]string{"Range": "bytes=99999-"}, 416)
	if !strings.HasPrefix(invalid.header.Get("Content-Type"), "application/json") {
		t.Fatal("range errors must use JSON")
	}
	f.request("GET", imageURL, "", nil, map[string]string{"If-None-Match": content.header.Get("ETag")}, 304)
	f.server.Close()
	f.app.Close()
	f.start()
	persisted := f.json("GET", publicPath, "", nil, 200)
	if persisted.object["id"] != p {
		t.Fatal("restart persistence")
	}
	f.request("GET", imageURL, "", nil, nil, 200)
	f.json("GET", "/api/v1/admin/auth/me", f.token, nil, 200)
	f.json("PATCH", adminPath, f.token, map[string]any{"status": "archived"}, 200)
	f.json("GET", publicPath, "", nil, 404)
	f.request("GET", imageURL, "", nil, map[string]string{"If-None-Match": content.header.Get("ETag")}, 404)
	f.request("HEAD", videoURL, "", nil, nil, 404)
	f.json("PUT", adminPath+"/media", f.token, map[string]any{"media_ids": []string{}}, 200)
	f.json("DELETE", "/api/v1/admin/media/"+imageID, f.token, nil, 204)
	f.json("DELETE", "/api/v1/admin/media/"+videoID, f.token, nil, 204)
	f.json("DELETE", adminPath, f.token, nil, 204)
	f.json("GET", adminPath, f.token, nil, 404)
	files, err := os.ReadDir(f.config.MediaDir)
	if err != nil || len(files) != 0 {
		t.Fatal("files leaked", files, err)
	}
}

func TestAuthRevocationExpiryAndLimits(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var hash string
	if err := f.pool.QueryRow(ctx, `SELECT token_hash FROM sessions`).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	expected := sha256.Sum256([]byte(f.token))
	if hash == f.token || hash != hex.EncodeToString(expected[:]) {
		t.Fatal("plaintext token stored")
	}
	f.json("GET", "/api/v1/admin/auth/me", f.token, nil, 200)
	token2 := f.login(password)
	f.json("POST", "/api/v1/admin/auth/logout", token2, nil, 204)
	f.json("GET", "/api/v1/admin/auth/me", token2, nil, 401)
	f.json("PUT", "/api/v1/admin/auth/password", f.token, map[string]any{"current_password": "incorrect-password", "new_password": "new-password-only-123"}, 401)
	f.json("PUT", "/api/v1/admin/auth/password", f.token, map[string]any{"current_password": password, "new_password": "short"}, 400)
	other := f.login(password)
	f.json("PUT", "/api/v1/admin/auth/password", f.token, map[string]any{"current_password": password, "new_password": "new-password-only-123"}, 204)
	f.json("GET", "/api/v1/admin/auth/me", f.token, nil, 401)
	f.json("GET", "/api/v1/admin/auth/me", other, nil, 401)
	f.json("POST", "/api/v1/admin/auth/login", "", map[string]any{"username": "admin", "password": password}, 401)
	fresh := f.login("new-password-only-123")
	if _, err := f.pool.Exec(ctx, `UPDATE sessions SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	f.json("GET", "/api/v1/admin/auth/me", fresh, nil, 401)
	f.server.Close()
	f.app.Close()
	f.start()
	for i := 0; i < 10; i++ {
		f.json("POST", "/api/v1/admin/auth/login", "", map[string]any{"username": "nobody", "password": "incorrect-password"}, 401)
	}
	limited := f.json("POST", "/api/v1/admin/auth/login", "", map[string]any{"username": "admin", "password": "new-password-only-123"}, 429)
	if limited.header.Get("Retry-After") == "" {
		t.Fatal("retry header")
	}
}

func TestConcurrentMutationsAndSharedMedia(t *testing.T) {
	f := setup(t)
	p := f.create("CONCURRENT")
	path := "/api/v1/admin/products/" + p
	var wg sync.WaitGroup
	for _, body := range []map[string]any{{"name": "updated"}, {"stock": 42}} {
		wg.Add(1)
		go func(body map[string]any) { defer wg.Done(); f.json("PATCH", path, f.token, body, 200) }(body)
	}
	wg.Wait()
	got := f.json("GET", path, f.token, nil, 200)
	if got.object["name"] != "updated" || got.object["stock"] != float64(42) {
		t.Fatal("concurrent patch lost update")
	}
	mediaID := str(f.upload("images", "shared.png", f.token, imageData(t), 201), "id")
	other := f.create("OTHER")
	for _, id := range []string{p, other} {
		f.json("PUT", "/api/v1/admin/products/"+id+"/media", f.token, map[string]any{"media_ids": []string{mediaID}}, 200)
		f.json("PATCH", "/api/v1/admin/products/"+id, f.token, map[string]any{"status": "published"}, 200)
	}
	f.json("DELETE", path, f.token, nil, 204)
	f.request("GET", "/api/v1/media/"+mediaID+"/content", "", nil, nil, 200)
	f.json("DELETE", "/api/v1/admin/media/"+mediaID, f.token, nil, 409)
	f.json("DELETE", "/api/v1/admin/products/"+other, f.token, nil, 204)
	f.json("GET", "/api/v1/media/"+mediaID+"/content", "", nil, 404)
	f.json("GET", "/api/v1/admin/media/"+mediaID, f.token, nil, 200)
	f.json("DELETE", "/api/v1/admin/media/"+mediaID, f.token, nil, 204)
}

func TestHTTPBoundariesAndDocumentedAuth(t *testing.T) {
	f := setup(t)
	for path, item := range f.spec.Paths.Map() {
		for method, op := range item.Operations() {
			if op.Security != nil && len(*op.Security) > 0 {
				f.request(method, strings.ReplaceAll(path, "{id}", uuid.NewString()), "", nil, nil, 401)
			}
		}
	}
	f.json("GET", "/not-found", "", nil, 404)
	f.json("POST", "/api/v1/products", "", nil, 405)
	f.request("POST", "/api/v1/admin/products", f.token, strings.NewReader(`{}`), nil, 415)
	f.request("POST", "/api/v1/admin/products", f.token, strings.NewReader(strings.Repeat(" ", 1<<20)+"{}"), map[string]string{"Content-Type": "application/json"}, 413)
	f.request("POST", "/api/v1/admin/products", f.token, strings.NewReader(`{"sku":"x"} {}`), map[string]string{"Content-Type": "application/json"}, 400)
	f.json("GET", "/api/v1/products?q=%00", "", nil, 400)
	f.json("GET", "/api/v1/products?q=%FF", "", nil, 400)
	f.request("POST", "/api/v1/admin/media/images", f.token, strings.NewReader("--broken\r\nContent-Disposition: form-data; name=\"file\"; filename=\"photo.png\"\r\n\r\npartial"), map[string]string{"Content-Type": "multipart/form-data; boundary=broken"}, 400)
	r := f.request("OPTIONS", "/api/v1/admin/products", "", nil, map[string]string{"Origin": "https://shop.example", "Access-Control-Request-Method": "POST"}, 204)
	if r.header.Get("Access-Control-Allow-Origin") != "https://shop.example" {
		t.Fatal("CORS")
	}
	f.request("OPTIONS", "/api/v1/admin/products", "", nil, map[string]string{"Origin": "https://evil.example"}, 403)
	r = f.request("GET", "/api/v1/products", "", nil, map[string]string{"Origin": "https://evil.example"}, 200)
	if r.header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS wildcard")
	}
	f.app.Files.Close()
	f.json("GET", "/readyz", "", nil, 503)
}
