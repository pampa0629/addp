package models

import (
	"math"
	"testing"
)

func TestListPageBounds(t *testing.T) {
	for _, size := range []int{1, 20, 100} {
		last := math.MaxInt32/size + 1
		for _, page := range []ListPage{{1, size}, {last, size}} {
			if !page.Valid() || page.Offset() < 0 || page.Offset() > math.MaxInt32 {
				t.Fatalf("valid page rejected: %+v", page)
			}
		}
		if (ListPage{last + 1, size}).Valid() {
			t.Fatal("OFFSET overflow accepted")
		}
	}
	for _, p := range []ListPage{{0, 20}, {-1, 20}, {1, 0}, {1, -1}, {1, 101}, {math.MaxInt, 100}} {
		if p.Valid() {
			t.Fatalf("invalid page accepted: %+v", p)
		}
	}
}
