package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLoginValidation(t *testing.T) {
	for _, test := range []struct {
		name        string
		body        string
		contentType string
		status      int
	}{
		{"null object", `null`, "application/json", 400},
		{"array", `[]`, "application/json", 400},
		{"null username", `{"username":null,"password":"abcdefghijkl"}`, "application/json", 400},
		{"null password", `{"username":"admin","password":null}`, "application/json", 400},
		{"unknown field", `{"username":"admin","password":"abcdefghijkl","extra":1}`, "application/json", 400},
		{"missing password", `{"username":"admin"}`, "application/json", 400},
		{"missing username", `{"password":"abcdefghijkl"}`, "application/json", 400},
		{"blank username", `{"username":"  ","password":"abcdefghijkl"}`, "application/json", 400},
		{"short password", `{"username":"admin","password":"short"}`, "application/json", 400},
		{"wrong type", `{"username":1,"password":"abcdefghijkl"}`, "application/json", 400},
		{"trailing object", `{"username":"admin","password":"abcdefghijkl"} {}`, "application/json", 400},
		{"missing content type", `{}`, "", 415},
		{"wrong content type", `{}`, "text/plain", 415},
		{"oversized body", strings.Repeat(" ", 1<<20+1), "application/json", 413},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := New(nil, time.Hour)
			mux := http.NewServeMux()
			s.Register(mux)
			r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", strings.NewReader(test.body))
			r.Header.Set("Content-Type", test.contentType)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status %d, want %d: %s", w.Code, test.status, w.Body.String())
			}
			assertAPIError(t, w)
			if strings.Contains(w.Body.String(), "abcdefghijkl") {
				t.Fatal("error echoed the password")
			}
		})
	}
}

func TestPasswordValidation(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{}`, `{"current_password":null,"new_password":"abcdefghijkl"}`,
		`{"current_password":"abcdefghijkl","new_password":null}`,
		`{"current_password":"abcdefghijkl","new_password":"short"}`,
		`{"current_password":"short","new_password":"abcdefghijkl"}`,
		`{"current_password":"abcdefghijkl","new_password":"abcdefghijkl","extra":1}`,
		`{"current_password":"abcdefghijkl","new_password":5}`,
		`{"current_password":"abcdefghijkl","new_password":"abcdefghijkl"} {}`,
	} {
		t.Run(body, func(t *testing.T) {
			s := New(nil, time.Hour)
			r := httptest.NewRequest(http.MethodPut, "/api/v1/admin/auth/password", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.handlePassword(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", w.Code, w.Body.String())
			}
			assertAPIError(t, w)
		})
	}
}

func TestProtectedRoutes(t *testing.T) {
	s := New(nil, time.Hour)
	mux := http.NewServeMux()
	s.Register(mux)
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/admin/auth/logout"},
		{http.MethodGet, "/api/v1/admin/auth/me"},
		{http.MethodPut, "/api/v1/admin/auth/password"},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(route.method, route.path, nil))
		if w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("unprotected route %s %s: %d", route.method, route.path, w.Code)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("authentication response can be cached")
		}
		assertAPIError(t, w)
	}
}

func TestRequireMalformedAuthorization(t *testing.T) {
	s := New(nil, time.Hour)
	called := false
	protected := s.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	for _, header := range [][]string{
		nil, {""}, {"Basic abc"}, {"Bearer"}, {"Bearer short"},
		{"Bearer " + strings.Repeat("a", 43) + " extra"},
		{"Bearer " + strings.Repeat("a", 43), "Bearer " + strings.Repeat("a", 43)},
		{"Bearer " + strings.Repeat("a", 42) + "B"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		for _, value := range header {
			r.Header.Add("Authorization", value)
		}
		w := httptest.NewRecorder()
		protected.ServeHTTP(w, r)
		if called || w.Code != http.StatusUnauthorized {
			t.Fatalf("malformed authorization accepted: status %d", w.Code)
		}
		assertAPIError(t, w)
	}
}

func TestLoginRateLimitIgnoresForwardingHeaders(t *testing.T) {
	s := New(nil, time.Hour)
	mux := http.NewServeMux()
	s.Register(mux)
	for i := range loginIPLimit + 1 {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", strings.NewReader(`{}`))
		r.RemoteAddr = fmt.Sprintf("192.0.2.1:%d", 1000+i)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		r.Header.Set("X-Real-IP", fmt.Sprintf("203.0.113.%d", i))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if i < loginIPLimit {
			if w.Code != http.StatusBadRequest {
				t.Fatalf("attempt %d: unexpected status %d", i, w.Code)
			}
		} else {
			assertRateLimited(t, w)
		}
	}
}

func TestLoginGlobalRateLimitHTTP(t *testing.T) {
	s := New(nil, time.Hour)
	for i := range loginGlobalLimit + 1 {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/auth/login", strings.NewReader(`{}`))
		r.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleLogin(w, r)
		if i < loginGlobalLimit {
			if w.Code != http.StatusBadRequest {
				t.Fatalf("attempt %d: unexpected status %d", i, w.Code)
			}
		} else {
			assertRateLimited(t, w)
		}
	}
}

func TestMePlainObject(t *testing.T) {
	s := New(nil, time.Hour)
	admin := Admin{ID: uuid.New(), Username: "admin", CreatedAt: time.Now().UTC()}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/auth/me", nil)
	r = r.WithContext(context.WithValue(r.Context(), identityKey{}, identity{admin: admin, tokenHash: "secret digest"}))
	w := httptest.NewRecorder()
	s.handleMe(w, r)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &object); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(object) != 3 || object["id"] == nil || object["username"] == nil || object["created_at"] == nil {
		t.Fatalf("incorrect me response: %d %s", w.Code, w.Body.String())
	}
}

func assertAPIError(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code == "" || response.Error.Message == "" {
		t.Fatalf("not an httpapi error response: %s", w.Body.String())
	}
}

func assertRateLimited(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", w.Code, w.Body.String())
	}
	retry, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || retry < 1 || retry > 60 {
		t.Fatalf("invalid Retry-After: %q", w.Header().Get("Retry-After"))
	}
	assertAPIError(t, w)
}
