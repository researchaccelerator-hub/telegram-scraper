package common

import (
	"time"
)

// Configuration structure
type CrawlerConfig struct {
	DaprMode     bool
	DaprPort     int
	Concurrency  int
	Timeout      int
	UserAgent    string
	OutputFormat string
	StorageRoot  string
	MinPostDate  time.Time
}

// GenerateCrawlID generates a unique identifier based on the current timestamp.
// The identifier is formatted as a string in the "YYYYMMDDHHMMSS" format.
func GenerateCrawlID() string {
	// Get the current timestamp
	currentTime := time.Now()

	// Format the timestamp to a string (e.g., "20060102150405" for YYYYMMDDHHMMSS)
	crawlID := currentTime.Format("20060102150405")

	return crawlID
}
