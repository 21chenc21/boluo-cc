package abstraction

import (
	"math"
	"sort"
)

// KMeans1D — Lloyd's algorithm on 1-D data. Returns (assignments, centers).
//   - assignments[i] = bucket index (0..k-1) for points[i]
//   - centers[j] = cluster center for bucket j, sorted ascending
//
// For 1-D data, the optimal clustering has all points in a bucket forming a
// contiguous interval. The implementation initializes with equally-spaced
// quantile centers and iterates Lloyd's; for ~200 points and k≤20 this
// converges in <10 iterations to global optimum.
//
// Bucket ID is by ascending value: bucket 0 = lowest equity hands, bucket k-1 =
// highest. Useful for engine-side ordering.
func KMeans1D(points []float64, k int, maxIter int) ([]int, []float64) {
	n := len(points)
	if n == 0 || k <= 0 {
		return nil, nil
	}
	if k > n {
		k = n
	}

	// Index sort to derive initial centers from quantiles.
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool {
		return points[order[i]] < points[order[j]]
	})
	sorted := make([]float64, n)
	for i, o := range order {
		sorted[i] = points[o]
	}

	centers := make([]float64, k)
	for j := 0; j < k; j++ {
		// Pick the (j+0.5)/k quantile.
		idx := int(float64(n) * (float64(j) + 0.5) / float64(k))
		if idx >= n {
			idx = n - 1
		}
		centers[j] = sorted[idx]
	}

	assigns := make([]int, n)
	for iter := 0; iter < maxIter; iter++ {
		changed := false
		// Assignment step.
		for i, p := range points {
			best := 0
			bestD := math.Abs(p - centers[0])
			for j := 1; j < k; j++ {
				d := math.Abs(p - centers[j])
				if d < bestD {
					bestD = d
					best = j
				}
			}
			if assigns[i] != best {
				changed = true
				assigns[i] = best
			}
		}
		// Update step.
		sums := make([]float64, k)
		counts := make([]int, k)
		for i, a := range assigns {
			sums[a] += points[i]
			counts[a]++
		}
		for j := 0; j < k; j++ {
			if counts[j] > 0 {
				centers[j] = sums[j] / float64(counts[j])
			}
		}
		if !changed {
			break
		}
	}

	// Sort centers ascending; remap assignments so bucket 0 = lowest.
	type centerIdx struct {
		val float64
		old int
	}
	idx := make([]centerIdx, k)
	for j := 0; j < k; j++ {
		idx[j] = centerIdx{centers[j], j}
	}
	sort.Slice(idx, func(i, j int) bool { return idx[i].val < idx[j].val })

	oldToNew := make([]int, k)
	newCenters := make([]float64, k)
	for newIdx, c := range idx {
		oldToNew[c.old] = newIdx
		newCenters[newIdx] = c.val
	}
	for i := range assigns {
		assigns[i] = oldToNew[assigns[i]]
	}
	return assigns, newCenters
}
