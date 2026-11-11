// Package money handles currency amounts as integer cents.
package money

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Cents is an amount of money in US cents.
type Cents int64

// ErrInvalidAmount is returned when an amount cannot be parsed.
var ErrInvalidAmount = errors.New("invalid amount")

// maxWhole is the largest whole-dollar value that still fits in Cents with
// any two-digit fraction.
const maxWhole = (math.MaxInt64 - 99) / 100

// ParseDollars parses a non-negative dollar amount such as "12", "12.5", or
// "12.34" into cents. Signs, spaces, and more than two decimal places are
// rejected.
func ParseDollars(s string) (Cents, error) {
	whole, frac, hasFrac := strings.Cut(s, ".")
	if !isDigits(whole) || (hasFrac && (!isDigits(frac) || len(frac) > 2)) {
		return 0, fmt.Errorf("%w: %q", ErrInvalidAmount, s)
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > maxWhole {
		return 0, fmt.Errorf("%w: %q is out of range", ErrInvalidAmount, s)
	}
	var f int64
	if hasFrac {
		// Parse the fraction as a decimal so "5" and "50" both mean fifty
		// cents, without padding by hand.
		x, err := strconv.ParseFloat("0."+frac, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: %q", ErrInvalidAmount, s)
		}
		f = int64(x * 100)
	}
	return Cents(w*100 + f), nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// String formats c as dollars, for example "$12.34" or "-$0.05".
func (c Cents) String() string {
	sign := ""
	u := uint64(c)
	if c < 0 {
		sign = "-"
		u = uint64(-(c + 1)) + 1 // avoids overflow for math.MinInt64
	}
	return fmt.Sprintf("%s$%d.%02d", sign, u/100, u%100)
}
