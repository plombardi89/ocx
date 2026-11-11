package store

import (
	"errors"
	"testing"
)

func newReserveStore(t *testing.T) *Store {
	t.Helper()
	s := New()
	if err := s.Put(Product{ID: "a", Name: "Apple", Price: 50, Stock: 5}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestReserve(t *testing.T) {
	s := newReserveStore(t)
	if err := s.Reserve("a", 2); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Get("a"); p.Stock != 3 {
		t.Fatalf("stock = %d, want 3", p.Stock)
	}
	if err := s.Reserve("a", 3); err != nil {
		t.Fatal(err)
	}
	if p, _ := s.Get("a"); p.Stock != 0 {
		t.Fatalf("stock = %d, want 0", p.Stock)
	}
}

func TestReserveErrors(t *testing.T) {
	s := newReserveStore(t)
	cases := []struct {
		id   string
		qty  int
		want error
	}{
		{"a", 0, ErrInvalidQuantity},
		{"a", -1, ErrInvalidQuantity},
		{"zzz", 1, ErrNotFound},
		{"a", 6, ErrInsufficientStock},
	}
	for _, c := range cases {
		if err := s.Reserve(c.id, c.qty); !errors.Is(err, c.want) {
			t.Errorf("Reserve(%q, %d) = %v, want %v", c.id, c.qty, err, c.want)
		}
	}
	if p, _ := s.Get("a"); p.Stock != 5 {
		t.Fatalf("failed reservations changed stock to %d", p.Stock)
	}
}
