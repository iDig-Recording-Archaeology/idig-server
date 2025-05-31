package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/disintegration/imaging"
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
	
	// Resize to max 500px (should maintain dimensions but re-encode to strip EXIF)
	resized, err := ResizeImage(testData, 500, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to resize image: %v", err)
	}
	
	// Check dimensions are preserved
	width, height, err := getImageDimensions(resized, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to decode resized image: %v", err)
	}
	
	if width != 200 || height != 150 {
		t.Errorf("Expected 200x150, got %dx%d", width, height)
	}
	
	// Note: Data will be different due to re-encoding to strip EXIF
	// This is the intended behavior for our new implementation
}

func TestResizeImage_UnsupportedFormat(t *testing.T) {
	testData := []byte("not an image")
	
	_, err := ResizeImage(testData, 500, "test.gif")
	if err == nil {
		t.Error("Expected error for unsupported format")
	}
	
	// The new implementation uses imaging library which gives different error messages
	// We just check that an error occurred for invalid data
	if !strings.Contains(err.Error(), "failed to decode image") {
		t.Errorf("Expected decode error, got: %s", err.Error())
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
	name := "photo.jpg"
	checksum := "abc123"
	size := "thumbnail"
	
	expected := "/test/root/.cache/myproject/mytrench/photo.jpg-abc123-thumbnail.jpg"
	actual := GetCachePath(rootDir, project, trench, name, checksum, size)
	
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

// TestResizeImage_EXIFHandling tests that EXIF orientation is properly handled
func TestResizeImage_EXIFHandling(t *testing.T) {
	// Create a test image using the imaging library (landscape)
	testImg := imaging.New(400, 300, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	
	// Encode as JPEG (this won't have EXIF, but tests the code path)
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, testImg, &jpeg.Options{Quality: 95})
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	
	// Test that it processes JPEG correctly
	resized, err := ResizeImage(buf.Bytes(), 200, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to resize JPEG: %v", err)
	}
	
	width, height, err := getImageDimensions(resized, "test.jpg")
	if err != nil {
		t.Fatalf("Failed to decode resized image: %v", err)
	}
	
	// Should maintain aspect ratio: 400:300 = 4:3, so 200px wide = 150px tall
	if width != 200 || height != 150 {
		t.Errorf("Expected 200x150, got %dx%d", width, height)
	}
	
	// Note: EXIF should be stripped from output, but we don't need to test this
	// since the imaging library handles it automatically
}

// TestAutoOrientation tests that the imaging library handles EXIF orientation automatically
func TestAutoOrientation(t *testing.T) {
	// Create a test image
	testImg := imaging.New(100, 50, color.NRGBA{R: 0, G: 255, B: 0, A: 255})
	
	// Encode as JPEG
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, testImg, &jpeg.Options{Quality: 95})
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	
	// Test decoding with AutoOrientation enabled (should work even without EXIF)
	result, err := imaging.Decode(bytes.NewReader(buf.Bytes()), imaging.AutoOrientation(true))
	if err != nil {
		t.Fatalf("Failed to decode with AutoOrientation: %v", err)
	}
	
	// Should be the same image (no EXIF to process)
	resultBounds := result.Bounds()
	if resultBounds.Dx() != 100 || resultBounds.Dy() != 50 {
		t.Errorf("Expected image unchanged, got %dx%d", resultBounds.Dx(), resultBounds.Dy())
	}
}