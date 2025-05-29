package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/image/draw"
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
	var img image.Image
	var err error
	
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg":
		img, err = jpeg.Decode(bytes.NewReader(data))
	case ".png":
		img, err = png.Decode(bytes.NewReader(data))
	default:
		return nil, fmt.Errorf("unsupported image format: %s", ext)
	}
	
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	
	if width <= int(maxDimension) && height <= int(maxDimension) {
		return data, nil
	}
	
	var newWidth, newHeight int
	if width > height {
		newWidth = int(maxDimension)
		newHeight = height * int(maxDimension) / width
	} else {
		newWidth = width * int(maxDimension) / height
		newHeight = int(maxDimension)
	}
	
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85})
	if err != nil {
		return nil, fmt.Errorf("failed to encode resized image: %w", err)
	}
	
	return buf.Bytes(), nil
}

func GetCachePath(rootDir, project, trench, checksum, size string) string {
	cacheDir := filepath.Join(rootDir, ".cache", project, trench)
	return filepath.Join(cacheDir, fmt.Sprintf("%s-%s.jpg", checksum, size))
}

func EnsureCacheDir(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0755)
}
