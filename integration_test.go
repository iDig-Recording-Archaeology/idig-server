package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Test configuration from environment variables
var (
	testServerURL = getEnv("IDIG_TEST_SERVER", "https://enki.agathe.gr")
	testUsername  = getEnv("IDIG_TEST_USER", "")
	testPassword  = getEnv("IDIG_TEST_PASSWORD", "")
)

// Test structures matching server responses
type TrenchesResponse struct {
	Trenches []Trench `json:"trenches"`
}

type SurveysResponse struct {
	Version string `json:"version"`
	Surveys []map[string]interface{} `json:"surveys"`
}

type TestAttachment struct {
	Name     string
	Checksum string
}

// TestListTrenches verifies we can get the list of all trenches
func TestListTrenches(t *testing.T) {
	if !hasCredentials(t) {
		return
	}

	resp, err := makeRequest("GET", "/idig", nil)
	if err != nil {
		t.Fatalf("Failed to list trenches: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var trenches TrenchesResponse
	if err := json.NewDecoder(resp.Body).Decode(&trenches); err != nil {
		t.Fatalf("Failed to decode trenches response: %v", err)
	}

	if len(trenches.Trenches) == 0 {
		t.Fatal("No trenches found - expected at least one")
	}

	// Verify we have expected projects
	foundAgora := false
	for _, trench := range trenches.Trenches {
		if trench.Project == "Agora" {
			foundAgora = true
			break
		}
	}
	if !foundAgora {
		t.Error("Expected to find Agora project in trenches list")
	}

	t.Logf("Found %d trenches", len(trenches.Trenches))
}

// TestTrenchDataRetrieval tests getting survey data from specific trenches
func TestTrenchDataRetrieval(t *testing.T) {
	if !hasCredentials(t) {
		return
	}

	testTrenches := []struct {
		project string
		trench  string
	}{
		{"Agora", "ΒΖ North 2015"},
		{"Agora", "Agora"},
	}

	for _, tt := range testTrenches {
		t.Run(fmt.Sprintf("%s/%s", tt.project, tt.trench), func(t *testing.T) {
			url := fmt.Sprintf("/idig/%s/%s/surveys", tt.project, tt.trench)
			resp, err := makeRequest("GET", url, nil)
			if err != nil {
				t.Fatalf("Failed to get surveys: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200, got %d", resp.StatusCode)
			}

			var surveys SurveysResponse
			if err := json.NewDecoder(resp.Body).Decode(&surveys); err != nil {
				t.Fatalf("Failed to decode surveys response: %v", err)
			}

			if len(surveys.Surveys) == 0 {
				t.Logf("No surveys found in %s/%s", tt.project, tt.trench)
			} else {
				t.Logf("Found %d surveys in %s/%s", len(surveys.Surveys), tt.project, tt.trench)
			}
		})
	}
}

// TestAttachmentsList tests extracting attachments from trench surveys
func TestAttachmentsList(t *testing.T) {
	if !hasCredentials(t) {
		return
	}

	// Test with Agora/ΒΖ North 2015 which should have attachments
	resp, err := makeRequest("GET", "/idig/Agora/ΒΖ North 2015/surveys", nil)
	if err != nil {
		t.Fatalf("Failed to get surveys: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var surveys SurveysResponse
	if err := json.NewDecoder(resp.Body).Decode(&surveys); err != nil {
		t.Fatalf("Failed to decode surveys response: %v", err)
	}

	attachments := extractAttachments(surveys.Surveys)
	if len(attachments) == 0 {
		t.Skip("No attachments found in test trench - skipping attachment tests")
	}

	t.Logf("Found %d attachments in ΒΖ North 2015", len(attachments))

	// Test first few attachments
	testCount := 3
	if len(attachments) < testCount {
		testCount = len(attachments)
	}

	for i := 0; i < testCount; i++ {
		att := attachments[i]
		t.Run(fmt.Sprintf("attachment_%s", att.Name), func(t *testing.T) {
			// Test original
			if err := testAttachmentDownload(t, "Agora", "ΒΖ North 2015", att, ""); err != nil {
				t.Errorf("Failed to download original: %v", err)
			}

			// Test thumbnail
			if err := testAttachmentDownload(t, "Agora", "ΒΖ North 2015", att, "thumbnail"); err != nil {
				t.Errorf("Failed to download thumbnail: %v", err)
			}

			// Test preview
			if err := testAttachmentDownload(t, "Agora", "ΒΖ North 2015", att, "preview"); err != nil {
				t.Errorf("Failed to download preview: %v", err)
			}
		})
	}
}

// BenchmarkThumbnailGeneration measures thumbnail generation performance
func BenchmarkThumbnailGeneration(b *testing.B) {
	if testUsername == "" || testPassword == "" {
		b.Skip("Set IDIG_TEST_USER and IDIG_TEST_PASSWORD environment variables to run benchmarks")
	}

	// Get a test attachment
	attachments := getTestAttachments(b)
	if len(attachments) == 0 {
		b.Skip("No attachments available for benchmark")
	}

	att := attachments[0]
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		url := fmt.Sprintf("/idig/Agora/ΒΖ North 2015/attachments/%s?checksum=%s&size=thumbnail", att.Name, att.Checksum)
		resp, err := makeRequest("GET", url, nil)
		if err != nil {
			b.Fatalf("Request failed: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// Helper functions

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func hasCredentials(t *testing.T) bool {
	if testUsername == "" || testPassword == "" {
		t.Skip("Set IDIG_TEST_USER and IDIG_TEST_PASSWORD environment variables to run integration tests")
		return false
	}
	return true
}

func makeRequest(method, path string, body io.Reader) (*http.Response, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	
	req, err := http.NewRequest(method, testServerURL+path, body)
	if err != nil {
		return nil, err
	}
	
	req.SetBasicAuth(testUsername, testPassword)
	return client.Do(req)
}

func extractAttachments(surveys []map[string]interface{}) []TestAttachment {
	var attachments []TestAttachment
	
	for _, survey := range surveys {
		relationAttachments, ok := survey["RelationAttachments"].(string)
		if !ok || relationAttachments == "" {
			continue
		}
		
		blocks := strings.Split(relationAttachments, "\n\n")
		for _, block := range blocks {
			var name, checksum string
			lines := strings.Split(block, "\n")
			for _, line := range lines {
				if strings.HasPrefix(line, "n=") {
					name = line[2:]
				} else if strings.HasPrefix(line, "d=") {
					checksum = line[2:]
				}
			}
			
			if name != "" && checksum != "" {
				attachments = append(attachments, TestAttachment{
					Name:     name,
					Checksum: checksum,
				})
			}
		}
	}
	
	return attachments
}

func testAttachmentDownload(t *testing.T, project, trench string, att TestAttachment, size string) error {
	url := fmt.Sprintf("/idig/%s/%s/attachments/%s?checksum=%s", project, trench, att.Name, att.Checksum)
	if size != "" {
		url += "&size=" + size
	}
	
	start := time.Now()
	resp, err := makeRequest("GET", url, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	
	duration := time.Since(start)
	sizeStr := "original"
	if size != "" {
		sizeStr = size
	}
	
	t.Logf("Downloaded %s (%s): %d bytes in %v", att.Name, sizeStr, len(data), duration)
	return nil
}

func getTestAttachments(tb testing.TB) []TestAttachment {
	resp, err := makeRequest("GET", "/idig/Agora/ΒΖ North 2015/surveys", nil)
	if err != nil {
		tb.Fatalf("Failed to get surveys: %v", err)
	}
	defer resp.Body.Close()

	var surveys SurveysResponse
	if err := json.NewDecoder(resp.Body).Decode(&surveys); err != nil {
		tb.Fatalf("Failed to decode surveys: %v", err)
	}

	return extractAttachments(surveys.Surveys)
}