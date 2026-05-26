//go:build ignore

// generate_report.go — Standalone CROM Quality Report Generator
// Usage: go run generate_report.go
//
// Compresses images from training_dataset/ and testing_dataset/,
// measures MSE, PSNR, and structural similarity, then produces
// a Markdown report at report_output/similarity_report.md.

package main

import (
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MrJc01/crompressor/pkg/codebook"
	"crompressor-sql-image/pkg/compressor"
)

const (
	codebookPath  = "codebook_4.cromdb"
	blockSize     = 4
	maxPerDataset = 15 // limit per dataset to keep runtime reasonable
	outputDir     = "report_output"
)

type ImageResult struct {
	Name       string
	Dataset    string // "train" or "test"
	Width      int
	Height     int
	OrigSize   int64
	CromSize   int
	MSE        float64
	PSNR       float64
	SSIM       float64
	Ratio      float64
	CompressMs int64
}

func main() {
	start := time.Now()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║          CROM SQL — Similarity Report Generator             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")

	// 1. Load codebook
	fmt.Println("\n[1/4] Loading codebook...")
	cb, err := codebook.Open(codebookPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Cannot open codebook %s: %v\n", codebookPath, err)
		os.Exit(1)
	}
	hdr := cb.Header()
	fmt.Printf("       Codebook: %d codewords, codeword size %d bytes, block %dx%d\n",
		hdr.CodewordCount, hdr.CodewordSize, blockSize, blockSize)

	// 2. Create output dir
	os.MkdirAll(filepath.Join(outputDir, "recon_train"), 0o755)
	os.MkdirAll(filepath.Join(outputDir, "recon_test"), 0o755)

	// 3. Process datasets
	fmt.Println("\n[2/4] Processing training images...")
	trainResults := processDataset("training_dataset", "train", cb)

	fmt.Println("\n[3/4] Processing testing images...")
	testResults := processDataset("testing_dataset", "test", cb)

	// 4. Generate report
	fmt.Println("\n[4/4] Generating Markdown report...")
	allResults := append(trainResults, testResults...)
	generateReport(allResults, time.Since(start))

	fmt.Printf("\n✅ Report generated: %s/similarity_report.md\n", outputDir)
	fmt.Printf("   Reconstructed images saved in %s/recon_train/ and %s/recon_test/\n", outputDir, outputDir)
	fmt.Printf("   Total time: %s\n", time.Since(start).Round(time.Millisecond))
}

func processDataset(dirPath, label string, cb *codebook.Reader) []ImageResult {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  WARN: Cannot read %s: %v\n", dirPath, err)
		return nil
	}

	var imageFiles []string
	for _, e := range entries {
		name := e.Name()
		lower := strings.ToLower(name)
		if !e.IsDir() && (strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".png")) {
			imageFiles = append(imageFiles, name)
		}
	}
	sort.Strings(imageFiles)

	if len(imageFiles) > maxPerDataset {
		imageFiles = imageFiles[:maxPerDataset]
	}

	var results []ImageResult
	for i, name := range imageFiles {
		fmt.Printf("  [%d/%d] %s ... ", i+1, len(imageFiles), name)
		r := processImage(filepath.Join(dirPath, name), label, cb)
		if r != nil {
			results = append(results, *r)
			fmt.Printf("PSNR=%.2f dB, SSIM=%.4f, ratio=%.1f:1 (%dms)\n",
				r.PSNR, r.SSIM, r.Ratio, r.CompressMs)
		} else {
			fmt.Println("SKIP")
		}
	}
	return results
}

func processImage(path, label string, cb *codebook.Reader) *ImageResult {
	// Load original
	img, err := compressor.LoadImage(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "err: %v ", err)
		return nil
	}

	// Get original file size
	fi, _ := os.Stat(path)
	origSize := fi.Size()

	// Compress
	t0 := time.Now()
	payload, w, h, err := compressor.CompressImage(img, cb, blockSize)
	compressMs := time.Since(t0).Milliseconds()
	if err != nil {
		fmt.Fprintf(os.Stderr, "compress err: %v ", err)
		return nil
	}

	// Decompress
	reconImg, err := compressor.DecompressImage(payload, cb, w, h, blockSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "decompress err: %v ", err)
		return nil
	}

	// Calculate metrics
	mse, psnr := compressor.CalculateMetrics(img, reconImg)
	ssim := calculateSSIM(img, reconImg)

	// Save reconstructed image
	reconPath := filepath.Join(outputDir, "recon_"+label, filepath.Base(path))
	saveJPEG(reconImg, reconPath)

	ratio := float64(origSize) / float64(len(payload))

	return &ImageResult{
		Name:       filepath.Base(path),
		Dataset:    label,
		Width:      w,
		Height:     h,
		OrigSize:   origSize,
		CromSize:   len(payload),
		MSE:        mse,
		PSNR:       psnr,
		SSIM:       ssim,
		Ratio:      ratio,
		CompressMs: compressMs,
	}
}

func saveJPEG(img image.Image, path string) {
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	jpeg.Encode(f, img, &jpeg.Options{Quality: 95})
}

// calculateSSIM computes a simplified SSIM between two images.
// Uses the standard SSIM formula with luminance, contrast, and structure terms.
func calculateSSIM(orig, recon image.Image) float64 {
	origRGBA := compressor.ConvertToRGBA(orig)
	reconRGBA := compressor.ConvertToRGBA(recon)

	bounds := origRGBA.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// Use an 8x8 sliding window
	const windowSize = 8
	const C1 = 6.5025   // (0.01*255)^2
	const C2 = 58.5225  // (0.03*255)^2

	var ssimSum float64
	var windowCount int

	for wy := 0; wy+windowSize <= h; wy += windowSize {
		for wx := 0; wx+windowSize <= w; wx += windowSize {
			// Compute mean and variance for each window
			var sumO, sumR float64
			var sumO2, sumR2, sumOR float64
			n := float64(windowSize * windowSize * 3) // RGB channels

			for y := wy; y < wy+windowSize; y++ {
				for x := wx; x < wx+windowSize; x++ {
					oOff := origRGBA.PixOffset(x, y)
					rOff := reconRGBA.PixOffset(x, y)

					for c := 0; c < 3; c++ {
						o := float64(origRGBA.Pix[oOff+c])
						r := float64(reconRGBA.Pix[rOff+c])
						sumO += o
						sumR += r
						sumO2 += o * o
						sumR2 += r * r
						sumOR += o * r
					}
				}
			}

			muO := sumO / n
			muR := sumR / n
			sigmaO2 := sumO2/n - muO*muO
			sigmaR2 := sumR2/n - muR*muR
			sigmaOR := sumOR/n - muO*muR

			// SSIM formula
			num := (2*muO*muR + C1) * (2*sigmaOR + C2)
			den := (muO*muO + muR*muR + C1) * (sigmaO2 + sigmaR2 + C2)
			ssimSum += num / den
			windowCount++
		}
	}

	if windowCount == 0 {
		return 0
	}
	return ssimSum / float64(windowCount)
}

func generateReport(results []ImageResult, totalTime time.Duration) {
	f, err := os.Create(filepath.Join(outputDir, "similarity_report.md"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Cannot create report: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	// Separate results
	var train, test []ImageResult
	for _, r := range results {
		if r.Dataset == "train" {
			train = append(train, r)
		} else {
			test = append(test, r)
		}
	}

	// Compute averages
	avgTrain := computeAvg(train)
	avgTest := computeAvg(test)
	avgAll := computeAvg(results)

	fmt.Fprintf(f, "# 📊 CROM SQL — Image Quality & Similarity Report\n\n")
	fmt.Fprintf(f, "> Generated: %s | Total processing time: %s\n\n", time.Now().Format("2006-01-02 15:04:05"), totalTime.Round(time.Millisecond))

	// Executive Summary
	fmt.Fprintf(f, "## Executive Summary\n\n")
	fmt.Fprintf(f, "| Metric | Training Set (%d imgs) | Testing Set (%d imgs) | Overall (%d imgs) |\n",
		len(train), len(test), len(results))
	fmt.Fprintf(f, "|--------|------------------------|------------------------|--------------------|\n")
	fmt.Fprintf(f, "| **Avg MSE** | %.2f | %.2f | %.2f |\n", avgTrain.MSE, avgTest.MSE, avgAll.MSE)
	fmt.Fprintf(f, "| **Avg PSNR (dB)** | %.2f | %.2f | %.2f |\n", avgTrain.PSNR, avgTest.PSNR, avgAll.PSNR)
	fmt.Fprintf(f, "| **Avg SSIM** | %.4f | %.4f | %.4f |\n", avgTrain.SSIM, avgTest.SSIM, avgAll.SSIM)
	fmt.Fprintf(f, "| **Avg Compression Ratio** | %.1f:1 | %.1f:1 | %.1f:1 |\n", avgTrain.Ratio, avgTest.Ratio, avgAll.Ratio)
	fmt.Fprintf(f, "| **Avg Compress Time** | %dms | %dms | %dms |\n\n", avgTrain.CompressMs, avgTest.CompressMs, avgAll.CompressMs)

	// Quality Assessment
	fmt.Fprintf(f, "## Quality Assessment\n\n")
	fmt.Fprintf(f, "| PSNR Range | Quality Level | Status |\n")
	fmt.Fprintf(f, "|------------|--------------|--------|\n")
	fmt.Fprintf(f, "| > 30 dB | Excellent — near lossless | ✅ |\n")
	fmt.Fprintf(f, "| 25-30 dB | Good — minor artifacts | 🟡 |\n")
	fmt.Fprintf(f, "| 20-25 dB | Fair — visible degradation | 🟠 |\n")
	fmt.Fprintf(f, "| < 20 dB | Poor — significant loss | 🔴 |\n\n")

	psnrLevel := "🔴 Poor"
	if avgAll.PSNR >= 30 {
		psnrLevel = "✅ Excellent"
	} else if avgAll.PSNR >= 25 {
		psnrLevel = "🟡 Good"
	} else if avgAll.PSNR >= 20 {
		psnrLevel = "🟠 Fair"
	}
	fmt.Fprintf(f, "**Current Overall Level: %s (%.2f dB)**\n\n", psnrLevel, avgAll.PSNR)

	// Generalization Analysis
	fmt.Fprintf(f, "## Generalization Analysis (Train vs Test)\n\n")
	psnrDelta := avgTrain.PSNR - avgTest.PSNR
	ssimDelta := avgTrain.SSIM - avgTest.SSIM
	fmt.Fprintf(f, "| Metric | Delta (Train − Test) | Assessment |\n")
	fmt.Fprintf(f, "|--------|---------------------|------------|\n")
	psnrAssess := "✅ Generalizes well"
	if math.Abs(psnrDelta) > 3 {
		psnrAssess = "🔴 Significant gap — overfitting"
	} else if math.Abs(psnrDelta) > 1.5 {
		psnrAssess = "🟡 Moderate gap"
	}
	ssimAssess := "✅ Generalizes well"
	if math.Abs(ssimDelta) > 0.05 {
		ssimAssess = "🔴 Significant gap — overfitting"
	} else if math.Abs(ssimDelta) > 0.02 {
		ssimAssess = "🟡 Moderate gap"
	}
	fmt.Fprintf(f, "| PSNR | %+.2f dB | %s |\n", psnrDelta, psnrAssess)
	fmt.Fprintf(f, "| SSIM | %+.4f | %s |\n\n", ssimDelta, ssimAssess)

	// Detailed Training Results
	fmt.Fprintf(f, "## Training Dataset — Detailed Results\n\n")
	fmt.Fprintf(f, "| # | Image | Resolution | Orig Size | CROM Size | Ratio | MSE | PSNR (dB) | SSIM | Time |\n")
	fmt.Fprintf(f, "|---|-------|-----------|-----------|-----------|-------|-----|-----------|------|------|\n")
	for i, r := range train {
		emoji := psnrEmoji(r.PSNR)
		fmt.Fprintf(f, "| %d | %s | %dx%d | %s | %s | %.1f:1 | %.1f | %s %.2f | %.4f | %dms |\n",
			i+1, r.Name, r.Width, r.Height, humanSize(r.OrigSize), humanSize(int64(r.CromSize)),
			r.Ratio, r.MSE, emoji, r.PSNR, r.SSIM, r.CompressMs)
	}

	// Detailed Testing Results
	fmt.Fprintf(f, "\n## Testing Dataset — Detailed Results\n\n")
	fmt.Fprintf(f, "| # | Image | Resolution | Orig Size | CROM Size | Ratio | MSE | PSNR (dB) | SSIM | Time |\n")
	fmt.Fprintf(f, "|---|-------|-----------|-----------|-----------|-------|-----|-----------|------|------|\n")
	for i, r := range test {
		emoji := psnrEmoji(r.PSNR)
		fmt.Fprintf(f, "| %d | %s | %dx%d | %s | %s | %.1f:1 | %.1f | %s %.2f | %.4f | %dms |\n",
			i+1, r.Name, r.Width, r.Height, humanSize(r.OrigSize), humanSize(int64(r.CromSize)),
			r.Ratio, r.MSE, emoji, r.PSNR, r.SSIM, r.CompressMs)
	}

	// Distribution analysis
	fmt.Fprintf(f, "\n## PSNR Distribution\n\n")
	fmt.Fprintf(f, "| Range | Training | Testing | Total |\n")
	fmt.Fprintf(f, "|-------|----------|---------|-------|\n")
	ranges := [][2]float64{{0, 20}, {20, 25}, {25, 30}, {30, 100}}
	labels := []string{"< 20 dB 🔴", "20-25 dB 🟠", "25-30 dB 🟡", "> 30 dB ✅"}
	for i, r := range ranges {
		tc := countInRange(train, r[0], r[1])
		ec := countInRange(test, r[0], r[1])
		fmt.Fprintf(f, "| %s | %d (%.0f%%) | %d (%.0f%%) | %d |\n",
			labels[i], tc, pct(tc, len(train)), ec, pct(ec, len(test)), tc+ec)
	}

	// Worst performers
	fmt.Fprintf(f, "\n## Worst Performers (Bottom 5 by PSNR)\n\n")
	sorted := make([]ImageResult, len(results))
	copy(sorted, results)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PSNR < sorted[j].PSNR })
	fmt.Fprintf(f, "| Image | Dataset | PSNR | SSIM | MSE |\n")
	fmt.Fprintf(f, "|-------|---------|------|------|-----|\n")
	for i := 0; i < len(sorted) && i < 5; i++ {
		r := sorted[i]
		fmt.Fprintf(f, "| %s | %s | %.2f | %.4f | %.1f |\n", r.Name, r.Dataset, r.PSNR, r.SSIM, r.MSE)
	}

	// Best performers
	fmt.Fprintf(f, "\n## Best Performers (Top 5 by PSNR)\n\n")
	fmt.Fprintf(f, "| Image | Dataset | PSNR | SSIM | MSE |\n")
	fmt.Fprintf(f, "|-------|---------|------|------|-----|\n")
	for i := len(sorted) - 1; i >= 0 && i >= len(sorted)-5; i-- {
		r := sorted[i]
		fmt.Fprintf(f, "| %s | %s | %.2f | %.4f | %.1f |\n", r.Name, r.Dataset, r.PSNR, r.SSIM, r.MSE)
	}

	// Integrity checks
	fmt.Fprintf(f, "\n## Integrity Checks\n\n")
	var integrityIssues []string
	for _, r := range results {
		if r.CromSize > int(r.OrigSize) {
			integrityIssues = append(integrityIssues, fmt.Sprintf("⚠️ %s: CROM (%s) > original (%s)", r.Name, humanSize(int64(r.CromSize)), humanSize(r.OrigSize)))
		}
		if r.PSNR < 15 {
			integrityIssues = append(integrityIssues, fmt.Sprintf("🔴 %s: PSNR=%.2f — severe quality loss", r.Name, r.PSNR))
		}
	}
	if len(integrityIssues) == 0 {
		fmt.Fprintf(f, "✅ All integrity checks passed.\n")
	} else {
		for _, issue := range integrityIssues {
			fmt.Fprintf(f, "- %s\n", issue)
		}
	}

	fmt.Fprintf(f, "\n---\n*Report generated by CROM SQL Quality Analysis Engine*\n")
}

type AvgResult struct {
	MSE        float64
	PSNR       float64
	SSIM       float64
	Ratio      float64
	CompressMs int64
}

func computeAvg(results []ImageResult) AvgResult {
	if len(results) == 0 {
		return AvgResult{}
	}
	var a AvgResult
	for _, r := range results {
		a.MSE += r.MSE
		a.PSNR += r.PSNR
		a.SSIM += r.SSIM
		a.Ratio += r.Ratio
		a.CompressMs += r.CompressMs
	}
	n := float64(len(results))
	a.MSE /= n
	a.PSNR /= n
	a.SSIM /= n
	a.Ratio /= n
	a.CompressMs = a.CompressMs / int64(len(results))
	return a
}

func humanSize(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%dB", b)
	}
	kb := float64(b) / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.1fKB", kb)
	}
	return fmt.Sprintf("%.1fMB", kb/1024)
}

func psnrEmoji(psnr float64) string {
	if psnr >= 30 {
		return "✅"
	} else if psnr >= 25 {
		return "🟡"
	} else if psnr >= 20 {
		return "🟠"
	}
	return "🔴"
}

func countInRange(results []ImageResult, lo, hi float64) int {
	c := 0
	for _, r := range results {
		if r.PSNR >= lo && r.PSNR < hi {
			c++
		}
	}
	return c
}

func pct(count, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(count) / float64(total) * 100
}
