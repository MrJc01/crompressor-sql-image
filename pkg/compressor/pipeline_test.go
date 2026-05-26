package compressor

import (
	"crypto/sha256"
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrJc01/crompressor/pkg/codebook"
)

// createDummyCodebook creates a valid CROMDB file for testing.
func createDummyCodebook(t *testing.T, path string, cwSize int, cwCount int) {
	header := make([]byte, 512)
	copy(header[0:6], "CROMDB")
	binary.LittleEndian.PutUint16(header[6:8], 1)                // version
	binary.LittleEndian.PutUint16(header[8:10], uint16(cwSize))   // codeword size
	binary.LittleEndian.PutUint64(header[10:18], uint64(cwCount)) // codeword count
	binary.LittleEndian.PutUint64(header[18:26], 512)            // data offset

	// Generate deterministic patterns
	h := sha256.New()
	var codewords []byte
	for i := 0; i < cwCount; i++ {
		cw := make([]byte, cwSize)
		for j := range cw {
			cw[j] = byte((i * 7 + j * 3) % 256)
		}
		codewords = append(codewords, cw...)
		h.Write(cw)
	}
	copy(header[26:58], h.Sum(nil))

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if _, err := f.Write(header); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(codewords); err != nil {
		t.Fatal(err)
	}
}

func TestPipeline(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crom-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	blockSize := 8
	cwSize := blockSize * blockSize * 3
	cwCount := 256
	cbPath := filepath.Join(tmpDir, "test.cromdb")
	createDummyCodebook(t, cbPath, cwSize, cwCount)

	cb, err := codebook.Open(cbPath)
	if err != nil {
		t.Fatalf("failed to open codebook: %v", err)
	}
	defer cb.Close()

	// Create a test image (16x16 pixels)
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			// Deterministic color gradients
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 15),
				G: uint8(y * 15),
				B: uint8((x + y) * 7),
				A: 255,
			})
		}
	}

	// 1. Test Segmentation
	data, w, h := Segment(img, blockSize)
	if w != 16 || h != 16 {
		t.Errorf("expected size 16x16, got %dx%d", w, h)
	}
	expectedLen := 16 * 16 * 3
	if len(data) != expectedLen {
		t.Errorf("expected segment data size %d, got %d", expectedLen, len(data))
	}

	// 2. Test Compression
	payload, cwW, cwH, err := CompressImage(img, cb, blockSize)
	if err != nil {
		t.Fatalf("compression failed: %v", err)
	}
	if cwW != 16 || cwH != 16 {
		t.Errorf("expected compression dimensions 16x16, got %dx%d", cwW, cwH)
	}
	expectedPayloadLen := (16 / blockSize) * (16 / blockSize) * 2 // 4 blocks * 2 bytes
	if len(payload) != expectedPayloadLen {
		t.Errorf("expected payload len %d, got %d", expectedPayloadLen, len(payload))
	}

	// 3. Test Decompression
	reconImg, err := DecompressImage(payload, cb, cwW, cwH, blockSize)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}
	if reconImg.Bounds().Dx() != 16 || reconImg.Bounds().Dy() != 16 {
		t.Errorf("expected reconstructed size 16x16, got %dx%d", reconImg.Bounds().Dx(), reconImg.Bounds().Dy())
	}

	// 4. Test Metrics Calculation
	mse, psnr := CalculateMetrics(img, reconImg)
	t.Logf("Test execution metrics: MSE = %.4f, PSNR = %.4f dB", mse, psnr)
	if psnr < 0 {
		t.Errorf("invalid PSNR: %f", psnr)
	}
}
