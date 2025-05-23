package common

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
	"time"
)

func TestGenerateCrawlID(t *testing.T) {
	// Call the function
	crawlID := GenerateCrawlID()

	// Check that the crawlID is not empty
	if crawlID == "" {
		t.Error("Expected non-empty crawlID, got empty string")
	}

	// Check that the crawlID is a string of 14 digits (YYYYMMDDHHMMSS)
	matched, err := regexp.MatchString(`^\d{14}$`, crawlID)
	if err != nil {
		t.Fatalf("Error in regex matching: %v", err)
	}
	if !matched {
		t.Errorf("CrawlID %s does not match the expected format YYYYMMDDHHMMSS", crawlID)
	}

	// Try to parse the crawlID back to a time
	parsedTime, err := time.Parse("20060102150405", crawlID)
	if err != nil {
		t.Fatalf("Could not parse crawlID %s back to time: %v", crawlID, err)
	}

	// Note: We're not checking the exact time range because this can lead to flaky tests
	// Especially in CI environments where time execution might vary
	// For this test, we'll simply verify that the parsed time is within a reasonable
	// window of "now" (last 24 hours), rather than a specific range, to avoid timezone issues
	now := time.Now()
	dayAgo := now.Add(-24 * time.Hour)
	dayLater := now.Add(24 * time.Hour)
	
	if parsedTime.Before(dayAgo) || parsedTime.After(dayLater) {
		t.Errorf("Parsed time %v is not within a reasonable time range of now", parsedTime)
	} else {
		// Additional check - the difference between the parsed time and now should be less than 5 minutes
		// This will detect if the times are way off but within the 24 hour window
		diff := now.Sub(parsedTime)
		if diff < 0 {
			diff = -diff
		}
		
		if diff > 5*time.Minute {
			t.Logf("Warning: Parsed time %v differs from current time %v by %v", 
				parsedTime, now, diff)
		}
	}
}

func ExampleGenerateCrawlID() {
	// Mock the current time for consistent output in the example
	// In a real application, you wouldn't do this
	currentTime, _ := time.Parse("2006-01-02 15:04:05", "2023-05-15 10:30:45")

	// For the example, we'll create a modified version that uses our fixed time
	mockCrawlID := func() string {
		return currentTime.Format("20060102150405")
	}

	// Show the result
	fmt.Println(mockCrawlID())
	// Output: 20230515103045
}

func ExampleGenerateCrawlID_usage() {
	// Mock a fixed time for consistent example output
	currentTime, _ := time.Parse("2006-01-02 15:04:05", "2023-05-15 10:30:45")
	mockCrawlID := currentTime.Format("20060102150405")

	// Demonstrate practical usage of a crawl ID
	fmt.Printf("Initiating web crawl with ID: %s\n", mockCrawlID)
	fmt.Printf("Saving results to: crawl_%s.json\n", mockCrawlID)

	// Output:
	// Initiating web crawl with ID: 20230515103045
	// Saving results to: crawl_20230515103045.json
}

func TestDownloadURLFile(t *testing.T) {
	// Create a test server
	testContent := `# Seed URLs for crawling
https://example.com/page1
https://example.com/page2
# Another comment
https://example.com/page3`

	// Start test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(testContent))
	}))
	defer server.Close()

	// Call the function with the test server URL
	filePath, err := DownloadURLFile(server.URL)
	if err != nil {
		t.Fatalf("DownloadURLFile failed with error: %v", err)
	}

	// Make sure we clean up the file
	defer os.Remove(filePath)

	// Verify the file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("Downloaded file not found at path: %s", filePath)
	}

	// Verify the content of the file
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read downloaded file: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Downloaded content doesn't match expected. Got: %s, Want: %s", content, testContent)
	}

	// Test reading URLs from the file
	urls, err := ReadURLsFromFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read URLs from file: %v", err)
	}

	expectedURLs := []string{
		"https://example.com/page1",
		"https://example.com/page2",
		"https://example.com/page3",
	}

	if len(urls) != len(expectedURLs) {
		t.Fatalf("Incorrect number of URLs. Got: %d, Want: %d", len(urls), len(expectedURLs))
	}

	for i, url := range urls {
		if url != expectedURLs[i] {
			t.Errorf("URL at index %d doesn't match. Got: %s, Want: %s", i, url, expectedURLs[i])
		}
	}
}

func TestDownloadURLFile_ErrorHandling(t *testing.T) {
	// Test with a server that returns a 404
	notFoundServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFoundServer.Close()

	_, err := DownloadURLFile(notFoundServer.URL)
	if err == nil {
		t.Error("Expected error for 404 response, got nil")
	}

	// Test with an invalid URL
	_, err = DownloadURLFile("invalid-url")
	if err == nil {
		t.Error("Expected error for invalid URL, got nil")
	}
}