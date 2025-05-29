package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/tiff"
)

// createTestJPEG creates a test JPEG image with given dimensions
func createTestJPEG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	
	// Fill with a simple pattern
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, image.NewRGBA(image.Rect(0, 0, 1, 1)).At(0, 0))
		}
	}
	
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100})
	return buf.Bytes()
}

// createTestPNG creates a test PNG image with given dimensions
func createTestPNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	
	// Fill with a simple pattern
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, image.NewRGBA(image.Rect(0, 0, 1, 1)).At(0, 0))
		}
	}
	
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// createTestTIFF creates a test TIFF image with given dimensions
func createTestTIFF(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	
	// Fill with a simple pattern
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, image.NewRGBA(image.Rect(0, 0, 1, 1)).At(0, 0))
		}
	}
	
	var buf bytes.Buffer
	tiff.Encode(&buf, img, nil)
	return buf.Bytes()
}

// getImageDimensions decodes an image and returns its dimensions
func getImageDimensions(data []byte, filename string) (int, int, error) {
	var img image.Image
	var err error
	
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png":
		img, err = png.Decode(bytes.NewReader(data))
	case ".tif", ".tiff":
		img, err = tiff.Decode(bytes.NewReader(data))
	default:
		img, err = jpeg.Decode(bytes.NewReader(data))
	}
	
	if err != nil {
		return 0, 0, err
	}
	
	bounds := img.Bounds()
	return bounds.Dx(), bounds.Dy(), nil
}

func TestResizeImage_JPEG_LargerThanMax(t *testing.T) {
	// Create a 1000x800 JPEG
	testData := createTestJPEG(1000, 800)
	
	// Resize to max 500px
	resized, err := ResizeImage(testData, 500, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to resize image: %v", err)
	}
	
	// Check dimensions - should be 500x400 (maintaining aspect ratio)
	width, height, err := getImageDimensions(resized, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to decode resized image: %v", err)
	}
	
	if width != 500 || height != 400 {
		t.Errorf("Expected 500x400, got %dx%d", width, height)
	}
}

func TestResizeImage_PNG_LargerThanMax(t *testing.T) {
	// Create a 600x1200 PNG (taller than wide)
	testData := createTestPNG(600, 1200)
	
	// Resize to max 300px
	resized, err := ResizeImage(testData, 300, "test.png")
	if err != nil {
		t.Fatalf("Failed to resize image: %v", err)
	}
	
	// Check dimensions - should be 150x300 (maintaining aspect ratio)
	width, height, err := getImageDimensions(resized, "test.jpg") // Output is always JPEG
	if err != nil {
		t.Fatalf("Failed to decode resized image: %v", err)
	}
	
	if width != 150 || height != 300 {
		t.Errorf("Expected 150x300, got %dx%d", width, height)
	}
}

func TestResizeImage_TIFF_LargerThanMax(t *testing.T) {
	// Create a 800x1000 TIFF (taller than wide)
	testData := createTestTIFF(800, 1000)
	
	// Resize to max 400px
	resized, err := ResizeImage(testData, 400, "test.tif")
	if err != nil {
		t.Fatalf("Failed to resize TIFF image: %v", err)
	}
	
	// Check dimensions - should be 320x400 (maintaining aspect ratio)
	width, height, err := getImageDimensions(resized, "test.jpg") // Output is always JPEG
	if err != nil {
		t.Fatalf("Failed to decode resized image: %v", err)
	}
	
	if width != 320 || height != 400 {
		t.Errorf("Expected 320x400, got %dx%d", width, height)
	}
}

func TestResizeImage_SmallerThanMax_ReturnsOriginal(t *testing.T) {
	// Create a 200x150 JPEG
	testData := createTestJPEG(200, 150)
	originalLen := len(testData)
	
	// Resize to max 500px (should return original)
	resized, err := ResizeImage(testData, 500, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to resize image: %v", err)
	}
	
	// Should return exact same data
	if len(resized) != originalLen {
		t.Errorf("Expected original data length %d, got %d", originalLen, len(resized))
	}
	
	if !bytes.Equal(testData, resized) {
		t.Error("Expected original data to be returned unchanged")
	}
}

func TestResizeImage_UnsupportedFormat(t *testing.T) {
	testData := []byte("not an image")
	
	_, err := ResizeImage(testData, 500, "test.gif")
	if err == nil {
		t.Error("Expected error for unsupported format")
	}
	
	expectedErr := "unsupported image format: .gif"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestResizeImage_InvalidImageData(t *testing.T) {
	testData := []byte("not valid jpeg data")
	
	_, err := ResizeImage(testData, 500, "test.jpg")
	if err == nil {
		t.Error("Expected error for invalid image data")
	}
	
	// Should contain "failed to decode image"
	if !bytes.Contains([]byte(err.Error()), []byte("failed to decode image")) {
		t.Errorf("Expected decode error, got: %s", err.Error())
	}
}

func TestGetCachePath(t *testing.T) {
	rootDir := "/test/root"
	project := "myproject"
	trench := "mytrench"
	checksum := "abc123"
	size := "thumbnail"
	
	expected := "/test/root/.cache/myproject/mytrench/abc123-thumbnail.jpg"
	actual := GetCachePath(rootDir, project, trench, checksum, size)
	
	if actual != expected {
		t.Errorf("Expected %s, got %s", expected, actual)
	}
}

func TestEnsureCacheDir(t *testing.T) {
	// Create temp directory for test
	tempDir := t.TempDir()
	cachePath := filepath.Join(tempDir, "deep", "nested", "cache", "file.jpg")
	
	// Should create all directories
	err := EnsureCacheDir(cachePath)
	if err != nil {
		t.Fatalf("Failed to ensure cache dir: %v", err)
	}
	
	// Check that directory exists
	expectedDir := filepath.Join(tempDir, "deep", "nested", "cache")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("Expected directory %s to exist", expectedDir)
	}
}

func TestResizeImage_ThumbnailSize(t *testing.T) {
	// Test actual thumbnail size (265px)
	testData := createTestJPEG(1000, 1000)
	
	resized, err := ResizeImage(testData, 265, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to resize image: %v", err)
	}
	
	width, height, err := getImageDimensions(resized, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to decode resized image: %v", err)
	}
	
	if width != 265 || height != 265 {
		t.Errorf("Expected 265x265, got %dx%d", width, height)
	}
}

func TestResizeImage_PreviewSize(t *testing.T) {
	// Test actual preview size (1024px)
	testData := createTestJPEG(2000, 1500)
	
	resized, err := ResizeImage(testData, 1024, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to resize image: %v", err)
	}
	
	width, height, err := getImageDimensions(resized, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to decode resized image: %v", err)
	}
	
	// Should be 1024x768 (maintaining 4:3 aspect ratio)
	if width != 1024 || height != 768 {
		t.Errorf("Expected 1024x768, got %dx%d", width, height)
	}
}

func TestResizeImage_SquareImage(t *testing.T) {
	// Test square image resizing
	testData := createTestJPEG(500, 500)
	
	resized, err := ResizeImage(testData, 300, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to resize image: %v", err)
	}
	
	width, height, err := getImageDimensions(resized, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to decode resized image: %v", err)
	}
	
	if width != 300 || height != 300 {
		t.Errorf("Expected 300x300, got %dx%d", width, height)
	}
}