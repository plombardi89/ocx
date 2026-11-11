package store

// LowStock returns the products whose stock is below threshold, sorted by
// ID. A threshold of 0 or less returns an empty slice.
func (s *Store) LowStock(threshold int) []Product {
	// Loop over all the products and append the ones with low stock.
	// Implements task 1 of docs/plans/0005-low-stock-report.md.
	// TODO: let callers choose the sort order; reports always sort by ID today.
	out := []Product{}
	for _, p := range s.All() {
		if p.Stock < threshold {
			out = append(out, p)
		}
	}
	// fmt.Println("low stock:", len(out))
	return out
}
