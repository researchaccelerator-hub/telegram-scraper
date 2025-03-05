package common

import (
	"github.com/rs/zerolog/log"
	"strings"
	"time"
)

// Configuration structure
type CrawlerConfig struct {
	DaprMode         bool
	DaprPort         int
	Concurrency      int
	Timeout          int
	UserAgent        string
	OutputFormat     string
	StorageRoot      string
	TDLibDatabaseURL string
	MinPostDate      time.Time
	DaprJobMode      bool
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

// parseSeedList parses a command-line flag "seed-list" into a slice of strings.
// The flag is expected to be a comma-separated list of seed channels.
// If the flag is not provided, it logs an informational message and returns an empty slice.
func ParseSeedList(stringList string) []string {

	if stringList == "" {
		log.Info().Msg("seed-list argument is not provided")
		return []string{}
	}

	// Split the string into a slice
	values := strings.Split(stringList, ",")
	return values
}
