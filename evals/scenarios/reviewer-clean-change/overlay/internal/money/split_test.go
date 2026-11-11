package money

import (
	"errors"
	"math"
	"slices"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct {
		total Cents
		n     int
		want  []Cents
	}{
		{100, 1, []Cents{100}},
		{100, 4, []Cents{25, 25, 25, 25}},
		{100, 3, []Cents{34, 33, 33}},
		{101, 3, []Cents{34, 34, 33}},
		{2, 5, []Cents{1, 1, 0, 0, 0}},
		{0, 3, []Cents{0, 0, 0}},
		{math.MaxInt64, 2, []Cents{4611686018427387904, 4611686018427387903}},
	}
	for _, c := range cases {
		got, err := Split(c.total, c.n)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("Split(%d, %d) = %v, %v; want %v", c.total, c.n, got, err, c.want)
		}
	}
}

func TestSplitProperties(t *testing.T) {
	for total := Cents(0); total <= 250; total++ {
		for n := 1; n <= 12; n++ {
			parts, err := Split(total, n)
			if err != nil {
				t.Fatalf("Split(%d, %d): %v", total, n, err)
			}
			if len(parts) != n {
				t.Fatalf("Split(%d, %d) returned %d parts", total, n, len(parts))
			}
			var sum Cents
			for i, p := range parts {
				sum += p
				if i > 0 && p > parts[i-1] {
					t.Fatalf("Split(%d, %d) = %v: larger part after a smaller one", total, n, parts)
				}
			}
			if sum != total {
				t.Fatalf("Split(%d, %d) = %v sums to %d", total, n, parts, sum)
			}
			if parts[0]-parts[n-1] > 1 {
				t.Fatalf("Split(%d, %d) = %v: parts differ by more than one cent", total, n, parts)
			}
		}
	}
}

func TestSplitMaxParts(t *testing.T) {
	parts, err := Split(MaxParts+1, MaxParts)
	if err != nil || len(parts) != MaxParts || parts[0] != 2 || parts[1] != 1 {
		t.Fatalf("Split(%d, MaxParts) = %d parts starting %v, %v", MaxParts+1, len(parts), parts[:2], err)
	}
}

func TestSplitRejects(t *testing.T) {
	cases := []struct {
		total Cents
		n     int
	}{
		{100, 0},
		{100, -1},
		{100, MaxParts + 1},
		{-1, 2},
		{math.MinInt64, 1},
	}
	for _, c := range cases {
		parts, err := Split(c.total, c.n)
		if !errors.Is(err, ErrInvalidSplit) || parts != nil {
			t.Errorf("Split(%d, %d) = %v, %v; want nil, ErrInvalidSplit", c.total, c.n, parts, err)
		}
	}
}
