package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ysxk/aisop/internal/httpapi"
	"golang.org/x/crypto/bcrypt"
)

func TestValidateCredentials(t *testing.T) {
	for _, test := range []struct {
		name     string
		username string
		password string
		want     string
		valid    bool
	}{
		{"trimmed", " \tadmin\n", "abcdefghijkl", "admin", true},
		{"unicode username", strings.Repeat("用", 100), "abcdefghijkl", strings.Repeat("用", 100), true},
		{"long username", strings.Repeat("用", 101), "abcdefghijkl", "", false},
		{"empty username", "", "abcdefghijkl", "", false},
		{"blank username", " \n\t", "abcdefghijkl", "", false},
		{"invalid utf8", string([]byte{0xff}), "abcdefghijkl", "", false},
		{"short password", "admin", strings.Repeat("p", 11), "", false},
		{"maximum password", "admin", strings.Repeat("p", 72), "admin", true},
		{"long password", "admin", strings.Repeat("p", 73), "", false},
		{"password bytes minimum", "admin", strings.Repeat("密", 4), "admin", true},
		{"password bytes maximum", "admin", strings.Repeat("密", 24), "admin", true},
		{"password too many bytes", "admin", strings.Repeat("密", 25), "", false},
		{"password not trimmed", "admin", "            ", "admin", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateCredentials(test.username, test.password)
			if test.valid {
				if err != nil || got != test.want {
					t.Fatalf("got username %q, error %v", got, err)
				}
				return
			}
			var apiErr *httpapi.Error
			if !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
				t.Fatalf("expected validation error, got %v", err)
			}
		})
	}
}

func TestBootstrapValidatesBeforeDatabase(t *testing.T) {
	s := New(nil, time.Hour)
	if err := s.Bootstrap(context.Background(), " ", "abcdefghijkl"); err == nil {
		t.Fatal("accepted empty username")
	}
	if err := s.Bootstrap(context.Background(), "admin", "short"); err == nil {
		t.Fatal("accepted short password")
	}
}

func TestTokens(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		token, hash, err := generateToken()
		if err != nil {
			t.Fatal(err)
		}
		if !validToken(token) || len(token) != 43 || strings.ContainsAny(token, "+/=") {
			t.Fatal("token is not canonical raw URL base64")
		}
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(decoded) != 32 {
			t.Fatal("token does not contain 32 random bytes")
		}
		expected := sha256.Sum256([]byte(token))
		if hash != hex.EncodeToString(expected[:]) || len(hash) != 64 {
			t.Fatal("stored token digest is not SHA256 hex")
		}
		if seen[token] {
			t.Fatal("duplicate token")
		}
		seen[token] = true
	}
}

func TestInvalidTokens(t *testing.T) {
	for _, token := range []string{
		"", "short", strings.Repeat("a", 42), strings.Repeat("a", 44),
		strings.Repeat("a", 42) + "=", strings.Repeat("a", 42) + "+",
		strings.Repeat("a", 42) + "/", strings.Repeat("a", 42) + "B",
		strings.Repeat("a", 42) + "\n", strings.Repeat("a", 10000),
	} {
		if validToken(token) {
			t.Fatal("accepted malformed token")
		}
	}
}

func TestDummyHashUsesDefaultCost(t *testing.T) {
	cost, err := bcrypt.Cost(dummyPasswordHash)
	if err != nil || cost != bcrypt.DefaultCost {
		t.Fatalf("invalid dummy bcrypt cost: %d, %v", cost, err)
	}
	if err := bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte("wrong password")); !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		t.Fatalf("dummy hash did not perform a password comparison: %v", err)
	}
}

func TestLoginResponseShape(t *testing.T) {
	response := loginResponse{
		Token: "token", TokenType: "Bearer", ExpiresAt: time.Now().UTC(),
		Admin: Admin{ID: uuid.New(), Username: "admin", CreatedAt: time.Now().UTC()},
	}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if len(object) != 4 || object["token"] == nil || object["token_type"] == nil || object["expires_at"] == nil || object["admin"] == nil {
		t.Fatalf("incorrect login response keys: %s", data)
	}
	var admin map[string]json.RawMessage
	if err := json.Unmarshal(object["admin"], &admin); err != nil {
		t.Fatal(err)
	}
	if len(admin) != 3 || admin["id"] == nil || admin["username"] == nil || admin["created_at"] == nil {
		t.Fatalf("incorrect admin response keys: %s", object["admin"])
	}
}
