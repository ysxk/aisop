package product

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ysxk/aisop/internal/httpapi"
)

type handler struct{ service *service }

func Register(mux *http.ServeMux, pool *pgxpool.Pool, require func(http.Handler) http.Handler) {
	h := &handler{service: &service{repo: &repository{pool: pool}}}
	mux.HandleFunc("GET /api/v1/products", h.list(false))
	mux.HandleFunc("GET /api/v1/products/{id}", h.get(false))
	for _, route := range []struct {
		pattern string
		handle  http.HandlerFunc
	}{
		{"GET /api/v1/admin/products", h.list(true)},
		{"POST /api/v1/admin/products", h.create},
		{"GET /api/v1/admin/products/{id}", h.get(true)},
		{"PATCH /api/v1/admin/products/{id}", h.patch},
		{"DELETE /api/v1/admin/products/{id}", h.delete},
		{"PUT /api/v1/admin/products/{id}/media", h.associate},
	} {
		mux.Handle(route.pattern, require(route.handle))
	}
}

func (h *handler) list(admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filter, err := parseFilter(r, admin)
		if err != nil {
			httpapi.Fail(w, r, err)
			return
		}
		result, err := h.service.repo.list(r.Context(), filter, admin)
		if err != nil {
			httpapi.Fail(w, r, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, result)
	}
}

func (h *handler) get(admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := httpapi.ID(r)
		if err != nil {
			httpapi.Fail(w, r, err)
			return
		}
		p, err := h.service.repo.get(r.Context(), id, admin)
		if err != nil {
			httpapi.Fail(w, r, err)
			return
		}
		httpapi.JSON(w, http.StatusOK, p)
	}
}

func (h *handler) create(w http.ResponseWriter, r *http.Request) {
	var in productInput
	if err := httpapi.Decode(w, r, &in); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	p, err := h.service.create(r.Context(), in)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, p)
}

func (h *handler) patch(w http.ResponseWriter, r *http.Request) {
	id, err := httpapi.ID(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	var in productInput
	if err := httpapi.Decode(w, r, &in); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	p, err := h.service.patch(r.Context(), id, in)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, p)
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

func (h *handler) associate(w http.ResponseWriter, r *http.Request) {
	id, err := httpapi.ID(r)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	var in associationInput
	if err := httpapi.Decode(w, r, &in); err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	p, err := h.service.associate(r.Context(), id, in)
	if err != nil {
		httpapi.Fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, p)
}
