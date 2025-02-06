package crawler

import (
	"testing"
)

// This really isn't thorough enough.
func TestGoodSeats(t *testing.T) {
	tcs := []struct {
		name string
		good bool
		// Seats are "." for reserved or "a" for available.
		grid [][]string
	}{
		{
			name: "empty",
			good: true,
			grid: [][]string{
				{"a", "a", "a", "a", "a", "a", "a", "a"},
				{"a", "a", "a", "a", "a", "a", "a", "a"},
				{"a", "a", "a", "a", "a", "a", "a", "a"},
				{"a", "a", "a", "a", "a", "a", "a", "a"},
				{"a", "a", "a", "a", "a", "a", "a", "a"},
				{"a", "a", "a", "a", "a", "a", "a", "a"},
				{"a", "a", "a", "a", "a", "a", "a", "a"},
			},
		},
		{
			name: "too shallow",
			good: false,
			grid: [][]string{
				{"a", "a", "a", "a", "a", "a", "a", "a"},
				{"a", "a", "a", "a", "a", "a", "a", "a"},
			},
		},
		{
			name: "too skinny",
			good: false,
			grid: [][]string{
				{"a", "a"},
				{"a", "a"},
				{"a", "a"},
				{"a", "a"},
				{"a", "a"},
				{"a", "a"},
				{"a", "a"},
				{"a", "a"},
			},
		},
		{
			name: "only one",
			good: false,
			grid: [][]string{
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", "a", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
			},
		},
		{
			name: "too close to edges",
			good: false,
			grid: [][]string{
				{".", ".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", "a", "a", ".", ".", ".", "."},
				{".", ".", "a", "a", ".", "a", "a", ".", "."},
				{".", ".", "a", "a", ".", "a", "a", ".", "."},
				{".", ".", ".", ".", "a", "a", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", ".", "."},
			},
		},
		{
			name: "enough space",
			good: true,
			grid: [][]string{
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", "a", "a", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
			},
		},
		{
			name: "multiple spaces",
			good: true,
			grid: [][]string{
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", "a", "a", ".", ".", "."},
				{".", ".", ".", "a", "a", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
			},
		},
		{
			name: "short row failure",
			good: false,
			grid: [][]string{
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", "a", "a", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
			},
		},
		{
			name: "short row pass",
			good: true,
			grid: [][]string{
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", "a", "a", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
				{".", ".", ".", ".", ".", ".", ".", "."},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			// Translate grid to a []seat.
			var seats []seat
			var maxRow int
			var maxCol int
			for i, row := range tc.grid {
				for j, cell := range row {
					maxRow = max(maxRow, i)
					maxCol = max(maxRow, j)
					st := seat{
						row: i,
						col: j,
					}
					switch cell {
					case "a":
					case ".":
						st.reserved = true
					default:
						t.Fatalf("invalid grid character %q", cell)
					}
					seats = append(seats, st)
				}
			}

			if got := checkSeats(seats, maxRow, maxCol, 2); got != tc.good {
				t.Errorf("expected checkSeats() to return %t, but got %t", tc.good, got)
			}
		})
	}
}
