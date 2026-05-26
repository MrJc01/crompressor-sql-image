package search

// MatchResult represents the outcome of a search operation.
type MatchResult struct {
	// CodebookID is the index of the matching codeword in the Codebook.
	CodebookID uint64

	// Pattern is the actual byte content of the codeword.
	Pattern []byte

	// Distance is the quantitative difference between the chunk and the codeword.
	// For SSD (Sum of Squared Differences), 0 means perfect match.
	Distance int
}

// Similarity returns a 0.0-1.0 value representing how closely the match
// resembles the input chunk. 1.0 = perfect match (distance=0), 0.0 = completely different.
// chunkBits is len(chunk)*8 (total bits in the input).
func (m MatchResult) Similarity(chunkBits int) float64 {
	if chunkBits == 0 {
		return 0
	}
	// For SSD, maximum possible distance is length * 255 * 255
	length := chunkBits / 8
	maxDist := float64(length * 255 * 255)
	if maxDist == 0 {
		return 0
	}
	s := 1.0 - float64(m.Distance)/maxDist
	if s < 0 {
		return 0
	}
	return s
}

// Searcher defines the interface for finding patterns in a Codebook.
type Searcher interface {
	// FindBestMatch searches for the codeword that is most similar to the given chunk.
	FindBestMatch(chunk []byte) (MatchResult, error)
	Restrict(allowed []uint64)
}

// ssdDistance calculates the Sum of Squared Differences (SSD) between two byte slices.
func ssdDistance(a, b []byte) int {
	dist := 0
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	// Simple loop that Go compiler can optimize / vectorize
	for i := 0; i < minLen; i++ {
		diff := int(a[i]) - int(b[i])
		dist += diff * diff
	}

	// If lengths are different, missing bytes count as maximum difference (255^2)
	if len(a) != len(b) {
		diffLen := len(a) - len(b)
		if diffLen < 0 {
			diffLen = -diffLen
		}
		dist += diffLen * 255 * 255
	}

	return dist
}
