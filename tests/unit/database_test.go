package unit

import (
	"os"
	"path/filepath"
	"testing"

	"crompressor-sql-image/pkg/database"
)

func TestDatabaseOperations(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "crom-db-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test_images.db")

	// 1. Initialize DB
	err = database.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to initialize database: %v", err)
	}
	if database.DB == nil {
		t.Fatal("database connection is nil after initialization")
	}
	defer database.DB.Close()

	// Ensure database file is created
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatalf("database file was not created at %s", dbPath)
	}

	// 2. Insert test image record
	id := "test-uuid-12345"
	name := "test_image.png"
	width := 128
	height := 128
	cromPayload := []byte{0x01, 0x02, 0x03, 0x04}
	originalSize := 128 * 128 * 3
	base64Size := 5000
	base64Payload := "iVBORw0KGgoAAAANSUhEUgAA..."
	jpegSize := 4000
	webpSize := 2800

	_, err = database.DB.Exec(`
		INSERT INTO images (id, name, width, height, crom_payload, original_size, base64_size, base64_payload, jpeg_size, webp_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, name, width, height, cromPayload, originalSize, base64Size, base64Payload, jpegSize, webpSize)
	if err != nil {
		t.Fatalf("failed to insert image: %v", err)
	}

	// 3. Query the image record
	var queryName string
	var queryWidth, queryHeight int
	var queryCromPayload []byte
	err = database.DB.QueryRow(`
		SELECT name, width, height, crom_payload FROM images WHERE id = ?
	`, id).Scan(&queryName, &queryWidth, &queryHeight, &queryCromPayload)
	if err != nil {
		t.Fatalf("failed to query inserted image: %v", err)
	}

	if queryName != name {
		t.Errorf("expected name %s, got %s", name, queryName)
	}
	if queryWidth != width || queryHeight != height {
		t.Errorf("expected dimensions %dx%d, got %dx%d", width, height, queryWidth, queryHeight)
	}
	if len(queryCromPayload) != len(cromPayload) || queryCromPayload[0] != cromPayload[0] {
		t.Errorf("expected CROM payload %v, got %v", cromPayload, queryCromPayload)
	}

	// 4. Test Upsert (INSERT OR REPLACE) behaviour
	newWebpSize := 2900
	_, err = database.DB.Exec(`
		INSERT OR REPLACE INTO images (id, name, width, height, crom_payload, original_size, base64_size, base64_payload, jpeg_size, webp_size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, name, width, height, cromPayload, originalSize, base64Size, base64Payload, jpegSize, newWebpSize)
	if err != nil {
		t.Fatalf("failed to upsert image: %v", err)
	}

	var queryWebpSize int
	err = database.DB.QueryRow("SELECT webp_size FROM images WHERE id = ?", id).Scan(&queryWebpSize)
	if err != nil {
		t.Fatalf("failed to query webp_size: %v", err)
	}
	if queryWebpSize != newWebpSize {
		t.Errorf("expected updated webp_size %d, got %d", newWebpSize, queryWebpSize)
	}

	// 5. Delete image record
	_, err = database.DB.Exec("DELETE FROM images WHERE id = ?", id)
	if err != nil {
		t.Fatalf("failed to delete image: %v", err)
	}

	var dummy string
	err = database.DB.QueryRow("SELECT name FROM images WHERE id = ?", id).Scan(&dummy)
	if err == nil {
		t.Error("expected row to be deleted, but it was found")
	}
}
