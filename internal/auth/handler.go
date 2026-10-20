package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ysxk/aisop/internal/httpapi"
)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/admin/auth/login", s.handleLogin)
	mux.Handle("POST /api/v1/admin/auth/logout", s.Require(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /api/v1/admin/auth/me", s.Require(http.HandlerFunc(s.handleMe)))
	mux.Handle("PUT /api/v1/admin/auth/password", s.Require(http.HandlerFunc(s.handlePassword)))
}

func (s *Service) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			httpapi.Fail(w, r, httpapi.Unauthorized())
			return
		}
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !validToken(parts[1]) {
			httpapi.Fail(w, r, httpapi.Unauthorized())
			return
		}
		hash := tokenDigest(parts[1])
		admin, err := s.repo.session(r.Context(), hash)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				httpapi.Fail(w, r, httpapi.Unauthorized())
			} else {
				httpapi.Fail(w, r, errInternal)
			}
			return
		}
		ctx := context.WithValue(r.Context(), identityKey{}, identity{admin: admin, tokenHash: hash})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if allowed, retry := s.limiter.allow(remoteIP(r.RemoteAddr), time.Now()); !allowed {
		seconds := int64((retry + time.Second - 1) / time.Second)
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		httpapi.Fail(w, r, &httpapi.Error{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "Too many login attempts"})
		return
	}
	var request credentials
	if err := httpapi.Decode(w, r, &request); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	username, err := validateCredentials(request.Username, request.Password)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	response, err := s.login(r.Context(), username, request.Password)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, response)
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	who := r.Context().Value(identityKey{}).(identity)
	if err := s.repo.logout(r.Context(), who.tokenHash); err != nil {
		httpapi.Fail(w, r, errInternal)
		return
	}
	httpapi.JSON(w, http.StatusNoContent, nil)
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	who := r.Context().Value(identityKey{}).(identity)
	httpapi.JSON(w, http.StatusOK, who.admin)
}

func (s *Service) handlePassword(w http.ResponseWriter, r *http.Request) {
	var request passwordRequest
	if err := httpapi.Decode(w, r, &request); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	if err := validatePassword(request.CurrentPassword); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	if err := validatePassword(request.NewPassword); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	who := r.Context().Value(identityKey{}).(identity)
	if err := s.changePassword(r.Context(), who, request.CurrentPassword, request.NewPassword); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusNoContent, nil)
}
