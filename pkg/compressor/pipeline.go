package compressor

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"

	"github.com/MrJc01/crompressor/pkg/codebook"
)

// LoadImage loads an image from the filesystem (supports JPEG and PNG).
func LoadImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open image file: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}

	return img, nil
}

// ConvertToRGBA converts any image to an image.RGBA.
func ConvertToRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}
	bounds := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, bounds.Min, draw.Src)
	return rgba
}

// Segment extracts non-overlapping BxB blocks from the image in row-major order.
// Returns a flat slice of block bytes (BxBx3 per block) and the adjusted width/height (multiples of B).
func Segment(img image.Image, blockSize int) ([]byte, int, int) {
	rgba := ConvertToRGBA(img)
	bounds := rgba.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Adjust width and height to be multiples of blockSize (crop boundaries)
	adjW := (w / blockSize) * blockSize
	adjH := (h / blockSize) * blockSize

	blockByteSize := blockSize * blockSize * 3
	numBlocks := (adjW / blockSize) * (adjH / blockSize)
	data := make([]byte, numBlocks*blockByteSize)

	offset := 0
	for by := 0; by < adjH; by += blockSize {
		for bx := 0; bx < adjW; bx += blockSize {
			// Extract block of size blockSize x blockSize
			for y := 0; y < blockSize; y++ {
				for x := 0; x < blockSize; x++ {
					pixelY := by + y
					pixelX := bx + x
					pixOffset := rgba.PixOffset(pixelX, pixelY)
					
					// Get RGB values (ignoring alpha channel)
					data[offset] = rgba.Pix[pixOffset]   // R
					data[offset+1] = rgba.Pix[pixOffset+1] // G
					data[offset+2] = rgba.Pix[pixOffset+2] // B
					offset += 3
				}
			}
		}
	}

	return data, adjW, adjH
}

// CompressImage segments the image and compresses it using CROM codebook vector quantization.
// Returns a payload containing uint16 indices, and the width/height.
func CompressImage(img image.Image, cb *codebook.Reader, blockSize int) ([]byte, int, int, error) {
	flatBlocks, w, h := Segment(img, blockSize)
	blockByteSize := blockSize * blockSize * 3
	numBlocks := len(flatBlocks) / blockByteSize

	searcher := codebook.NewSearcher(cb)
	payload := make([]byte, numBlocks*2) // uint16 per block

	for i := 0; i < numBlocks; i++ {
		start := i * blockByteSize
		end := start + blockByteSize
		blockData := flatBlocks[start:end]

		idx, err := searcher.FindBestMatch(blockData)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("failed to compress block %d: %w", i, err)
		}

		binary.LittleEndian.PutUint16(payload[i*2:(i+1)*2], uint16(idx))
	}

	return payload, w, h, nil
}

// DecompressImage reconstructs the image from its CROM indices using the codebook.
func DecompressImage(payload []byte, cb *codebook.Reader, w, h, blockSize int) (image.Image, error) {
	numBlocks := len(payload) / 2
	blockByteSize := blockSize * blockSize * 3

	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	blocksPerRow := w / blockSize

	for i := 0; i < numBlocks; i++ {
		idx := binary.LittleEndian.Uint16(payload[i*2 : (i+1)*2])
		
		pattern, err := cb.Lookup(uint64(idx))
		if err != nil {
			return nil, fmt.Errorf("failed to lookup codeword %d: %w", idx, err)
		}

		// Ensure codeword length matches block size
		if len(pattern) < blockByteSize {
			return nil, fmt.Errorf("codeword %d size too small: got %d, want %d", idx, len(pattern), blockByteSize)
		}

		// Write block back to image.RGBA
		blockX := (i % blocksPerRow) * blockSize
		blockY := (i / blocksPerRow) * blockSize

		offset := 0
		for y := 0; y < blockSize; y++ {
			for x := 0; x < blockSize; x++ {
				pixelX := blockX + x
				pixelY := blockY + y
				pixOffset := rgba.PixOffset(pixelX, pixelY)

				rgba.Pix[pixOffset] = pattern[offset]
				rgba.Pix[pixOffset+1] = pattern[offset+1]
				rgba.Pix[pixOffset+2] = pattern[offset+2]
				rgba.Pix[pixOffset+3] = 255 // Alpha is fully opaque
				offset += 3
			}
		}
	}

	return rgba, nil
}

// CalculateMetrics computes MSE and PSNR between original and reconstructed images.
// Both images are expected to have the same bounds.
func CalculateMetrics(orig, recon image.Image) (mse float64, psnr float64) {
	rgbaOrig := ConvertToRGBA(orig)
	rgbaRecon := ConvertToRGBA(recon)

	bounds := rgbaOrig.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	var squaredErrorSum float64
	pixelCount := 0

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			offsetO := rgbaOrig.PixOffset(x, y)
			offsetR := rgbaRecon.PixOffset(x, y)

			// Compare RGB channels
			dr := float64(rgbaOrig.Pix[offsetO]) - float64(rgbaRecon.Pix[offsetR])
			dg := float64(rgbaOrig.Pix[offsetO+1]) - float64(rgbaRecon.Pix[offsetR+1])
			db := float64(rgbaOrig.Pix[offsetO+2]) - float64(rgbaRecon.Pix[offsetR+2])

			squaredErrorSum += dr*dr + dg*dg + db*db
			pixelCount++
		}
	}

	// Mean squared error across all color components
	mse = squaredErrorSum / float64(pixelCount*3)
	if mse == 0 {
		return 0, 99.0 // Perfect match
	}

	// Calculate PSNR
	psnr = 10 * math.Log10((255*255)/mse)
	return mse, psnr
}
