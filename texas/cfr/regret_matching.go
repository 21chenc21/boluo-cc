package cfr

// regretMatching — vanilla regret matching: σ ∝ max(R, 0), uniform if all non-positive.
// Writes into out (length must equal len(regret)) and returns it.
func regretMatching(regret, out []float64) []float64 {
	var sum float64
	for i, r := range regret {
		if r > 0 {
			out[i] = r
			sum += r
		} else {
			out[i] = 0
		}
	}
	if sum > 0 {
		for i := range out {
			out[i] /= sum
		}
		return out
	}
	u := 1.0 / float64(len(out))
	for i := range out {
		out[i] = u
	}
	return out
}
