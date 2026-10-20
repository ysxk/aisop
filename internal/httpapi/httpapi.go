package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Error struct {
	Status  int
	Code    string
	Message string
	Fields  map[string]string
}

func (e *Error) Error() string { return e.Message }
func Invalid(message string) error {
	return &Error{Status: 400, Code: "invalid_request", Message: message}
}
func NotFound() error               { return &Error{Status: 404, Code: "not_found", Message: "Resource not found"} }
func Conflict(message string) error { return &Error{Status: 409, Code: "conflict", Message: message} }
func Unauthorized() error {
	return &Error{Status: 401, Code: "unauthorized", Message: "Invalid or expired credentials"}
}

type requestIDKey struct{}

func RequestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}
func WithRequestID(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestIDKey{}, uuid.NewString()))
}

func JSON(w http.ResponseWriter, status int, value any) {
	if status == http.StatusNoContent {
		w.WriteHeader(status)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Fail(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		var pgErr *pgconn.PgError
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			apiErr = &Error{Status: 404, Code: "not_found", Message: "Resource not found"}
		case errors.As(err, &pgErr) && pgErr.Code == "23505":
			apiErr = &Error{Status: 409, Code: "conflict", Message: "Resource already exists"}
		case errors.As(err, &pgErr) && pgErr.Code == "23503":
			apiErr = &Error{Status: 409, Code: "conflict", Message: "Resource is referenced or reference is invalid"}
		default:
			apiErr = &Error{Status: 500, Code: "internal_error", Message: "Internal server error"}
		}
	}
	if apiErr.Status == 401 {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	JSON(w, apiErr.Status, map[string]any{"error": struct {
		Code      string            `json:"code"`
		Message   string            `json:"message"`
		Fields    map[string]string `json:"fields,omitempty"`
		RequestID string            `json:"request_id"`
	}{apiErr.Code, apiErr.Message, apiErr.Fields, RequestID(r)}})
}

func Decode(w http.ResponseWriter, r *http.Request, dst any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &Error{Status: 415, Code: "unsupported_media_type", Message: "Content-Type must be application/json"}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return &Error{Status: 413, Code: "payload_too_large", Message: "JSON body exceeds 1 MiB"}
		}
		return Invalid("Cannot read JSON body")
	}
	if !utf8.Valid(data) {
		return Invalid("JSON must be valid UTF-8")
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return Invalid("Body must be a JSON object")
	}
	for name, value := range fields {
		if value == nil {
			return Invalid("Field '" + name + "' cannot be null; omit it to leave unchanged")
		}
		if containsNUL(value) {
			return Invalid("Text must not contain NUL characters")
		}
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return Invalid("Invalid JSON or unknown fields")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Invalid("Only one JSON object is allowed")
	}
	return nil
}

func containsNUL(value any) bool {
	switch v := value.(type) {
	case string:
		return strings.ContainsRune(v, 0)
	case []any:
		for _, item := range v {
			if containsNUL(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range v {
			if strings.ContainsRune(key, 0) || containsNUL(item) {
				return true
			}
		}
	}
	return false
}

func ID(r *http.Request) (string, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return "", Invalid("id must be a UUID")
	}
	return id.String(), nil
}

type Page struct {
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}

func Pagination(r *http.Request) (Page, error) {
	p := Page{Page: 1, Limit: 20}
	for _, item := range []struct {
		name string
		dest *int
		max  int
	}{{"page", &p.Page, 1000000}, {"limit", &p.Limit, 100}} {
		if raw, ok := r.URL.Query()[item.name]; ok {
			if len(raw) != 1 {
				return p, Invalid("Duplicate pagination parameter")
			}
			n, err := strconv.Atoi(raw[0])
			if err != nil || n < 1 || n > item.max {
				return p, Invalid("Invalid " + item.name)
			}
			*item.dest = n
		}
	}
	return p, nil
}
func (p Page) Offset() int { return (p.Page - 1) * p.Limit }
