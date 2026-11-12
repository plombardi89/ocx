# Tests: docs/plans/0002-split-amounts.md

## Criteria

1. PASSED: `internal/money/split_parts_test.go::TestSplitReturnsTheRequestedNumberOfParts`. Mutation: internal/money/split.go: made the result one part shorter; this test failed.
2. PASSED: `internal/money/split_parts_test.go::TestSplitPartsSumToTheTotal`. Mutation: internal/money/split.go: dropped the remainder; this test failed.
3. PASSED: `internal/money/split_parts_test.go::TestSplitPutsTheLargerPartsFirst`. Mutation: internal/money/split.go: gave the remainder to the last parts; this test failed.
4. PASSED: `internal/money/split_parts_test.go::TestSplitIntoMorePartsThanCents`. Mutation: internal/money/split.go: rejected n greater than total; this test failed.
5. PASSED: `internal/money/split_parts_test.go::TestSplitRejectsInvalidArguments`. Mutation: internal/money/split.go: removed the MaxParts check; this test failed.
6. PASSED: `internal/money/split_parts_test.go::TestSplitTheLargestTotal`. Mutation: internal/money/split.go: computed the base part in float64; this test failed.

## Failures

None.

## Plan gaps

None.

## Test files

### `internal/money/split_parts_test.go`

```go
package money

import (
	"errors"
	"math"
	"slices"
	"testing"
)

func TestSplitReturnsTheRequestedNumberOfParts(t *testing.T) {
	for _, n := range []int{1, 2, 7, MaxParts} {
		parts, err := Split(1000, n)
		if err != nil || len(parts) != n {
			t.Fatalf("Split(1000, %d) = %d parts, %v", n, len(parts), err)
		}
	}
}

func TestSplitPartsSumToTheTotal(t *testing.T) {
	for _, total := range []Cents{0, 1, 99, 100, 101, 12345} {
		for n := 1; n <= 12; n++ {
			parts, err := Split(total, n)
			if err != nil {
				t.Fatal(err)
			}
			var sum Cents
			for _, p := range parts {
				sum += p
			}
			if sum != total {
				t.Fatalf("Split(%d, %d) sums to %d", total, n, sum)
			}
		}
	}
}

func TestSplitPutsTheLargerPartsFirst(t *testing.T) {
	parts, err := Split(100, 3)
	if err != nil || !slices.Equal(parts, []Cents{34, 33, 33}) {
		t.Fatalf("Split(100, 3) = %v, %v; want [34 33 33]", parts, err)
	}
}

func TestSplitIntoMorePartsThanCents(t *testing.T) {
	parts, err := Split(2, 5)
	if err != nil || !slices.Equal(parts, []Cents{1, 1, 0, 0, 0}) {
		t.Fatalf("Split(2, 5) = %v, %v; want [1 1 0 0 0]", parts, err)
	}
}

func TestSplitRejectsInvalidArguments(t *testing.T) {
	cases := []struct {
		total Cents
		n     int
	}{{100, 0}, {100, -1}, {100, MaxParts + 1}, {-1, 2}}
	for _, c := range cases {
		parts, err := Split(c.total, c.n)
		if !errors.Is(err, ErrInvalidSplit) || parts != nil {
			t.Fatalf("Split(%d, %d) = %v, %v; want nil and ErrInvalidSplit", c.total, c.n, parts, err)
		}
	}
}

func TestSplitTheLargestTotal(t *testing.T) {
	parts, err := Split(math.MaxInt64, 3)
	if err != nil {
		t.Fatal(err)
	}
	var sum Cents
	for _, p := range parts {
		sum += p
	}
	if sum != math.MaxInt64 || parts[0]-parts[2] > 1 {
		t.Fatalf("Split(MaxInt64, 3) = %v", parts)
	}
}
```

## How to run

1. Only the new tests: `go test -count=1 -run '^TestSplit(ReturnsTheRequestedNumberOfParts|PartsSumToTheTotal|PutsTheLargerPartsFirst|IntoMorePartsThanCents|RejectsInvalidArguments|TheLargestTotal)$' ./internal/money`
2. The normal test command (`go test ./...`) runs them: yes.

OCTESTS status=COMPLETE criteria=6 passed=6 failed=0 gaps=0 not_run=0 head={{HEAD}}
