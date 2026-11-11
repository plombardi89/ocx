// Package store keeps the product catalog in memory.
package store

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"example.com/shop/internal/money"
)

// Product is an item for sale.
type Product struct {
	ID    string      `json:"id"`
	Name  string      `json:"name"`
	Price money.Cents `json:"price_cents"`
	Stock int         `json:"stock"`
}

// ErrInvalidProduct is returned by Put for products that fail validation.
var ErrInvalidProduct = errors.New("invalid product")

// Store is an in-memory product catalog, safe for concurrent use.
type Store struct {
	mu       sync.RWMutex
	products map[string]Product
}

// New returns an empty store.
func New() *Store {
	return &Store{products: make(map[string]Product)}
}

// Put adds or replaces a product after validating it.
func (s *Store) Put(p Product) error {
	switch {
	case p.ID == "":
		return fmt.Errorf("%w: missing id", ErrInvalidProduct)
	case p.Name == "":
		return fmt.Errorf("%w: missing name", ErrInvalidProduct)
	case p.Price < 0:
		return fmt.Errorf("%w: negative price", ErrInvalidProduct)
	case p.Stock < 0:
		return fmt.Errorf("%w: negative stock", ErrInvalidProduct)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.products[p.ID] = p
	return nil
}

// Get returns the product with the given ID.
func (s *Store) Get(id string) (Product, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.products[id]
	return p, ok
}

// All returns every product sorted by ID.
func (s *Store) All() []Product {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Product, 0, len(s.products))
	for _, p := range s.products {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
