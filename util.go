package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/rwcarlsen/goexif/exif"
	"golang.org/x/crypto/bcrypt"
)

func FileExists(name string) bool {
	fi, err := os.Stat(name)
	if err != nil {
		return false
	}
	return fi.Mode().IsRegular()
}

func ReadLines(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

// This function does not make any actual connections
func GetOutboundIP() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil, fmt.Errorf("Could not get outbound IP")
	}
	return addr.IP, nil
}

func Prefix(s string, n int) string {
	if len(s) > n {
		return s[:n]
	} else {
		return s
	}
}

type Set map[string]struct{}

func (s Set) Array() []string {
	var a []string
	for k := range s {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}

func (s Set) Insert(k string) {
	s[k] = struct{}{}
}

func (s Set) Union(a Set) Set {
	u := make(Set)
	for k := range s {
		u[k] = struct{}{}
	}
	for k := range a {
		u[k] = struct{}{}
	}
	return u
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func ResizeImage(data []byte, maxDimension uint, filename string) ([]byte, error) {
	// Step 1: Decode image with automatic format detection
	img, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	
	// Step 2: Handle EXIF orientation for JPEG files
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".jpg" || ext == ".jpeg" {
		img = fixOrientation(img, data)
	}
	
	// Step 3: Check if resize is needed
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	
	if width <= int(maxDimension) && height <= int(maxDimension) {
		// Image is already small enough, but we still need to re-encode
		// to strip EXIF and standardize format
		var buf bytes.Buffer
		err = imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(85))
		if err != nil {
			return nil, fmt.Errorf("failed to encode image: %w", err)
		}
		return buf.Bytes(), nil
	}
	
	// Step 4: Resize maintaining aspect ratio
	resized := imaging.Fit(img, int(maxDimension), int(maxDimension), imaging.Lanczos)
	
	// Step 5: Encode as JPEG
	var buf bytes.Buffer
	err = imaging.Encode(&buf, resized, imaging.JPEG, imaging.JPEGQuality(85))
	if err != nil {
		return nil, fmt.Errorf("failed to encode resized image: %w", err)
	}
	
	return buf.Bytes(), nil
}

// fixOrientation applies EXIF orientation transformation
func fixOrientation(img image.Image, data []byte) image.Image {
	// Extract EXIF data
	x, err := exif.Decode(bytes.NewReader(data))
	if err != nil {
		// No EXIF data or couldn't parse, return original
		return img
	}
	
	// Get orientation tag
	tag, err := x.Get(exif.Orientation)
	if err != nil {
		// No orientation tag, return original
		return img
	}
	
	orientation, err := tag.Int(0)
	if err != nil {
		// Couldn't read orientation value, return original
		return img
	}
	
	// Apply transformation based on EXIF orientation value
	switch orientation {
	case 1:
		// Normal orientation, no change needed
		return img
	case 2:
		// Horizontal flip
		return imaging.FlipH(img)
	case 3:
		// 180° rotation
		return imaging.Rotate180(img)
	case 4:
		// Vertical flip
		return imaging.FlipV(img)
	case 5:
		// Vertical flip + 90° CW rotation
		return imaging.Rotate90(imaging.FlipV(img))
	case 6:
		// 90° CW rotation
		return imaging.Rotate90(img)
	case 7:
		// Horizontal flip + 90° CW rotation
		return imaging.Rotate90(imaging.FlipH(img))
	case 8:
		// 270° CW rotation (90° CCW)
		return imaging.Rotate270(img)
	default:
		// Unknown orientation, return original
		return img
	}
}

func GetCachePath(rootDir, project, trench, name, checksum, size string) string {
	cacheDir := filepath.Join(rootDir, ".cache", project, trench)
	// Sanitize filename to prevent path issues
	safeName := strings.ReplaceAll(filepath.Base(name), "/", "_")
	safeName = strings.ReplaceAll(safeName, "\\", "_")
	return filepath.Join(cacheDir, fmt.Sprintf("%s-%s-%s.jpg", safeName, checksum, size))
}

func EnsureCacheDir(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0755)
}
