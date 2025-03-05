package state

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/researchaccelerator-hub/telegram-scraper/model"
	"github.com/rs/zerolog/log"
)

// Config holds the configuration for StateManager
type Config struct {
	StorageRoot   string
	ContainerName string
	BlobNameRoot  string
	JobID         string
	CrawlID       string
}

// StateManager encapsulates state management with a configurable storage root prefix.
type StateManager struct {
	config       Config
	azureClient  *azblob.Client
	listFile     string
	progressFile string
}

// NewStateManager initializes a new StateManager with the given storage root prefix.
func NewStateManager(config Config) (*StateManager, error) {
	sm := &StateManager{
		config:       config,
		listFile:     filepath.Join(config.StorageRoot, "list.txt"),
		progressFile: filepath.Join(config.StorageRoot, "progress", fmt.Sprintf("%s.txt", config.CrawlID)),
	}

	// Initialize Azure client if we have the credentials
	if config.ContainerName != "" && config.BlobNameRoot != "" {
		accountURL := os.Getenv("AZURE_STORAGE_ACCOUNT_URL")
		if accountURL != "" {
			client, err := createAzureClient(accountURL)
			if err != nil {
				return nil, fmt.Errorf("failed to create Azure client: %w", err)
			}
			sm.azureClient = client
		}
	}

	return sm, nil
}

// NewStateManagerFromEnv creates a new StateManager from environment variables
func NewStateManagerFromEnv(crawlID string) (*StateManager, error) {
	storageRoot := os.Getenv("STORAGE_ROOT")
	if storageRoot == "" {
		storageRoot = "./storage" // Default value
	}

	containerName := os.Getenv("CONTAINER_NAME")
	blobNameRoot := os.Getenv("BLOB_NAME")
	jobID := os.Getenv("JOB_UID")

	config := Config{
		StorageRoot:   storageRoot,
		ContainerName: containerName,
		BlobNameRoot:  blobNameRoot,
		JobID:         jobID,
		CrawlID:       crawlID,
	}

	return NewStateManager(config)
}

// createAzureClient creates a new Azure Blob Storage client
func createAzureClient(accountURL string) (*azblob.Client, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	client, err := azblob.NewClient(accountURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure Blob Storage client: %w", err)
	}

	return client, nil
}

type readSeekCloserWrapper struct {
	*bytes.Reader
}

func (r readSeekCloserWrapper) Close() error {
	return nil
}

// SeedSetup initializes the list file with the provided seed list if it does not exist,
// and then loads the list from the file.
func (sm *StateManager) SeedSetup(seedlist []string) ([]string, error) {
	useAzure := sm.shouldUseAzure()

	if useAzure {
		// Check if list exists in Azure
		exists, err := sm.blobExists(sm.config.ContainerName, sm.getListBlobPath())
		if err != nil {
			return nil, fmt.Errorf("failed to check if list blob exists: %w", err)
		}

		if !exists {
			// Need to seed the list
			if err := sm.seedListToBlob(seedlist); err != nil {
				return nil, fmt.Errorf("failed to seed list to Azure: %w", err)
			}
		}

		// Load list from Azure
		return sm.loadListFromBlob()
	} else {
		// Check if list exists locally
		if _, err := os.Stat(sm.listFile); os.IsNotExist(err) {
			if err := sm.seedList(seedlist); err != nil {
				return nil, fmt.Errorf("failed to seed list locally: %w", err)
			}
		}

		// Load list from local file
		return sm.loadList()
	}
}

// loadListFromBlob downloads a list from an Azure Blob Storage container and returns it as a slice of strings.
func (sm *StateManager) loadListFromBlob() ([]string, error) {
	if sm.azureClient == nil {
		return nil, fmt.Errorf("Azure client not initialized")
	}

	// Create temporary file to download the blob
	tmpFile, err := os.CreateTemp("", "list-*.txt")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpFile.Name()) // Clean up after reading
	defer tmpFile.Close()

	// Download blob directly to temp file
	_, err = sm.azureClient.DownloadFile(
		context.TODO(),
		sm.config.ContainerName,
		sm.getListBlobPath(),
		tmpFile,
		nil,
	)

	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Blob not found, return empty list
		}
		return nil, fmt.Errorf("failed to download list from Azure: %w", err)
	}

	// Read and parse the downloaded file
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to reset file pointer: %w", err)
	}

	var list []string
	scanner := bufio.NewScanner(tmpFile)
	for scanner.Scan() {
		list = append(list, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read downloaded list: %w", err)
	}

	log.Info().Msgf("Loaded %d items from Azure Blob Storage", len(list))
	return list, nil
}

// seedList writes a list of items to a file, creating the file if it does not exist.
func (sm *StateManager) seedList(items []string) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(sm.listFile), os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	file, err := os.Create(sm.listFile)
	if err != nil {
		return fmt.Errorf("failed to create list file: %w", err)
	}
	defer file.Close()

	for _, item := range items {
		if _, err := file.WriteString(item + "\n"); err != nil {
			return fmt.Errorf("failed to write to list file: %w", err)
		}
	}

	log.Info().Msg("List seeded successfully.")
	return nil
}

func (sm *StateManager) seedListToBlob(items []string) error {
	if sm.azureClient == nil {
		return fmt.Errorf("Azure client not initialized")
	}

	var data strings.Builder
	for _, item := range items {
		data.WriteString(item + "\n")
	}

	// Upload to Azure Blob Storage
	reader := strings.NewReader(data.String())
	_, err := sm.azureClient.UploadStream(
		context.TODO(),
		sm.config.ContainerName,
		sm.getListBlobPath(),
		reader,
		nil,
	)

	if err != nil {
		return fmt.Errorf("failed to upload list to Azure: %w", err)
	}

	log.Info().Msgf("Seed list uploaded to Azure: %s/%s", sm.config.ContainerName, sm.getListBlobPath())
	return nil
}

// loadList reads the list of items from the list file and returns them as a slice of strings.
func (sm *StateManager) loadList() ([]string, error) {
	file, err := os.Open(sm.listFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open list file: %w", err)
	}
	defer file.Close()

	var list []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		list = append(list, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read list file: %w", err)
	}

	return list, nil
}

// LoadProgress retrieves the current progress index from the progress file.
func (sm *StateManager) LoadProgress() (int, error) {
	if sm.shouldUseAzure() {
		return sm.loadProgressFromBlob()
	}

	// Ensure progress directory exists
	progressDir := filepath.Dir(sm.progressFile)
	if err := os.MkdirAll(progressDir, os.ModePerm); err != nil {
		return 0, fmt.Errorf("failed to create progress directory: %w", err)
	}

	// Local file loading
	if _, err := os.Stat(sm.progressFile); os.IsNotExist(err) {
		return 0, nil // Start from the beginning if no progress file
	}

	data, err := os.ReadFile(sm.progressFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read progress file: %w", err)
	}

	progress, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid progress format: %w", err)
	}

	log.Info().Msgf("Loaded progress for crawl '%s': %d", sm.config.CrawlID, progress)
	return progress, nil
}

// SaveProgress writes the given progress index to the progress file.
func (sm *StateManager) SaveProgress(index int) error {
	if sm.shouldUseAzure() {
		return sm.saveProgressToBlob(index)
	}

	// Ensure directory exists
	progressDir := filepath.Dir(sm.progressFile)
	if err := os.MkdirAll(progressDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create progress directory: %w", err)
	}

	// Local file saving
	file, err := os.Create(sm.progressFile)
	if err != nil {
		return fmt.Errorf("failed to create progress file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(strconv.Itoa(index) + "\n")
	if err != nil {
		return fmt.Errorf("failed to write progress: %w", err)
	}

	log.Info().Msgf("Saved progress for crawl '%s': %d", sm.config.CrawlID, index)
	return nil
}

// loadProgressFromBlob downloads and reads the progress file from Azure Blob Storage.
func (sm *StateManager) loadProgressFromBlob() (int, error) {
	if sm.azureClient == nil {
		return 0, fmt.Errorf("Azure client not initialized")
	}

	// Create a temporary file to download the blob
	tmpFile, err := os.CreateTemp("", "progress-*.txt")
	if err != nil {
		return 0, fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpFile.Name()) // Clean up after reading
	defer tmpFile.Close()

	// Check if blob exists
	exists, err := sm.blobExists(sm.config.ContainerName, sm.getProgressBlobPath())
	if err != nil {
		return 0, fmt.Errorf("failed to check if progress blob exists: %w", err)
	}

	if !exists {
		return 0, nil // Start from 0 if the blob doesn't exist
	}

	// Download blob to file
	_, err = sm.azureClient.DownloadFile(
		context.TODO(),
		sm.config.ContainerName,
		sm.getProgressBlobPath(),
		tmpFile,
		nil,
	)

	if err != nil {
		return 0, fmt.Errorf("failed to download progress file: %w", err)
	}

	// Read and parse the downloaded file
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return 0, fmt.Errorf("failed to reset file pointer: %w", err)
	}

	data, err := io.ReadAll(tmpFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read progress data: %w", err)
	}

	progress, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid progress format: %w", err)
	}

	log.Info().Msgf("Loaded progress for crawl '%s' from Azure: %d", sm.config.CrawlID, progress)
	return progress, nil
}

// saveProgressToBlob uploads the current progress index to Azure Blob Storage.
func (sm *StateManager) saveProgressToBlob(index int) error {
	if sm.azureClient == nil {
		return fmt.Errorf("Azure client not initialized")
	}

	// Write progress to an in-memory buffer
	data := []byte(strconv.Itoa(index) + "\n")
	reader := bytes.NewReader(data)

	_, err := sm.azureClient.UploadStream(
		context.TODO(),
		sm.config.ContainerName,
		sm.getProgressBlobPath(),
		reader,
		nil,
	)

	if err != nil {
		return fmt.Errorf("failed to upload progress to Azure: %w", err)
	}

	log.Info().Msgf("Progress for crawl '%s' saved to Azure: %d", sm.config.CrawlID, index)
	return nil
}

// StoreData saves a model.Post to a JSONL file under the channel's directory.
func (sm *StateManager) StoreData(channelname string, post model.Post) error {
	postData, err := json.Marshal(post)
	if err != nil {
		return fmt.Errorf("failed to marshal post: %w", err)
	}

	postData = append(postData, '\n')

	if sm.shouldUseAzure() {
		// Azure Blob Storage logic
		if sm.azureClient == nil {
			return fmt.Errorf("Azure client not initialized")
		}

		blobPath := sm.getChannelDataBlobPath(channelname)

		// Check if blob exists
		exists, err := sm.blobExists(sm.config.ContainerName, blobPath)
		if err != nil {
			return fmt.Errorf("failed to check if channel blob exists: %w", err)
		}

		// For append blobs, we need to create the blob first if it doesn't exist
		if !exists {
			if err := sm.createAppendBlob(sm.config.ContainerName, blobPath); err != nil {
				return fmt.Errorf("failed to create append blob: %w", err)
			}
		}

		// Append data to blob
		if err := sm.appendToBlob(sm.config.ContainerName, blobPath, postData); err != nil {
			return fmt.Errorf("failed to append data to blob: %w", err)
		}

		log.Info().Msgf("Post successfully uploaded to Azure for channel %s", channelname)
		return nil
	}

	// Local Storage logic
	channelDir := filepath.Join(sm.config.StorageRoot, "crawls", sm.config.CrawlID, channelname)
	if err := os.MkdirAll(channelDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory for channel %s: %w", channelname, err)
	}

	jsonlFile := filepath.Join(channelDir, "data.jsonl")
	file, err := os.OpenFile(jsonlFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", jsonlFile, err)
	}
	defer file.Close()

	if _, err := file.Write(postData); err != nil {
		return fmt.Errorf("failed to write post to file %s: %w", jsonlFile, err)
	}

	log.Info().Msgf("Post successfully stored locally for channel %s", channelname)
	return nil
}

// UploadBlobFileAndDelete uploads a local file to Azure Blob Storage and deletes it locally upon successful upload.
func (sm *StateManager) UploadBlobFileAndDelete(channelid, rawURL, filePath string) error {
	// Check if the file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %w", err)
	}

	// Open the file for reading
	file, err := os.OpenFile(filePath, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Read file content into memory (we need this for both local and remote storage)
	fileContent, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("failed to read file content: %w", err)
	}

	// Reset file pointer to beginning for potential reuse
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to reset file pointer: %w", err)
	}

	filename := filepath.Base(filePath)

	// Try Azure upload if it should be used
	if sm.shouldUseAzure() && sm.azureClient != nil {
		fp, err := sm.urlToBlobPath(rawURL)
		if err != nil {
			log.Warn().Err(err).Msg("failed to convert URL to blob path, proceeding with local storage only")
		} else {
			fp = fp + "_" + filename
			blobName := filepath.Join(
				sm.config.BlobNameRoot,
				sm.config.JobID,
				sm.config.CrawlID,
				"media",
				channelid,
				fp,
			)

			// Upload to Azure
			_, err = sm.azureClient.UploadFile(
				context.TODO(),
				sm.config.ContainerName,
				blobName,
				file,
				nil,
			)

			if err != nil {
				log.Error().Err(err).Msg("failed to upload file to Azure, proceeding with local storage only")
			} else {
				log.Info().Msg("File uploaded to Azure successfully.")
			}
		}
	} else {
		// Always store locally regardless of Azure upload result
		outputDir := filepath.Join(sm.config.StorageRoot, sm.config.CrawlID)
		if outputDir == "" {
			outputDir = "output" // Default directory if not specified
		}

		// Create media directory structure
		mediaDir := filepath.Join(outputDir, "media", channelid)
		if err := os.MkdirAll(mediaDir, 0755); err != nil {
			return fmt.Errorf("failed to create media directory: %w", err)
		}

		// Create a new filename by sanitizing the rawURL to create a unique but readable name
		sanitizedURL := sanitizeURLForFilename(rawURL)
		localFilename := sanitizedURL + "_" + filename
		localFilePath := filepath.Join(mediaDir, localFilename)

		// Write file locally
		if err := os.WriteFile(localFilePath, fileContent, 0644); err != nil {
			return fmt.Errorf("failed to write local file copy: %w", err)
		}

		log.Info().Str("path", localFilePath).Msg("File saved locally")
	}

	// Remove the original file after successful processing
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete original file after upload: %w", err)
	}

	log.Info().Msg("Original file deleted successfully.")
	return nil
}

// sanitizeURLForFilename creates a safe filename from a URL
func sanitizeURLForFilename(url string) string {
	// Remove protocol and domain parts
	parts := strings.Split(url, "/")
	relevantParts := parts[len(parts)-2:]

	// Replace unsafe characters with underscores
	sanitized := strings.Join(relevantParts, "_")
	sanitized = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, sanitized)

	// Limit filename length
	if len(sanitized) > 50 {
		sanitized = sanitized[:50]
	}

	return sanitized
}

// urlToBlobPath converts a raw URL string into a blob path
func (sm *StateManager) urlToBlobPath(rawURL string) (string, error) {
	// Parse the URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	// Get the path and remove the leading slash
	path := strings.TrimPrefix(parsedURL.Path, "/")

	return path, nil
}

// blobExists checks if a blob exists in Azure
func (sm *StateManager) blobExists(containerName, blobName string) (bool, error) {
	if sm.azureClient == nil {
		return false, fmt.Errorf("Azure client not initialized")
	}

	blobClient := sm.azureClient.ServiceClient().NewContainerClient(containerName).NewBlobClient(blobName)
	_, err := blobClient.GetProperties(context.Background(), nil)

	if err == nil {
		return true, nil
	}

	if strings.Contains(err.Error(), "404") {
		return false, nil
	}

	return false, err
}

// createAppendBlob creates a new append blob in Azure
func (sm *StateManager) createAppendBlob(containerName, blobName string) error {
	if sm.azureClient == nil {
		return fmt.Errorf("Azure client not initialized")
	}

	appendBlobClient := sm.azureClient.ServiceClient().NewContainerClient(containerName).NewAppendBlobClient(blobName)
	_, err := appendBlobClient.Create(context.Background(), nil)
	return err
}

// appendToBlob appends data to an existing append blob in Azure
func (sm *StateManager) appendToBlob(containerName, blobName string, data []byte) error {
	if sm.azureClient == nil {
		return fmt.Errorf("Azure client not initialized")
	}

	appendBlobClient := sm.azureClient.ServiceClient().NewContainerClient(containerName).NewAppendBlobClient(blobName)
	reader := bytes.NewReader(data)
	readSeekCloser := readSeekCloserWrapper{reader}

	_, err := appendBlobClient.AppendBlock(context.Background(), readSeekCloser, nil)
	return err
}

// Helper methods for blob paths
func (sm *StateManager) getListBlobPath() string {
	return filepath.Join(sm.config.BlobNameRoot, sm.config.JobID, "list.txt")
}

func (sm *StateManager) getProgressBlobPath() string {
	// Incorporate crawlID into the progress file path for per-crawl tracking
	return filepath.Join(
		sm.config.BlobNameRoot,
		sm.config.JobID,
		"progress",
		fmt.Sprintf("%s.txt", sm.config.CrawlID),
	)
}

func (sm *StateManager) getChannelDataBlobPath(channelname string) string {
	return filepath.Join(sm.config.BlobNameRoot, sm.config.JobID, sm.config.CrawlID, channelname+".jsonl")
}

// Helper method to determine if Azure storage should be used
func (sm *StateManager) shouldUseAzure() bool {
	return sm.config.ContainerName != "" && sm.config.BlobNameRoot != "" && sm.azureClient != nil
}
