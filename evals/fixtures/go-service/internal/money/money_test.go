package money

import (
	"errors"
	"math"
	"testing"
)

func TestParseDollars(t *testing.T) {
	cases := []struct {
		in   string
		want Cents
	}{
		{"0", 0},
		{"12", 1200},
		{"12.5", 1250},
		{"12.34", 1234},
		{"0.07", 7},
		{"92233720368547757.99", 9223372036854775799},
	}
	for _, c := range cases {
		got, err := ParseDollars(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseDollars(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}

func TestParseDollarsRejects(t *testing.T) {
	for _, in := range []string{"", ".", "1.", ".5", "-1", "+1", "1.234", "1,00", " 1", "1.-5", "abc", "92233720368547758.00"} {
		if got, err := ParseDollars(in); !errors.Is(err, ErrInvalidAmount) {
			t.Errorf("ParseDollars(%q) = %d, %v; want ErrInvalidAmount", in, got, err)
		}
	}
}

func TestString(t *testing.T) {
	cases := map[Cents]string{
		0:             "$0.00",
		5:             "$0.05",
		1234:          "$12.34",
		-5:            "-$0.05",
		-1234:         "-$12.34",
		math.MinInt64: "-$92233720368547758.08",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("Cents(%d).String() = %q, want %q", int64(in), got, want)
		}
	}
}
