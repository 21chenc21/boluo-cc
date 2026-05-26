package abstraction

import (
	"math"
	"math/rand"
)

// KMeansND — Lloyd's K-means in N-dimensional Euclidean space.
//   - points[i] = N-d feature vector for item i
//   - returns (assignment per item, K cluster centers)
//
// Initialization: k-means++ (probabilistic seeding) for better convergence vs
// pure random. For ~169 points and k≤30 converges in <20 iters.
//
// Bucket ID ordering: by ascending first-dimension center (consistent with
// 1-D KMeans1D where bucket 0 = lowest value).
func KMeansND(points [][]float64, k, maxIter int, seed int64) ([]int, [][]float64) {
	n := len(points)
	if n == 0 || k <= 0 {
		return nil, nil
	}
	if k > n {
		k = n
	}
	dim := len(points[0])
	rng := rand.New(rand.NewSource(seed))

	// k-means++ init: pick first center randomly, subsequent biased far.
	centers := make([][]float64, k)
	for j := range centers {
		centers[j] = make([]float64, dim)
	}
	firstIdx := rng.Intn(n)
	copy(centers[0], points[firstIdx])

	dists := make([]float64, n)
	for cIdx := 1; cIdx < k; cIdx++ {
		// Distance to nearest existing center.
		var totalD2 float64
		for i, p := range points {
			minD := math.Inf(1)
			for j := 0; j < cIdx; j++ {
				d := euclidD2(p, centers[j])
				if d < minD {
					minD = d
				}
			}
			dists[i] = minD
			totalD2 += minD
		}
		// Sample new center weighted by squared distance.
		r := rng.Float64() * totalD2
		var cum float64
		for i := 0; i < n; i++ {
			cum += dists[i]
			if r < cum {
				copy(centers[cIdx], points[i])
				break
			}
		}
	}

	assigns := make([]int, n)
	for iter := 0; iter < maxIter; iter++ {
		changed := false
		for i, p := range points {
			best := 0
			bestD := euclidD2(p, centers[0])
			for j := 1; j < k; j++ {
				d := euclidD2(p, centers[j])
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
		// Update centers.
		sums := make([][]float64, k)
		counts := make([]int, k)
		for j := range sums {
			sums[j] = make([]float64, dim)
		}
		for i, a := range assigns {
			counts[a]++
			for d := 0; d < dim; d++ {
				sums[a][d] += points[i][d]
			}
		}
		for j := 0; j < k; j++ {
			if counts[j] > 0 {
				for d := 0; d < dim; d++ {
					centers[j][d] = sums[j][d] / float64(counts[j])
				}
			}
		}
		if !changed {
			break
		}
	}

	// Reorder buckets by ascending sum-of-center for deterministic output.
	type cIdx struct {
		val float64
		old int
	}
	idx := make([]cIdx, k)
	for j := 0; j < k; j++ {
		var s float64
		for _, v := range centers[j] {
			s += v
		}
		idx[j] = cIdx{s, j}
	}
	// Sort by sum ascending.
	for i := 1; i < len(idx); i++ {
		for j := i; j > 0 && idx[j-1].val > idx[j].val; j-- {
			idx[j-1], idx[j] = idx[j], idx[j-1]
		}
	}

	oldToNew := make([]int, k)
	newCenters := make([][]float64, k)
	for newIdx, c := range idx {
		oldToNew[c.old] = newIdx
		newCenters[newIdx] = centers[c.old]
	}
	for i := range assigns {
		assigns[i] = oldToNew[assigns[i]]
	}
	return assigns, newCenters
}

func euclidD2(a, b []float64) float64 {
	var sum float64
	for i := range a {
		d := a[i] - b[i]
		sum += d * d
	}
	return sum
}
