package cfr

import (
	"testing"
)

// BenchmarkCFRIter — single vanilla CFR iteration over the full Leduc tree.
// Use as A/B test baseline for engine refactors (int hash, snapshot/restore).
//
// Run: go test ./cfr -bench BenchmarkCFRIter -benchmem -benchtime=20x
func BenchmarkCFRIter(b *testing.B) {
	c := New()
	// Warm up: visit all infosets so map allocation isn't measured.
	c.Iter()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Iter()
	}
}

// BenchmarkBestResponse — full BR computation cost (Day 2's hot path during convergence eval).
func BenchmarkBestResponse(b *testing.B) {
	c := New()
	for i := 0; i < 100; i++ {
		c.Iter()
	}
	avg := c.AverageStrategy()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Exploitability(avg)
	}
}

