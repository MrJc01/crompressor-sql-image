package codebook

import (
	"github.com/MrJc01/crompressor/internal/codebook"
	"github.com/MrJc01/crompressor/internal/search"
)

// Reader wraps the internal codebook.Reader for public use.
type Reader = codebook.Reader

// Header wraps the internal codebook.Header for public use.
type Header = codebook.Header

// Open opens a .cromdb codebook file and returns a Reader.
func Open(path string) (*Reader, error) {
	return codebook.Open(path)
}

// OpenFromBytes creates a Reader from raw bytes in memory (no file I/O).
// This is the primary entry point for WASM environments.
func OpenFromBytes(data []byte) (*Reader, error) {
	return codebook.OpenFromBytes(data)
}

// Searcher wraps the internal search.Searcher interface for public use.
type Searcher struct {
	inner search.Searcher
}

// NewSearcher creates a new public Searcher wrapper.
func NewSearcher(r *Reader) *Searcher {
	return &Searcher{inner: search.NewLSHSearcher(r)}
}

// FindBestMatch finds the closest codeword index for the given chunk.
func (s *Searcher) FindBestMatch(chunk []byte) (uint64, error) {
	res, err := s.inner.FindBestMatch(chunk)
	if err != nil {
		return 0, err
	}
	return res.CodebookID, nil
}

