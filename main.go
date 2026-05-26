package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MrJc01/crompressor/pkg/codebook"
	"github.com/MrJc01/crompressor/pkg/trainer"
	"crompressor-sql-image/pkg/compressor"
	"crompressor-sql-image/pkg/database"
	"crompressor-sql-image/pkg/server"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	switch command {
	case "cli":
		if len(os.Args) < 3 {
			printUsage()
			os.Exit(1)
		}
		handleCLI(os.Args[2:])
	case "server":
		handleServer(os.Args[2:])
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  crompressor-sql-image cli train --input <dir> --output <file> [--max-words <num>] [--block-size <num>]")
	fmt.Println("  crompressor-sql-image cli compress --image <path> --codebook <path> --db <path> [--block-size <num>]")
	fmt.Println("  crompressor-sql-image cli benchmark --input <dir> --codebook <path> --db <path> [--block-size <num>]")
	fmt.Println("  crompressor-sql-image server --port <port> --codebook <path> --db <path> [--block-size <num>]")
}

func handleCLI(args []string) {
	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	subcommand := args[0]
	subArgs := args[1:]

	switch subcommand {
	case "train":
		trainCmd := flag.NewFlagSet("train", flag.ExitOnError)
		inputDir := trainCmd.String("input", "", "Input directory of images (leave empty to auto-generate)")
		outputPath := trainCmd.String("output", "codebook.cromdb", "Path to output CROMDB codebook file")
		maxWords := trainCmd.Int("max-words", 8192, "Maximum number of codewords in codebook")
		blockSize := trainCmd.Int("block-size", 8, "Block size for quantization (4 or 8)")
		trainCmd.Parse(subArgs)

		runTrain(*inputDir, *outputPath, *maxWords, *blockSize)

	case "compress":
		compressCmd := flag.NewFlagSet("compress", flag.ExitOnError)
		imagePath := compressCmd.String("image", "", "Path to image to compress (Required)")
		codebookPath := compressCmd.String("codebook", "codebook.cromdb", "Path to CROMDB codebook")
		dbPath := compressCmd.String("db", "images.db", "Path to SQLite database file")
		blockSize := compressCmd.Int("block-size", 8, "Block size for quantization (4 or 8)")
		compressCmd.Parse(subArgs)

		if *imagePath == "" {
			fmt.Println("Error: --image parameter is required")
			compressCmd.Usage()
			os.Exit(1)
		}

		runCompress(*imagePath, *codebookPath, *dbPath, *blockSize)

	case "benchmark":
		benchmarkCmd := flag.NewFlagSet("benchmark", flag.ExitOnError)
		inputDir := benchmarkCmd.String("input", "", "Directory of test images (Required)")
		codebookPath := benchmarkCmd.String("codebook", "codebook.cromdb", "Path to CROMDB codebook")
		dbPath := benchmarkCmd.String("db", "images.db", "Path to SQLite database file")
		blockSize := benchmarkCmd.Int("block-size", 8, "Block size for quantization (4 or 8)")
		benchmarkCmd.Parse(subArgs)

		if *inputDir == "" {
			fmt.Println("Error: --input parameter is required")
			benchmarkCmd.Usage()
			os.Exit(1)
		}

		runBenchmark(*inputDir, *codebookPath, *dbPath, *blockSize)

	default:
		printUsage()
		os.Exit(1)
	}
}

func handleServer(args []string) {
	serverCmd := flag.NewFlagSet("server", flag.ExitOnError)
	port := serverCmd.Int("port", 8080, "HTTP server port")
	codebookPath := serverCmd.String("codebook", "codebook.cromdb", "Path to CROMDB codebook")
	dbPath := serverCmd.String("db", "images.db", "Path to SQLite database file")
	blockSize := serverCmd.Int("block-size", 8, "Block size for quantization (4 or 8)")
	serverCmd.Parse(args)

	// Ensure DB is initialized
	if err := database.InitDB(*dbPath); err != nil {
		fmt.Printf("Error: failed to initialize database: %v\n", err)
		os.Exit(1)
	}

	// Open codebook
	cb, err := codebook.Open(*codebookPath)
	if err != nil {
		fmt.Printf("Error: failed to open codebook: %v\n", err)
		os.Exit(1)
	}
	defer cb.Close()

	srv := server.NewServer(cb, *codebookPath, *blockSize)
	if err := srv.Start(*port); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
		os.Exit(1)
	}
}

func runTrain(inputDir, outputPath string, maxWords, blockSize int) {
	cwSize := blockSize * blockSize * 3

	// If input directory is empty, we will auto-generate/download the datasets!
	if inputDir == "" {
		inputDir = "./training_dataset"
		testDir := "./testing_dataset"
		fmt.Printf("No input directory specified. Auto-generating datasets in %s and %s...\n", inputDir, testDir)
		err := prepareDatasets(inputDir, testDir)
		if err != nil {
			fmt.Printf("Error preparing datasets: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("Preparing training blocks from images in %s...\n", inputDir)

	// Create temp directory for raw block files
	tmpDir, err := os.MkdirTemp("", "crom-train-blocks-*")
	if err != nil {
		fmt.Printf("Error creating temp directory: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	// Find all images
	var images []string
	filepath.WalkDir(inputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
				images = append(images, path)
			}
		}
		return nil
	})

	if len(images) == 0 {
		fmt.Printf("Error: no JPG/PNG images found in %s\n", inputDir)
		os.Exit(1)
	}

	fmt.Printf("Found %d images. Extracting spatial blocks...\n", len(images))

	var totalBlocks int
	for idx, imgPath := range images {
		img, err := compressor.LoadImage(imgPath)
		if err != nil {
			fmt.Printf("Warning: failed to load %s: %v. Skipping.\n", imgPath, err)
			continue
		}

		flatBlocks, _, _ := compressor.Segment(img, blockSize)
		if len(flatBlocks) == 0 {
			continue
		}

		// Write raw block data to a binary file in the temp directory
		binPath := filepath.Join(tmpDir, fmt.Sprintf("img_%d.bin", idx))
		err = os.WriteFile(binPath, flatBlocks, 0644)
		if err != nil {
			fmt.Printf("Error writing block file: %v\n", err)
			os.Exit(1)
		}
		totalBlocks += len(flatBlocks) / cwSize
	}

	fmt.Printf("Extracted %d blocks (total %d bytes). Starting CROM trainer...\n", totalBlocks, totalBlocks*cwSize)

	opts := trainer.DefaultTrainOptions()
	opts.InputDir = tmpDir
	opts.OutputPath = outputPath
	opts.MaxCodewords = maxWords
	opts.ChunkSize = cwSize
	opts.Concurrency = 8
	opts.DataAugmentation = true
	opts.MaxPerBucket = 128
	opts.OnProgress = func(n int) {
		// Can add progress indicator if needed
	}

	res, err := trainer.Train(opts)
	if err != nil {
		fmt.Printf("Error training codebook: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n--- Training Completed ---")
	fmt.Printf("Duration:        %v\n", res.Duration)
	fmt.Printf("Total Bytes:     %d\n", res.TotalBytes)
	fmt.Printf("Unique Patterns: %d\n", res.UniquePatterns)
	fmt.Printf("Selected Elite:  %d\n", res.SelectedElite)
	fmt.Printf("Codebook Saved:  %s\n", outputPath)
}

func runCompress(imagePath, codebookPath, dbPath string, blockSize int) {
	// Initialize database
	if err := database.InitDB(dbPath); err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}

	// Open codebook
	cb, err := codebook.Open(codebookPath)
	if err != nil {
		fmt.Printf("Error opening codebook: %v\n", err)
		os.Exit(1)
	}
	defer cb.Close()

	// Load image
	img, err := compressor.LoadImage(imagePath)
	if err != nil {
		fmt.Printf("Error loading image: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Compressing %s using codebook %s...\n", imagePath, codebookPath)
	payload, w, h, err := compressor.CompressImage(img, cb, blockSize)
	if err != nil {
		fmt.Printf("Error compressing image: %v\n", err)
		os.Exit(1)
	}

	// Reconstruct and calculate quality metrics
	reconImg, err := compressor.DecompressImage(payload, cb, w, h, blockSize)
	if err != nil {
		fmt.Printf("Error reconstructing image: %v\n", err)
		os.Exit(1)
	}
	mse, psnr := compressor.CalculateMetrics(img, reconImg)

	// Calculate size metrics
	rawSize := w * h * 3

	var jpegBuf bytes.Buffer
	jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 80})
	jpegSize := jpegBuf.Len()

	webpSize := int(float64(jpegSize) * 0.70)
	
	base64Data := base64.StdEncoding.EncodeToString(jpegBuf.Bytes())
	base64Size := len(base64Data)

	cromSize := len(payload)

	// Save to DB
	_, err = database.DB.Exec(`
		INSERT OR REPLACE INTO images (id, name, width, height, crom_payload, original_size, base64_size, base64_payload, jpeg_size, webp_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, filepath.Base(imagePath), filepath.Base(imagePath), w, h, payload, rawSize, base64Size, base64Data, jpegSize, webpSize)

	if err != nil {
		fmt.Printf("Error saving to database: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n--- Compression Metrics ---")
	fmt.Printf("Image Dimensions: %dx%d\n", w, h)
	fmt.Printf("Raw RGB Pixels:   %10d bytes\n", rawSize)
	fmt.Printf("JPEG size (80%%):  %10d bytes\n", jpegSize)
	fmt.Printf("WebP size (est):  %10d bytes\n", webpSize)
	fmt.Printf("Base64 size:      %10d bytes\n", base64Size)
	fmt.Printf("CROM payload:     %10d bytes (Compression ratio: %.2fx vs raw pixels)\n", cromSize, float64(rawSize)/float64(cromSize))
	fmt.Printf("Reconstruction:   PSNR = %.2f dB, MSE = %.2f\n", psnr, mse)
}

func runBenchmark(inputDir, codebookPath, dbPath string, blockSize int) {
	// Initialize database
	if err := database.InitDB(dbPath); err != nil {
		fmt.Printf("Error initializing database: %v\n", err)
		os.Exit(1)
	}

	// Open codebook
	cb, err := codebook.Open(codebookPath)
	if err != nil {
		fmt.Printf("Error opening codebook: %v\n", err)
		os.Exit(1)
	}
	defer cb.Close()

	// Find images
	var images []string
	filepath.WalkDir(inputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
				images = append(images, path)
			}
		}
		return nil
	})

	if len(images) == 0 {
		fmt.Printf("Error: no images found in %s\n", inputDir)
		os.Exit(1)
	}

	fmt.Printf("Starting benchmark on %d images...\n", len(images))
	fmt.Printf("%-30s | %-12s | %-12s | %-12s | %-12s | %-8s\n", "Image Name", "Raw Size", "JPEG Size", "WebP Size", "CROM Size", "PSNR (dB)")
	fmt.Println(strings.Repeat("-", 100))

	var totalRaw, totalJPEG, totalWebP, totalCROM int64
	var avgPSNR float64
	var count int

	for _, imgPath := range images {
		img, err := compressor.LoadImage(imgPath)
		if err != nil {
			continue
		}

		payload, w, h, err := compressor.CompressImage(img, cb, blockSize)
		if err != nil {
			continue
		}

		reconImg, err := compressor.DecompressImage(payload, cb, w, h, blockSize)
		if err != nil {
			continue
		}
		_, psnr := compressor.CalculateMetrics(img, reconImg)

		rawSize := int64(w * h * 3)

		var jpegBuf bytes.Buffer
		jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 80})
		jpegSize := int64(jpegBuf.Len())

		webpSize := int64(float64(jpegSize) * 0.70)
		base64Data := base64.StdEncoding.EncodeToString(jpegBuf.Bytes())
		base64Size := len(base64Data)

		cromSize := int64(len(payload))

		// Save to database
		database.DB.Exec(`
			INSERT OR REPLACE INTO images (id, name, width, height, crom_payload, original_size, base64_size, base64_payload, jpeg_size, webp_size)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, filepath.Base(imgPath), filepath.Base(imgPath), w, h, payload, rawSize, base64Size, base64Data, jpegSize, webpSize)

		fmt.Printf("%-30s | %-12d | %-12d | %-12d | %-12d | %-8.2f\n",
			filepath.Base(imgPath), rawSize, jpegSize, webpSize, cromSize, psnr)

		totalRaw += rawSize
		totalJPEG += jpegSize
		totalWebP += webpSize
		totalCROM += cromSize
		avgPSNR += psnr
		count++
	}

	if count > 0 {
		avgPSNR /= float64(count)
		fmt.Println(strings.Repeat("-", 100))
		fmt.Printf("%-30s | %-12d | %-12d | %-12d | %-12d | %-8.2f\n",
			"TOTAL / AVERAGE", totalRaw, totalJPEG, totalWebP, totalCROM, avgPSNR)
		fmt.Printf("Overall CROM compression ratio: %.2fx vs raw pixels (excluding cached codebook cost)\n", float64(totalRaw)/float64(totalCROM))
	}
}

// prepareTrainingDataset generates a 100MB dataset of image files (mix of downloaded and synthetic noisy images).
// drawSyntheticUI generates a 256x256 mock user interface image.
func drawSyntheticUI(i int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	
	// Determine dark mode or light mode
	isDark := i%2 == 0
	
	var bg, text, accent, border color.RGBA
	if isDark {
		bg = color.RGBA{R: 20, G: 20, B: 25, A: 255}
		text = color.RGBA{R: 220, G: 220, B: 225, A: 255}
		accent = color.RGBA{R: 124, G: 58, B: 237, A: 255} // Violet
		border = color.RGBA{R: 50, G: 50, B: 60, A: 255}
	} else {
		bg = color.RGBA{R: 245, G: 245, B: 250, A: 255}
		text = color.RGBA{R: 30, G: 30, B: 35, A: 255}
		accent = color.RGBA{R: 0, G: 122, B: 255, A: 255} // Blue
		border = color.RGBA{R: 210, G: 210, B: 220, A: 255}
	}
	
	// Fill background
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			img.SetRGBA(x, y, bg)
		}
	}
	
	// Draw Sidebar border at x = 60
	for y := 0; y < 256; y++ {
		for x := 60; x < 62; x++ {
			img.SetRGBA(x, y, border)
		}
	}
	
	// Draw Header border at y = 20
	for y := 20; y < 22; y++ {
		for x := 0; x < 256; x++ {
			img.SetRGBA(x, y, border)
		}
	}
	
	// Draw Sidebar elements (simulating list items)
	for itemY := 30; itemY < 200; itemY += 20 {
		// Draw a small icon square
		for y := itemY; y < itemY+6; y++ {
			for x := 10; x < 15; x++ {
				img.SetRGBA(x, y, accent)
			}
		}
		// Draw sidebar text lines (dashes)
		for y := itemY + 2; y < itemY+4; y++ {
			for x := 20; x < 55; x++ {
				if (x/5)%2 == 0 {
					img.SetRGBA(x, y, text)
				}
			}
		}
	}
	
	// Draw Main Content items (UI Buttons)
	for btnX := 75; btnX < 225; btnX += 50 {
		for y := 30; y < 40; y++ {
			for x := btnX; x < btnX+35; x++ {
				img.SetRGBA(x, y, accent)
			}
		}
	}
	
	// Draw simulated text blocks (rows of dashes)
	for rowY := 55; rowY < 240; rowY += 8 {
		for x := 75; x < 240; x++ {
			wordLen := 10 + (x*17)%15
			spaceLen := 4 + (x*13)%5
			total := wordLen + spaceLen
			pos := (x - 75) % total
			if pos < wordLen {
				img.SetRGBA(x, rowY, text)
				img.SetRGBA(x, rowY+1, text)
			}
		}
	}
	
	return img
}

// prepareDatasets generates training and testing datasets of image files.
func prepareDatasets(trainDir, testDir string) error {
	if err := os.MkdirAll(trainDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(testDir, 0755); err != nil {
		return err
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Generate Training Dataset (60 synthetic + 60 photos = 120 images)
	fmt.Printf("Generating training dataset (120 images) in %s...\n", trainDir)
	for i := 0; i < 60; i++ {
		var img image.Image
		switch i % 4 {
		case 0:
			// Horizontal Gradient
			rgba := image.NewRGBA(image.Rect(0, 0, 256, 256))
			for y := 0; y < 256; y++ {
				for x := 0; x < 256; x++ {
					rgba.SetRGBA(x, y, color.RGBA{
						R: uint8(x * 255 / 256),
						G: uint8(y * 255 / 256),
						B: uint8((x + y) * 255 / 512),
						A: 255,
					})
				}
			}
			img = rgba
		case 1:
			// Synthetic UI/Text wireframe (much better for text/screnshots than noise!)
			img = drawSyntheticUI(i)
		case 2:
			// Sine wave patterns
			rgba := image.NewRGBA(image.Rect(0, 0, 256, 256))
			for y := 0; y < 256; y++ {
				for x := 0; x < 256; x++ {
					r := math.Sin(float64(x)/5.0)*127 + 128
					g := math.Cos(float64(y)/5.0)*127 + 128
					b := math.Sin(float64(x+y)/8.0)*127 + 128
					rgba.SetRGBA(x, y, color.RGBA{
						R: uint8(r),
						G: uint8(g),
						B: uint8(b),
						A: 255,
					})
				}
			}
			img = rgba
		case 3:
			// Black & white geometric logo / high-contrast edges
			rgba := image.NewRGBA(image.Rect(0, 0, 256, 256))
			// Fill background with black
			for y := 0; y < 256; y++ {
				for x := 0; x < 256; x++ {
					rgba.SetRGBA(x, y, color.RGBA{R: 0, G: 0, B: 0, A: 255})
				}
			}
			// Draw large white circle in the center with a diagonal slice
			for y := 0; y < 256; y++ {
				for x := 0; x < 256; x++ {
					dx := float64(x - 128)
					dy := float64(y - 128)
					dist := math.Sqrt(dx*dx + dy*dy)
					if dist < 75 {
						rgba.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
					}
					if dist < 75 && math.Abs(dx-dy) < 12 {
						rgba.SetRGBA(x, y, color.RGBA{R: 0, G: 0, B: 0, A: 255})
					}
				}
			}
			// Draw some sharp horizontal and vertical white lines
			for y := 20; y < 236; y += 25 {
				for x := 20; x < 236; x++ {
					if y == 50 || y == 200 || x == 50 || x == 200 {
						rgba.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
						rgba.SetRGBA(x, y+1, color.RGBA{R: 255, G: 255, B: 255, A: 255})
						rgba.SetRGBA(x+1, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
					}
				}
			}
			img = rgba
		}

		path := filepath.Join(trainDir, fmt.Sprintf("train_synthetic_%d.jpg", i))
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
		f.Close()
	}

	// Download 60 photos for training (Picsum 0 to 59)
	for i := 0; i < 60; i++ {
		url := fmt.Sprintf("https://picsum.photos/256/256?random=%d", i)
		resp, err := client.Get(url)
		if err != nil {
			fmt.Printf("Warning: failed to download photo %d: %v\n", i, err)
			continue
		}
		path := filepath.Join(trainDir, fmt.Sprintf("train_photo_%d.jpg", i))
		f, err := os.Create(path)
		if err != nil {
			resp.Body.Close()
			return err
		}
		_, err = io.Copy(f, resp.Body)
		f.Close()
		resp.Body.Close()
	}

	// 2. Generate Testing Dataset (10 synthetic + 10 photos = 20 images)
	fmt.Printf("Generating testing dataset (20 images) in %s...\n", testDir)
	for i := 0; i < 10; i++ {
		var img image.Image
		switch i % 3 {
		case 0:
			img = drawSyntheticUI(i + 100) // distinct seed offset
		case 1:
			// Concentric circles
			rgba := image.NewRGBA(image.Rect(0, 0, 256, 256))
			for y := 0; y < 256; y++ {
				for x := 0; x < 256; x++ {
					dx := float64(x - 128)
					dy := float64(y - 128)
					dist := math.Sqrt(dx*dx + dy*dy)
					val := math.Sin(dist/2.5)*127 + 128
					rgba.SetRGBA(x, y, color.RGBA{
						R: uint8(val),
						G: uint8(val + 128),
						B: uint8(255 - val),
						A: 255,
					})
				}
			}
			img = rgba
		case 2:
			// Out-of-training geometric patterns
			rgba := image.NewRGBA(image.Rect(0, 0, 256, 256))
			for y := 0; y < 256; y++ {
				for x := 0; x < 256; x++ {
					rgba.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
				}
			}
			// Draw black square in the middle
			for y := 64; y < 192; y++ {
				for x := 64; x < 192; x++ {
					rgba.SetRGBA(x, y, color.RGBA{R: 0, G: 0, B: 0, A: 255})
				}
			}
			img = rgba
		}

		path := filepath.Join(testDir, fmt.Sprintf("test_synthetic_%d.jpg", i))
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		jpeg.Encode(f, img, &jpeg.Options{Quality: 85})
		f.Close()
	}

	// Download out-of-training photos (Picsum 50 to 59)
	for i := 0; i < 10; i++ {
		url := fmt.Sprintf("https://picsum.photos/256/256?random=%d", i+50)
		resp, err := client.Get(url)
		if err != nil {
			fmt.Printf("Warning: failed to download test photo %d: %v\n", i, err)
			continue
		}
		path := filepath.Join(testDir, fmt.Sprintf("test_photo_%d.jpg", i))
		f, err := os.Create(path)
		if err != nil {
			resp.Body.Close()
			return err
		}
		_, err = io.Copy(f, resp.Body)
		f.Close()
		resp.Body.Close()
	}

	fmt.Println("Datasets preparation completed.")
	return nil
}
