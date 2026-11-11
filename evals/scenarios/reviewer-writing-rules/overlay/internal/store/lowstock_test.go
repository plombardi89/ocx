package store

import (
	"strings"
	"testing"
)

func newLowStockStore(t *testing.T) *Store {
	t.Helper()
	s := New()
	for _, p := range []Product{
		{ID: "d", Name: "Dates", Price: 1, Stock: 10},
		{ID: "a", Name: "Apples", Price: 1, Stock: 5},
		{ID: "c", Name: "Cherries", Price: 1, Stock: 0},
		{ID: "b", Name: "Bananas", Price: 1, Stock: 2},
	} {
		if err := s.Put(p); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func ids(ps []Product) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return strings.Join(out, ",")
}

func TestPlan0005Task2(t *testing.T) {
	s := newLowStockStore(t)
	cases := []struct {
		threshold int
		want      string
	}{
		{1, "c"},
		{3, "b,c"},
		{5, "b,c"},
		{6, "a,b,c"},
		{11, "a,b,c,d"},
		{1000, "a,b,c,d"},
	}
	for _, c := range cases {
		if got := ids(s.LowStock(c.threshold)); got != c.want {
			t.Errorf("LowStock(%d) = %q, want %q", c.threshold, got, c.want)
		}
	}
}

func TestLowStockNonPositiveThresholdIsEmpty(t *testing.T) {
	s := newLowStockStore(t)
	for _, threshold := range []int{0, -1, -1000} {
		got := s.LowStock(threshold)
		if got == nil || len(got) != 0 {
			t.Errorf("LowStock(%d) = %#v, want an empty non-nil slice", threshold, got)
		}
	}
	if got := New().LowStock(10); got == nil || len(got) != 0 {
		t.Errorf("empty store: LowStock(10) = %#v, want an empty non-nil slice", got)
	}
}
