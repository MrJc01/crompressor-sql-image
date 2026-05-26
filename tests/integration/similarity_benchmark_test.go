package integration

import (
	"fmt"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MrJc01/crompressor/pkg/codebook"
	"crompressor-sql-image/pkg/compressor"
)

func TestGenerateSimilarityReport(t *testing.T) {
	// Open the freshly trained codebook
	cbPath := "../../codebook_4.cromdb"
	cb, err := codebook.Open(cbPath)
	if err != nil {
		t.Fatalf("failed to open codebook: %v. Please run training first.", err)
	}
	defer cb.Close()

	blockSize := 4

	// Output report file path
	reportPath := "../results/similarity_comparison.md"
	err = os.MkdirAll(filepath.Dir(reportPath), 0755)
	if err != nil {
		t.Fatal(err)
	}

	reportFile, err := os.Create(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reportFile.Close()

	// Write Report Header
	fmt.Fprintln(reportFile, "# Relatório de Análise de Similaridade de Imagens")
	fmt.Fprintln(reportFile, "")
	fmt.Fprintln(reportFile, "Este relatório apresenta a similaridade visual (MSE e PSNR) e a taxa de compressão física obtidas pelo sistema **CROM** para imagens dentro do conjunto de treinamento (**In-Training**) e fora do conjunto (**Out-of-Training**).")
	fmt.Fprintln(reportFile, "")
	fmt.Fprintf(reportFile, "* **Data da Análise:** %s\n", time.Now().Format("2006-01-02 15:04:05 MST"))
	fmt.Fprintf(reportFile, "* **Codebook Utilizado:** `%s`\n", filepath.Base(cbPath))
	fmt.Fprintf(reportFile, "* **Tamanho de Bloco:** %dx%d pixels\n", blockSize, blockSize)
	fmt.Fprintln(reportFile, "")

	wd, _ := os.Getwd()
	t.Logf("Test execution working directory: %s", wd)

	// 1. Evaluate In-Training Dataset
	trainDir := "../../training_dataset"
	if _, err := os.Stat(trainDir); err != nil {
		t.Fatalf("training_dataset directory %s does not exist from working directory %s: %v", trainDir, wd, err)
	}

	fmt.Fprintln(reportFile, "## 🟢 Imagens de Treinamento (In-Training)")
	fmt.Fprintln(reportFile, "")
	fmt.Fprintln(reportFile, "| Nome da Imagem | Dimensões | Tamanho Original | Tamanho CROM | MSE | PSNR (dB) | Status |")
	fmt.Fprintln(reportFile, "| :--- | :---: | :---: | :---: | :---: | :---: | :---: |")

	var totalTrainPSNR float64
	var countTrain int

	err = filepath.WalkDir(trainDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
			img, err := compressor.LoadImage(path)
			if err != nil {
				t.Logf("LoadImage error for %s: %v", path, err)
				return nil
			}

			payload, w, h, err := compressor.CompressImage(img, cb, blockSize)
			if err != nil {
				t.Logf("CompressImage error for %s: %v", path, err)
				return nil
			}

			reconImg, err := compressor.DecompressImage(payload, cb, w, h, blockSize)
			if err != nil {
				t.Logf("DecompressImage error for %s: %v", path, err)
				return nil
			}

			mse, psnr := compressor.CalculateMetrics(img, reconImg)

			rawSize := w * h * 3
			cromSize := len(payload)

			status := "Excelente"
			if psnr < 15 {
				status = "Baixo"
			} else if psnr < 25 {
				status = "Aceitável"
			}

			fmt.Fprintf(reportFile, "| %s | %dx%d | %d KB | %d KB | %.2f | %.2f dB | %s |\n",
				d.Name(), w, h, rawSize/1024, cromSize/1024, mse, psnr, status)

			totalTrainPSNR += psnr
			countTrain++
		}
		return nil
	})

	if countTrain > 0 {
		avgTrainPSNR := totalTrainPSNR / float64(countTrain)
		fmt.Fprintf(reportFile, "| **MÉDIA** | | | | | **%.2f dB** | |\n", avgTrainPSNR)
	}
	fmt.Fprintln(reportFile, "")

	// 2. Evaluate Out-of-Training Dataset
	testDir := "../../testing_dataset"
	if _, err := os.Stat(testDir); err != nil {
		t.Fatalf("testing_dataset directory %s does not exist from working directory %s: %v", testDir, wd, err)
	}

	fmt.Fprintln(reportFile, "## 🔴 Imagens Fora de Treinamento (Out-of-Training)")
	fmt.Fprintln(reportFile, "")
	fmt.Fprintln(reportFile, "| Nome da Imagem | Dimensões | Tamanho Original | Tamanho CROM | MSE | PSNR (dB) | Status |")
	fmt.Fprintln(reportFile, "| :--- | :---: | :---: | :---: | :---: | :---: | :---: |")

	var totalTestPSNR float64
	var countTest int

	err = filepath.WalkDir(testDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".jpg" || ext == ".jpeg" || ext == ".png" {
			img, err := compressor.LoadImage(path)
			if err != nil {
				t.Logf("LoadImage error for %s: %v", path, err)
				return nil
			}

			payload, w, h, err := compressor.CompressImage(img, cb, blockSize)
			if err != nil {
				t.Logf("CompressImage error for %s: %v", path, err)
				return nil
			}

			reconImg, err := compressor.DecompressImage(payload, cb, w, h, blockSize)
			if err != nil {
				t.Logf("DecompressImage error for %s: %v", path, err)
				return nil
			}

			mse, psnr := compressor.CalculateMetrics(img, reconImg)

			rawSize := w * h * 3
			cromSize := len(payload)

			status := "Excelente"
			if psnr < 15 {
				status = "Baixo"
			} else if psnr < 25 {
				status = "Aceitável"
			}

			fmt.Fprintf(reportFile, "| %s | %dx%d | %d KB | %d KB | %.2f | %.2f dB | %s |\n",
				d.Name(), w, h, rawSize/1024, cromSize/1024, mse, psnr, status)

			totalTestPSNR += psnr
			countTest++
		}
		return nil
	})

	if countTest > 0 {
		avgTestPSNR := totalTestPSNR / float64(countTest)
		fmt.Fprintf(reportFile, "| **MÉDIA** | | | | | **%.2f dB** | |\n", avgTestPSNR)
	}

	t.Logf("Similarity report successfully generated at %s", reportPath)
}
