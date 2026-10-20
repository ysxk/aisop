package media

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/ysxk/aisop/internal/httpapi"
)

func (s *Service) Register(mux *http.ServeMux, require func(http.Handler) http.Handler, imageMax, videoMax int64) {
	mux.Handle("POST /api/v1/admin/media/images", require(s.upload("image", imageMax)))
	mux.Handle("POST /api/v1/admin/media/videos", require(s.upload("video", videoMax)))
	mux.Handle("GET /api/v1/admin/media", require(http.HandlerFunc(s.list)))
	mux.Handle("GET /api/v1/admin/media/{id}", require(http.HandlerFunc(s.get)))
	mux.Handle("PATCH /api/v1/admin/media/{id}", require(http.HandlerFunc(s.patch)))
	mux.Handle("DELETE /api/v1/admin/media/{id}", require(http.HandlerFunc(s.delete)))
	mux.Handle("GET /api/v1/admin/media/{id}/content", require(s.content(false)))
	mux.Handle("GET /api/v1/media/{id}/content", s.content(true))
}
func (s *Service) upload(kind string, limit int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "multipart/form-data" {
			httpapi.Fail(w, r, &httpapi.Error{Status: 415, Code: "unsupported_media_type", Message: "Content-Type must be multipart/form-data"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limit+(64<<10))
		mr, err := r.MultipartReader()
		if err != nil {
			httpapi.Fail(w, r, httpapi.Invalid("Invalid multipart boundary"))
			return
		}
		m, err := s.Upload(r.Context(), kind, mr, limit)
		if err != nil {
			httpapi.Fail(w, r, uploadError(err))
			return
		}
		w.Header().Set("Location", "/api/v1/admin/media/"+m.ID)
		httpapi.JSON(w, http.StatusCreated, m)
	})
}
func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	p, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "image" && kind != "video" {
		httpapi.Fail(w, r, httpapi.Invalid("kind must be image or video"))
		return
	}
	items, p, err := s.repo.list(r.Context(), p, kind)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, 200, struct {
		Items []Media `json:"items"`
		httpapi.Page
	}{items, p})
}
func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpapi.ID(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	m, err := s.repo.get(r.Context(), id, false)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, 200, m)
}
func (s *Service) patch(w http.ResponseWriter, r *http.Request) {
	id, err := httpapi.ID(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	var p Patch
	if err := httpapi.Decode(w, r, &p); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	m, err := s.Patch(r.Context(), id, p)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, 200, m)
}
func (s *Service) delete(w http.ResponseWriter, r *http.Request) {
	id, err := httpapi.ID(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	if err := s.repo.delete(r.Context(), id); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Cleanup(ctx)
	w.WriteHeader(http.StatusNoContent)
}
func (s *Service) content(public bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := httpapi.ID(r)
		if err != nil {
			httpapi.Fail(w, r, err)
			return
		}
		m, err := s.repo.get(r.Context(), id, public)
		if err != nil {
			httpapi.Fail(w, r, err)
			return
		}
		f, err := s.files.Open(m.StorageKey)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				err = httpapi.NotFound()
			}
			httpapi.Fail(w, r, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", m.MIMEType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": m.OriginalName}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-cache, max-age=0, must-revalidate")
		w.Header().Set("ETag", `"`+m.ID+`"`)
		cw := &contentWriter{ResponseWriter: w}
		http.ServeContent(cw, r, m.OriginalName, m.CreatedAt, f)
		if cw.rejected != 0 {
			w.Header().Del("Content-Length")
			w.Header().Del("Content-Disposition")
			w.Header().Del("ETag")
			w.Header().Set("Cache-Control", "no-store")
			code := "content_request_failed"
			if cw.rejected == http.StatusRequestedRangeNotSatisfiable {
				code = "invalid_range"
			}
			httpapi.Fail(w, r, &httpapi.Error{Status: cw.rejected, Code: code, Message: http.StatusText(cw.rejected)})
		}
	})
}

type contentWriter struct {
	http.ResponseWriter
	rejected int
}

func (w *contentWriter) WriteHeader(status int) {
	if status >= 400 {
		w.rejected = status
		return
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *contentWriter) Write(data []byte) (int, error) {
	if w.rejected != 0 {
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}
