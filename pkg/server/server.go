package server

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/MrJc01/crompressor/pkg/codebook"
	"crompressor-sql-image/pkg/compressor"
	"crompressor-sql-image/pkg/database"
)

// Server encapsulates the dependencies for the web server.
type Server struct {
	cb         *codebook.Reader
	cbPath     string
	blockSize  int
}

// NewServer creates a new server instance.
func NewServer(cb *codebook.Reader, cbPath string, blockSize int) *Server {
	return &Server{
		cb:        cb,
		cbPath:    cbPath,
		blockSize: blockSize,
	}
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func gzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()

		gzw := gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next.ServeHTTP(gzw, r)
	})
}

// Handler returns the HTTP handler containing all registered routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// API Routes
	mux.HandleFunc("GET /api/codebook", s.handleGetCodebook)
	mux.HandleFunc("GET /api/images", s.handleGetImages)
	mux.HandleFunc("GET /api/images/{id}", s.handleGetImagePayload)
	mux.HandleFunc("GET /api/images/{id}/original", s.handleGetOriginalImage)
	mux.HandleFunc("POST /api/images", s.handleUploadImage)
	mux.HandleFunc("DELETE /api/images/{id}", s.handleDeleteImage)

	// Static files serving
	// We'll serve from "./static" folder
	staticHandler := http.FileServer(http.Dir("./static"))
	mux.Handle("/", staticHandler)

	return gzipMiddleware(mux)
}

// Start launches the HTTP server.
func (s *Server) Start(port int) error {
	handler := s.Handler()
	addr := fmt.Sprintf(":%d", port)
	fmt.Printf("Starting server on http://localhost%s\n", addr)
	return http.ListenAndServe(addr, handler)
}

func (s *Server) handleGetCodebook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(s.cbPath)))
	http.ServeFile(w, r, s.cbPath)
}

type ImageMeta struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	OriginalSize int     `json:"original_size"`
	Base64Size   int     `json:"base64_size"`
	JPEGSize     int     `json:"jpeg_size"`
	WebPSize     int     `json:"webp_size"`
	CROMSize     int     `json:"crom_size"`
	CreatedAt    string  `json:"created_at"`
}

func (s *Server) handleGetImages(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, name, width, height, LENGTH(crom_payload) as crom_size, original_size, base64_size, jpeg_size, webp_size, created_at 
		FROM images 
		ORDER BY created_at DESC
	`)
	if err != nil {
		http.Error(w, fmt.Sprintf("database query failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var images []ImageMeta
	for rows.Next() {
		var img ImageMeta
		err := rows.Scan(&img.ID, &img.Name, &img.Width, &img.Height, &img.CROMSize, &img.OriginalSize, &img.Base64Size, &img.JPEGSize, &img.WebPSize, &img.CreatedAt)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to scan row: %v", err), http.StatusInternalServerError)
			return
		}
		images = append(images, img)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(images)
}

func (s *Server) handleGetImagePayload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing image id", http.StatusBadRequest)
		return
	}

	var payload []byte
	err := database.DB.QueryRow("SELECT crom_payload FROM images WHERE id = ?", id).Scan(&payload)
	if err != nil {
		http.Error(w, "image not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(payload)
}

func (s *Server) handleGetOriginalImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing image id", http.StatusBadRequest)
		return
	}

	var base64Payload string
	err := database.DB.QueryRow("SELECT base64_payload FROM images WHERE id = ?", id).Scan(&base64Payload)
	if err != nil {
		http.Error(w, "image not found", http.StatusNotFound)
		return
	}

	data, err := base64.StdEncoding.DecodeString(base64Payload)
	if err != nil {
		http.Error(w, "failed to decode original payload", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(data)
}

func (s *Server) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Name          string `json:"name"`
			Width         int    `json:"width"`
			Height        int    `json:"height"`
			CromPayload   string `json:"crom_payload"`   // base64
			Base64Payload string `json:"base64_payload"` // base64
			OriginalSize  int    `json:"original_size"`
			Base64Size    int    `json:"base64_size"`
			JPEGSize      int    `json:"jpeg_size"`
			WebPSize      int    `json:"webp_size"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("failed to decode JSON body: %v", err), http.StatusBadRequest)
			return
		}

		payload, err := base64.StdEncoding.DecodeString(req.CromPayload)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid crom_payload base64: %v", err), http.StatusBadRequest)
			return
		}

		id := uuid.New().String()
		_, err = database.DB.Exec(`
			INSERT INTO images (id, name, width, height, crom_payload, original_size, base64_size, base64_payload, jpeg_size, webp_size)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id, req.Name, req.Width, req.Height, payload, req.OriginalSize, req.Base64Size, req.Base64Payload, req.JPEGSize, req.WebPSize)

		if err != nil {
			http.Error(w, fmt.Sprintf("failed to save to database: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(ImageMeta{
			ID:           id,
			Name:         req.Name,
			Width:        req.Width,
			Height:       req.Height,
			OriginalSize: req.OriginalSize,
			Base64Size:   req.Base64Size,
			JPEGSize:     req.JPEGSize,
			WebPSize:     req.WebPSize,
			CROMSize:     len(payload),
		})
		return
	}

	// Limit upload size to 10MB
	r.ParseMultipartForm(10 << 20)

	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to parse uploaded file: %v", err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read file into buffer
	var imgBuf bytes.Buffer
	if _, err := io.Copy(&imgBuf, file); err != nil {
		http.Error(w, "failed to read file buffer", http.StatusInternalServerError)
		return
	}

	originalBytes := imgBuf.Bytes()

	// Decode image
	img, _, err := image.Decode(bytes.NewReader(originalBytes))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid image format: %v", err), http.StatusBadRequest)
		return
	}

	// Compress using CROM
	payload, adjW, adjH, err := compressor.CompressImage(img, s.cb, s.blockSize)
	if err != nil {
		http.Error(w, fmt.Sprintf("compression failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Compute comparison sizes
	// 1. Raw Size (uncompressed pixels)
	rawSize := adjW * adjH * 3

	// 2. JPEG size (at 80% quality)
	var jpegBuf bytes.Buffer
	if err := jpeg.Encode(&jpegBuf, img, &jpeg.Options{Quality: 80}); err != nil {
		http.Error(w, "JPEG compression failed", http.StatusInternalServerError)
		return
	}
	jpegSize := jpegBuf.Len()

	// 3. WebP size (estimated at 70% of JPEG size)
	webpSize := int(float64(jpegSize) * 0.70)

	// 4. Base64 Representation of the JPEG
	base64Payload := base64.StdEncoding.EncodeToString(jpegBuf.Bytes())
	base64Size := len(base64Payload)

	id := uuid.New().String()
	name := header.Filename

	// Save to DB
	_, err = database.DB.Exec(`
		INSERT INTO images (id, name, width, height, crom_payload, original_size, base64_size, base64_payload, jpeg_size, webp_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, name, adjW, adjH, payload, rawSize, base64Size, base64Payload, jpegSize, webpSize)

	if err != nil {
		http.Error(w, fmt.Sprintf("failed to save to database: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ImageMeta{
		ID:           id,
		Name:         name,
		Width:        adjW,
		Height:       adjH,
		OriginalSize: rawSize,
		Base64Size:   base64Size,
		JPEGSize:     jpegSize,
		WebPSize:     webpSize,
		CROMSize:     len(payload),
	})
}

func (s *Server) handleDeleteImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "missing image id", http.StatusBadRequest)
		return
	}

	_, err := database.DB.Exec("DELETE FROM images WHERE id = ?", id)
	if err != nil {
		http.Error(w, fmt.Sprintf("database delete failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
