// Package api serves the product catalog over HTTP.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"example.com/shop/internal/store"
)

const (
	maxBodyBytes = 1 << 20
	defaultLimit = 20
	maxLimit     = 100
)

// Handler serves the catalog API.
type Handler struct {
	store *store.Store
	mux   *http.ServeMux
}

// New returns the API handler backed by s.
func New(s *store.Store) *Handler {
	h := &Handler{store: s, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /products", h.listProducts)
	h.mux.HandleFunc("GET /products/{id}", h.getProduct)
	h.mux.HandleFunc("POST /products", h.putProduct)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) listProducts(w http.ResponseWriter, r *http.Request) {
	// URL.Query drops pairs it cannot parse, which would turn a malformed
	// limit into the default instead of a 400.
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid query string")
		return
	}
	offset, err := intParam(q, "offset", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limit, err := intParam(q, "limit", defaultLimit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, total := h.store.List(offset, min(limit, maxLimit))
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// intParam reads a non-negative integer parameter from q, returning def when
// the parameter is absent. A parameter that is present but empty is invalid.
func intParam(q url.Values, name string, def int) (int, error) {
	if !q.Has(name) {
		return def, nil
	}
	n, err := strconv.Atoi(q.Get(name))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", name)
	}
	return n, nil
}

func (h *Handler) getProduct(w http.ResponseWriter, r *http.Request) {
	p, ok := h.store.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "product not found")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h *Handler) putProduct(w http.ResponseWriter, r *http.Request) {
	var p store.Product
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := h.store.Put(p); err != nil {
		if errors.Is(err, store.ErrInvalidProduct) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status line is already sent; an encode error means the client
	// went away, and there is nothing useful left to tell it.
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
