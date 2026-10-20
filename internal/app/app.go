package app

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysxk/aisop/api"
	"github.com/ysxk/aisop/internal/auth"
	"github.com/ysxk/aisop/internal/config"
	"github.com/ysxk/aisop/internal/httpapi"
	"github.com/ysxk/aisop/internal/media"
	"github.com/ysxk/aisop/internal/product"
	"github.com/ysxk/aisop/internal/storage"
)

type App struct {
	Handler http.Handler
	Auth    *auth.Service
	Media   *media.Service
	Files   *storage.Local
}

func New(c config.Config, pool *pgxpool.Pool, logger *slog.Logger) (*App, error) {
	files, err := storage.New(c.MediaDir)
	if err != nil {
		return nil, err
	}
	a := &App{Auth: auth.New(pool, c.SessionTTL), Media: media.New(pool, files), Files: files}
	mux := http.NewServeMux()
	a.Auth.Register(mux)
	product.Register(mux, pool, a.Auth.Require)
	a.Media.Register(mux, a.Auth.Require, c.ImageMaxBytes, c.VideoMaxBytes)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { httpapi.JSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			httpapi.Fail(w, r, &httpapi.Error{Status: 503, Code: "not_ready", Message: "Service not ready"})
			return
		}
		if err := files.Ready(); err != nil {
			httpapi.Fail(w, r, &httpapi.Error{Status: 503, Code: "not_ready", Message: "Service not ready"})
			return
		}
		httpapi.JSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(api.OpenAPI)
	})
	a.Handler = middleware(mux, c.AllowedOrigins, logger)
	return a, nil
}
func (a *App) Close() error { return a.Files.Close() }
func (a *App) RunCleanup(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := a.Media.Cleanup(cleanupCtx)
		cancel()
		if err != nil && ctx.Err() == nil {
			logger.Warn("media cleanup deferred")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *responseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func middleware(mux *http.ServeMux, origins []string, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = httpapi.WithRequestID(r)
		rw := &responseWriter{ResponseWriter: w}
		w = rw
		w.Header().Set("X-Request-ID", httpapi.RequestID(r))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		start := time.Now()
		defer func() {
			if recover() != nil {
				if rw.status == 0 {
					httpapi.Fail(w, r, &httpapi.Error{Status: 500, Code: "internal_error", Message: "Internal server error"})
				}
				logger.Error("request panic", "request_id", httpapi.RequestID(r))
			}
			logger.Info("request", "request_id", httpapi.RequestID(r), "method", r.Method, "route", r.Pattern, "status", rw.status, "duration_ms", time.Since(start).Milliseconds())
		}()
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			allowed := false
			for _, candidate := range origins {
				if candidate == origin {
					allowed = true
					break
				}
			}
			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Content-Range, Accept-Ranges, ETag, Location")
				if r.Method == http.MethodOptions {
					w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range, If-None-Match, If-Range")
					w.Header().Set("Access-Control-Max-Age", "600")
					w.WriteHeader(204)
					return
				}
			} else if r.Method == http.MethodOptions {
				httpapi.Fail(w, r, &httpapi.Error{Status: 403, Code: "origin_not_allowed", Message: "Origin is not allowed"})
				return
			}
		}
		_, pattern := mux.Handler(r)
		if pattern == "" {
			var methods []string
			for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"} {
				probe := r.Clone(r.Context())
				probe.Method = method
				if _, match := mux.Handler(probe); match != "" {
					methods = append(methods, method)
				}
			}
			if len(methods) > 0 {
				w.Header().Set("Allow", strings.Join(methods, ", "))
				httpapi.Fail(w, r, &httpapi.Error{Status: 405, Code: "method_not_allowed", Message: "Method not allowed"})
			} else {
				httpapi.Fail(w, r, httpapi.NotFound())
			}
			return
		}
		mux.ServeHTTP(w, r)
	})
}
