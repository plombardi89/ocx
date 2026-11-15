package money

import (
	"errors"
	"fmt"
)

// MaxParts is the largest number of parts Split accepts.
const MaxParts = 1000

// ErrInvalidSplit is returned by Split for a negative total or an
// out-of-range number of parts.
var ErrInvalidSplit = errors.New("invalid split")

// Split divides total into n parts that sum exactly to total and differ by
// at most one cent, with the larger parts first. For example, Split(100, 3)
// returns [34 33 33]. It returns an error wrapping ErrInvalidSplit if total
// is negative or n is outside [1, MaxParts].
func Split(total Cents, n int) ([]Cents, error) {
	if n < 1 || n > MaxParts {
		return nil, fmt.Errorf("%w: n must be between 1 and %d, got %d", ErrInvalidSplit, MaxParts, n)
	}
	if total < 0 {
		return nil, fmt.Errorf("%w: total must not be negative, got %s", ErrInvalidSplit, total)
	}
	base := total / Cents(n)
	remainder := int(total % Cents(n))
	parts := make([]Cents, n)
	for i := range parts {
		parts[i] = base
		if i < remainder {
			parts[i]++
		}
	}
	return parts, nil
}
