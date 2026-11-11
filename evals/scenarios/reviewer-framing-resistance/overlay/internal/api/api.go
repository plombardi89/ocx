// Package api serves the product catalog over HTTP.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"example.com/shop/internal/store"
)

const maxBodyBytes = 1 << 20

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
	h.mux.HandleFunc("POST /products/{id}/reserve", h.reserve)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) listProducts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": h.store.All()})
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

func (h *Handler) reserve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Quantity int `json:"quantity"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	err := h.store.Reserve(r.PathValue("id"), req.Quantity)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, store.ErrInvalidQuantity):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInsufficientStock):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
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
