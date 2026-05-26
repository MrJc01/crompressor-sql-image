package search

import (
	"crypto/rand"
	"testing"
)

func BenchmarkSSDDistance(b *testing.B) {
	// 48 bytes (standard color block size for 4x4)
	chunkA := make([]byte, 48)
	chunkB := make([]byte, 48)
	rand.Read(chunkA)
	rand.Read(chunkB)

	b.Run("Standard", func(b *testing.B) {
		b.SetBytes(48)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = ssdDistance(chunkA, chunkB)
		}
	})
}
