package category

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysxk/aisop/internal/httpapi"
)

type handler struct{ service *service }

func Register(mux *http.ServeMux, pool *pgxpool.Pool, require func(http.Handler) http.Handler) {
	h := &handler{service: &service{repo: &repository{pool: pool}}}
	mux.HandleFunc("GET /api/v1/categories", h.list)
	mux.HandleFunc("GET /api/v1/categories/{id}", h.get)
	for _, route := range []struct {
		pattern string
		handle  http.HandlerFunc
	}{
		{"GET /api/v1/admin/categories", h.list},
		{"POST /api/v1/admin/categories", h.create},
		{"GET /api/v1/admin/categories/{id}", h.get},
		{"PATCH /api/v1/admin/categories/{id}", h.patch},
		{"DELETE /api/v1/admin/categories/{id}", h.delete},
	} {
		mux.Handle(route.pattern, require(route.handle))
	}
}
func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	page, err := httpapi.Pagination(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	result, err := h.service.repo.list(r.Context(), page)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, result)
}
func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpapi.ID(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	c, err := h.service.repo.get(r.Context(), id)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, c)
}
func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	var in input
	if err := httpapi.Decode(w, r, &in); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	c, err := h.service.create(r.Context(), in)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/admin/categories/"+c.ID)
	httpapi.JSON(w, http.StatusCreated, c)
}
func (h *handler) patch(w http.ResponseWriter, r *http.Request) {
	id, err := httpapi.ID(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	var in input
	if err := httpapi.Decode(w, r, &in); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	c, err := h.service.patch(r.Context(), id, in)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, c)
}
func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	id, err := httpapi.ID(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	if err := h.service.repo.delete(r.Context(), id); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
