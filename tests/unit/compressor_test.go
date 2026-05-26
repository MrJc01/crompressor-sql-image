package unit

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrJc01/crompressor/pkg/codebook"
	"crompressor-sql-image/pkg/compressor"
)

// createDummyCodebook creates a valid CROMDB file for testing.
func createDummyCodebook(t *testing.T, path string, cwSize int, cwCount int) {
	header := make([]byte, 512)
	copy(header[0:6], "CROMDB")
	binary.LittleEndian.PutUint16(header[6:8], 1)                // version
	binary.LittleEndian.PutUint16(header[8:10], uint16(cwSize))   // codeword size
	binary.LittleEndian.PutUint64(header[10:18], uint64(cwCount)) // codeword count
	binary.LittleEndian.PutUint64(header[18:26], 512)            // data offset

	h := sha256.New()
	var codewords []byte
	for i := 0; i < cwCount; i++ {
		cw := make([]byte, cwSize)
		for j := range cw {
			cw[j] = byte((i * 13 + j * 7) % 256)
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

func TestCompressorNonDivisibleDimensions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crom-unit-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	blockSize := 8
	cwSize := blockSize * blockSize * 3
	cwCount := 64
	cbPath := filepath.Join(tmpDir, "test.cromdb")
	createDummyCodebook(t, cbPath, cwSize, cwCount)

	cb, err := codebook.Open(cbPath)
	if err != nil {
		t.Fatalf("failed to open codebook: %v", err)
	}
	defer cb.Close()

	// Create a test image with non-divisible dimensions: 15x19 pixels
	img := image.NewRGBA(image.Rect(0, 0, 15, 19))
	for y := 0; y < 19; y++ {
		for x := 0; x < 15; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 10), G: uint8(y * 10), B: 128, A: 255})
		}
	}

	// 1. Segment must crop dimensions to multiples of blockSize (15 -> 8, 19 -> 16)
	data, w, h := compressor.Segment(img, blockSize)
	if w != 8 || h != 16 {
		t.Errorf("expected cropped dimensions 8x16, got %dx%d", w, h)
	}
	expectedLen := 8 * 16 * 3
	if len(data) != expectedLen {
		t.Errorf("expected segmented data length %d, got %d", expectedLen, len(data))
	}

	// 2. Compress should run on cropped dimensions
	payload, cwW, cwH, err := compressor.CompressImage(img, cb, blockSize)
	if err != nil {
		t.Fatalf("compression failed: %v", err)
	}
	if cwW != 8 || cwH != 16 {
		t.Errorf("expected compression dimensions 8x16, got %dx%d", cwW, cwH)
	}
	expectedPayloadLen := (8 / blockSize) * (16 / blockSize) * 2 // 2 blocks * 2 bytes = 4 bytes
	if len(payload) != expectedPayloadLen {
		t.Errorf("expected payload length %d, got %d", expectedPayloadLen, len(payload))
	}

	// 3. Decompress
	reconImg, err := compressor.DecompressImage(payload, cb, cwW, cwH, blockSize)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}
	if reconImg.Bounds().Dx() != 8 || reconImg.Bounds().Dy() != 16 {
		t.Errorf("expected reconstructed dimensions 8x16, got %dx%d", reconImg.Bounds().Dx(), reconImg.Bounds().Dy())
	}
}

func TestCalculateMetrics(t *testing.T) {
	// Test MSE/PSNR on identical images
	img1 := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img2 := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img1.SetRGBA(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
			img2.SetRGBA(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}

	mse, psnr := compressor.CalculateMetrics(img1, img2)
	if mse != 0 {
		t.Errorf("expected MSE = 0 for identical images, got %f", mse)
	}
	if psnr != 99.0 {
		t.Errorf("expected PSNR = 99.0 for identical images, got %f", psnr)
	}

	// Test with slight difference
	img2.SetRGBA(0, 0, color.RGBA{R: 101, G: 150, B: 200, A: 255}) // difference of 1 on Red channel
	mse, psnr = compressor.CalculateMetrics(img1, img2)
	if mse <= 0 {
		t.Errorf("expected MSE > 0, got %f", mse)
	}
	if psnr >= 99.0 || psnr <= 0 {
		t.Errorf("invalid PSNR for different images, got %f", psnr)
	}
}

func TestCompressorIntegrityConstraints(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crom-integrity-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	blockSize := 4
	cwSize := blockSize * blockSize * 3
	cwCount := 128
	cbPath := filepath.Join(tmpDir, "test_integrity.cromdb")
	createDummyCodebook(t, cbPath, cwSize, cwCount)

	cb, err := codebook.Open(cbPath)
	if err != nil {
		t.Fatalf("failed to open codebook: %v", err)
	}
	defer cb.Close()

	// Create a test image (e.g. 64x64 pixels)
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 100, A: 255})
		}
	}

	rawSize := 64 * 64 * 3 // 12,288 bytes

	// 1. Compress
	payload, w, h, err := compressor.CompressImage(img, cb, blockSize)
	if err != nil {
		t.Fatalf("compression failed: %v", err)
	}

	// 2. Validate Size Constraint: CROM indices must be significantly smaller than raw RGB
	cromSize := len(payload)
	expectedCromSize := (64 / blockSize) * (64 / blockSize) * 2 // 16 * 16 * 2 = 512 bytes
	if cromSize != expectedCromSize {
		t.Errorf("expected CROM size %d, got %d", expectedCromSize, cromSize)
	}
	if cromSize >= rawSize {
		t.Errorf("integrity violation: CROM payload size (%d) is not smaller than original raw size (%d)", cromSize, rawSize)
	}

	// 3. Decompress & Validate Structural Integrity
	reconImg, err := compressor.DecompressImage(payload, cb, w, h, blockSize)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}

	if reconImg.Bounds().Dx() != w || reconImg.Bounds().Dy() != h {
		t.Errorf("reconstructed dimensions mismatch: got %dx%d, want %dx%d", reconImg.Bounds().Dx(), reconImg.Bounds().Dy(), w, h)
	}

	// 4. Validate Likeness Constraint: PSNR must be a valid positive number
	mse, psnr := compressor.CalculateMetrics(img, reconImg)
	t.Logf("Integrity test metrics: MSE = %.2f, PSNR = %.2f dB", mse, psnr)
	if psnr <= 0 || mse < 0 {
		t.Errorf("visual likeness integrity failed: invalid PSNR (%.2f dB) or MSE (%.2f)", psnr, mse)
	}
}

func TestCompressedPayloadDiskComparison(t *testing.T) {
	// Open the actual codebook used by the application
	cbPath := "../../codebook_4.cromdb"
	if _, err := os.Stat(cbPath); os.IsNotExist(err) {
		cbPath = "../../codebook.cromdb"
	}
	if _, err := os.Stat(cbPath); os.IsNotExist(err) {
		t.Skip("actual codebook not found; skipping comparison test")
	}

	cb, err := codebook.Open(cbPath)
	if err != nil {
		t.Fatalf("failed to open codebook: %v", err)
	}
	defer cb.Close()

	blockSize := 4
	if filepath.Base(cbPath) == "codebook.cromdb" {
		blockSize = 8
	}

	// Test on a few files from testing_dataset
	testDir := "../../testing_dataset"
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		t.Skip("testing_dataset not found; skipping comparison test")
	}

	files, err := os.ReadDir(testDir)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		ext := filepath.Ext(f.Name())
		if ext != ".jpg" && ext != ".jpeg" && ext != ".png" {
			continue
		}

		path := filepath.Join(testDir, f.Name())
		img, err := compressor.LoadImage(path)
		if err != nil {
			continue
		}

		payload, w, h, err := compressor.CompressImage(img, cb, blockSize)
		if err != nil {
			t.Errorf("failed to compress %s: %v", f.Name(), err)
			continue
		}

		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		origSize := fi.Size()
		cromSize := len(payload)

		// Gzip compression simulation
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		gw.Write(payload)
		gw.Close()
		gzippedSize := buf.Len()

		t.Logf("Image %s: Original File Size = %d bytes, CROM Payload = %d bytes, Gzipped CROM = %d bytes",
			f.Name(), origSize, cromSize, gzippedSize)

		// Assert that for non-trivial images (e.g. original size > 8KB), the CROM/gzipped size is smaller
		if origSize > 8192 {
			if gzippedSize >= int(origSize) {
				t.Errorf("Integrity violation for %s: Gzipped CROM payload (%d) is not smaller than original file size (%d)",
					f.Name(), gzippedSize, origSize)
			}
		}

		// Verify decompression integrity
		reconImg, err := compressor.DecompressImage(payload, cb, w, h, blockSize)
		if err != nil {
			t.Errorf("failed to decompress %s: %v", f.Name(), err)
			continue
		}

		if reconImg.Bounds().Dx() != w || reconImg.Bounds().Dy() != h {
			t.Errorf("dimensions mismatch on reconstruction of %s", f.Name())
		}
	}
}
