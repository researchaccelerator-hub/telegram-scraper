package dapr

import (
	"github.com/researchaccelerator-hub/telegram-scraper/common"
	"github.com/researchaccelerator-hub/telegram-scraper/crawl"
	"github.com/researchaccelerator-hub/telegram-scraper/state"
	"github.com/researchaccelerator-hub/telegram-scraper/telegramhelper"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"os"
	"strings"
	"time"
)

// StartStandaloneMode initializes and starts the crawler in standalone mode. It collects URLs from the provided list or file,
// configures the crawler using the specified configuration, and optionally runs code generation. If no URLs are provided,
// the function logs a fatal error. The function logs the start and completion of the crawling process.
// Parameters:
//   - urlList: A list of URLs to crawl.
//   - urlFile: A file containing URLs to crawl.
//   - crawlerCfg: Configuration settings for the crawler.
//   - generateCode: A flag indicating whether to run code generation.
func StartDaprStandaloneMode(urlList []string, urlFile string, crawlerCfg common.CrawlerConfig, generateCode bool) {
	log.Info().Msg("Starting crawler in standalone mode")

	// Collect URLs from command line arguments or file
	var urls []string

	if len(urlList) > 0 {
		urls = append(urls, urlList...)
	}

	if urlFile != "" {
		fileURLs, err := readURLsFromFile(urlFile)
		if err != nil {
			log.Fatal().Err(err).Msg("Failed to read URLs from file")
		}
		urls = append(urls, fileURLs...)
	}

	if len(urls) == 0 {
		log.Fatal().Msg("No URLs provided. Use --urls or --url-file to specify URLs to crawl")
	}

	log.Info().Msgf("Starting crawl of %d URLs with concurrency %d", len(urls), crawlerCfg.Concurrency)

	if generateCode {
		log.Info().Msg("Running code generation...")
		svc := &telegramhelper.RealTelegramService{}
		telegramhelper.GenCode(svc, crawlerCfg.StorageRoot)
		os.Exit(0)
	}

	launch(urls, crawlerCfg)

	log.Info().Msg("Crawling completed")
}

// readURLsFromFile reads a file specified by the filename and returns a slice of URLs.
// It ignores empty lines and lines starting with a '#' character, which are considered comments.
// Returns an error if the file cannot be read.
func readURLsFromFile(filename string) ([]string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var urls []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}

	return urls, nil
}

// launch initializes and runs the scraping process for a given list of strings using the specified crawler configuration.
//
// It generates a unique crawl ID, sets up the state manager, and seeds the list. The function then loads the progress
// and processes each item in the list from the last saved progress point. Errors during processing are logged, and the
// progress is saved after each item is processed. The function ensures that all items are processed successfully, and
// handles any panics that occur during item processing.
//
// Parameters:
//   - stringList: A slice of strings representing the items to be processed.
//   - crawlCfg: A CrawlerConfig struct containing configuration settings for the crawler.
func launch(stringList []string, crawlCfg common.CrawlerConfig) {
	seenURLs := make(map[string]bool)

	// Initialize seenURLs with the seed URLs
	for _, url := range stringList {
		seenURLs[url] = true
	}
	crawlid := common.GenerateCrawlID()
	log.Info().Msgf("Starting scraper for crawl: %s", crawlid)
	cfg := state.Config{
		StorageRoot:   crawlCfg.StorageRoot,
		ContainerName: crawlid,
		BlobNameRoot:  "",
		JobID:         "",
		CrawlID:       crawlid,
		DAPREnabled:   crawlCfg.DaprMode,
	}
	sm, err := state.NewStateManager(cfg)
	if err != nil {
		log.Error().Err(err).Msg("Failed to load progress")
	}
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	list, err := sm.SeedSetup(stringList)
	// Load progress
	for i := 0; i < len(list); i++ {
		l := list[i]
		for j := 0; j < len(l.Pages); j++ {
			la := l.Pages[j]
			if la.Status != "fetched" {
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Error().Msgf("Recovered from panic while processing item: %s, error: %v", la.URL, r)
							// Continue to the next item
						}
					}()
					la.Timestamp = time.Now()
					if outlinks, err := crawl.Run(crawlid, &la, crawlCfg.StorageRoot, *sm, crawlCfg); err != nil {
						log.Error().Stack().Err(err).Msgf("Error processing item %s", la.URL)
						la.Status = "error"
					} else {
						la.Status = "fetched"
						pag := make([]state.Page, 0) // Initialize as empty, we'll add only unique URLs

						// Deduplicate outlinks within this batch and check against all previously seen URLs
						uniqueOutlinks := make(map[string]*state.Page)
						for _, ol := range outlinks {
							if _, exists := seenURLs[ol.URL]; !exists {
								// This is a new URL we haven't seen before
								seenURLs[ol.URL] = true
								uniqueOutlinks[ol.URL] = ol
							} else {
								// Skip this URL as we've seen it before (prevents cycles)
								log.Debug().Msgf("Skipping already seen URL: %s", ol.URL)
							}
						}

						// Convert map to slice
						for _, ol := range uniqueOutlinks {
							pag = append(pag, *ol)
						}

						if len(pag) > 0 {

							// Add unique pages to the next layer
							if len(list) > l.Depth+1 {
								existing := list[l.Depth+1]
								existing.Pages = append(existing.Pages, pag...)

								// Deduplicate the layer
								uniquePages := make(map[string]state.Page)
								for _, page := range existing.Pages {
									uniquePages[page.URL] = page
								}

								// Reset and rebuild the Pages slice
								existing.Pages = make([]state.Page, 0, len(uniquePages))
								for _, page := range uniquePages {
									existing.Pages = append(existing.Pages, page)
								}
							} else {
								layer := state.Layer{
									Depth: l.Depth + 1,
									Pages: pag,
								}
								list = append(list, &layer)
							}
						}
					}
					err = sm.StoreLayers(list)
					if err != nil {
						log.Error().Stack().Err(err).Msg("Failed to store layers")
					}
				}()
			}
		}
	}

	log.Info().Msg("All items processed successfully.")

}
