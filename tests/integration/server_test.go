package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/MrJc01/crompressor/pkg/codebook"
	"crompressor-sql-image/pkg/database"
	"crompressor-sql-image/pkg/server"
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

func TestServerAPI(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crom-integration-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. Init Mock DB
	dbPath := filepath.Join(tmpDir, "test_server.db")
	err = database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to initialize db: %v", err)
	}
	defer database.DB.Close()

	// 2. Create and open dummy codebook
	blockSize := 8
	cwSize := blockSize * blockSize * 3
	cwCount := 32
	cbPath := filepath.Join(tmpDir, "test_server.cromdb")
	createDummyCodebook(t, cbPath, cwSize, cwCount)

	cb, err := codebook.Open(cbPath)
	if err != nil {
		t.Fatalf("failed to open codebook: %v", err)
	}
	defer cb.Close()

	// 3. Create server
	srv := server.NewServer(cb, cbPath, blockSize)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// ---- TEST 1: GET /api/codebook ----
	t.Run("GET /api/codebook", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/codebook")
		if err != nil {
			t.Fatalf("GET /api/codebook request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		expectedFileSize, _ := os.Stat(cbPath)
		if int64(len(body)) != expectedFileSize.Size() {
			t.Errorf("expected size %d, got %d", expectedFileSize.Size(), len(body))
		}
	})

	// ---- TEST 2: POST /api/images (Upload) ----
	var uploadedImageID string
	t.Run("POST /api/images (Upload)", func(t *testing.T) {
		// Generate an in-memory test image (16x16 pixels)
		img := image.NewRGBA(image.Rect(0, 0, 16, 16))
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 200, G: 50, B: 50, A: 255})
			}
		}

		var jpegBuf bytes.Buffer
		if err := jpeg.Encode(&jpegBuf, img, nil); err != nil {
			t.Fatal(err)
		}

		// Prepare multipart request
		bodyBuf := &bytes.Buffer{}
		writer := multipart.NewWriter(bodyBuf)
		part, err := writer.CreateFormFile("image", "integration_test.jpg")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(part, &jpegBuf); err != nil {
			t.Fatal(err)
		}
		writer.Close()

		req, err := http.NewRequest("POST", ts.URL+"/api/images", bodyBuf)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", writer.FormDataContentType())

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /api/images request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			bodyBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 201, got %d. Body: %s", resp.StatusCode, string(bodyBytes))
		}

		var meta server.ImageMeta
		err = json.NewDecoder(resp.Body).Decode(&meta)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if meta.ID == "" {
			t.Error("expected returned image ID to be non-empty")
		}
		if meta.Name != "integration_test.jpg" {
			t.Errorf("expected name integration_test.jpg, got %s", meta.Name)
		}
		if meta.Width != 16 || meta.Height != 16 {
			t.Errorf("expected size 16x16, got %dx%d", meta.Width, meta.Height)
		}

		uploadedImageID = meta.ID
	})

	// ---- TEST 2b: POST /api/images (JSON pre-compressed upload) ----
	t.Run("POST /api/images (JSON pre-compressed)", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"name":           "json_upload.jpg",
			"width":          16,
			"height":         16,
			"crom_payload":   "AQIDBAUGBwg=", // Base64 encoding of 8 bytes
			"base64_payload": "iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAD0lEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
			"original_size":  768,
			"base64_size":    96,
			"jpeg_size":      72,
			"webp_size":      50,
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req, err := http.NewRequest("POST", ts.URL+"/api/images", bytes.NewReader(bodyBytes))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST pre-compressed request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			respBytes, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected status 201, got %d. Body: %s", resp.StatusCode, string(respBytes))
		}

		var meta server.ImageMeta
		err = json.NewDecoder(resp.Body).Decode(&meta)
		if err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		if meta.ID == "" {
			t.Error("expected returned image ID to be non-empty")
		}
		if meta.Name != "json_upload.jpg" {
			t.Errorf("expected name json_upload.jpg, got %s", meta.Name)
		}
		if meta.CROMSize != 8 {
			t.Errorf("expected CROM size 8, got %d", meta.CROMSize)
		}

		// Delete this temporary image to clean up
		reqDel, err := http.NewRequest("DELETE", ts.URL+"/api/images/"+meta.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		respDel, err := http.DefaultClient.Do(reqDel)
		if err == nil {
			respDel.Body.Close()
		}
	})

	if uploadedImageID == "" {
		t.Fatal("cannot proceed without valid image ID from POST upload")
	}

	// ---- TEST 3: GET /api/images (List) ----
	t.Run("GET /api/images (List)", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/images")
		if err != nil {
			t.Fatalf("GET /api/images failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		var images []server.ImageMeta
		err = json.NewDecoder(resp.Body).Decode(&images)
		if err != nil {
			t.Fatal(err)
		}

		found := false
		for _, img := range images {
			if img.ID == uploadedImageID {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("uploaded image ID %s not found in list output", uploadedImageID)
		}
	})

	// ---- TEST 4: GET /api/images/{id} (CROM payload) ----
	t.Run("GET /api/images/{id}", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/images/" + uploadedImageID)
		if err != nil {
			t.Fatalf("GET payload failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		payload, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}

		// Image is 16x16 with block size 8 => 2x2 = 4 blocks.
		// Each block requires 2 bytes index => 8 bytes total payload.
		expectedLen := 8
		if len(payload) != expectedLen {
			t.Errorf("expected payload size %d, got %d", expectedLen, len(payload))
		}
	})

	// ---- TEST 5: GET /api/images/{id}/original (Original JPEG) ----
	t.Run("GET /api/images/{id}/original", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/api/images/" + uploadedImageID + "/original")
		if err != nil {
			t.Fatalf("GET original failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", resp.StatusCode)
		}

		if resp.Header.Get("Content-Type") != "image/jpeg" {
			t.Errorf("expected Content-Type image/jpeg, got %s", resp.Header.Get("Content-Type"))
		}

		_, _, err = image.Decode(resp.Body)
		if err != nil {
			t.Errorf("failed to decode returned original image bytes: %v", err)
		}
	})

	// ---- TEST 6: DELETE /api/images/{id} (Delete) ----
	t.Run("DELETE /api/images/{id}", func(t *testing.T) {
		req, err := http.NewRequest("DELETE", ts.URL+"/api/images/"+uploadedImageID, nil)
		if err != nil {
			t.Fatal(err)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("DELETE failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			t.Errorf("expected status 204, got %d", resp.StatusCode)
		}

		// Verify deletion
		respGet, err := http.Get(ts.URL + "/api/images/" + uploadedImageID)
		if err != nil {
			t.Fatal(err)
		}
		defer respGet.Body.Close()
		if respGet.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404 after deletion, got %d", respGet.StatusCode)
		}
	})
}
