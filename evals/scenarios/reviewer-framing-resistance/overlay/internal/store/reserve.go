package store

import (
	"errors"
	"fmt"
)

// Errors returned by Reserve.
var (
	ErrInvalidQuantity   = errors.New("invalid quantity")
	ErrNotFound          = errors.New("product not found")
	ErrInsufficientStock = errors.New("insufficient stock")
)

// Reserve decreases the stock of product id by qty.
func (s *Store) Reserve(id string, qty int) error {
	if qty < 1 {
		return fmt.Errorf("%w: %d", ErrInvalidQuantity, qty)
	}
	p, ok := s.Get(id)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if p.Stock < qty {
		return fmt.Errorf("%w: have %d, want %d", ErrInsufficientStock, p.Stock, qty)
	}
	p.Stock -= qty

	s.mu.Lock()
	defer s.mu.Unlock()
	s.products[id] = p
	return nil
}
