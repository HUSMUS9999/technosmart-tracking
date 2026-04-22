package gdrive

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"fiber-tracker/internal/excel"
)

// Client manages Google Drive public folder sync.
// Uses "Anyone with the link" sharing — no credentials needed.
type Client struct {
	folderID   string
	folderName string
	mu         sync.RWMutex
	httpClient *http.Client
}

// FileInfo holds metadata for a file in Google Drive.
type FileInfo struct {
	ID   string
	Name string
}

// New creates a new Drive client.
func New() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// folderIDRegex extracts the folder ID from various Drive URL formats.
var folderIDRegex = regexp.MustCompile(`(?:folders/|id=)([a-zA-Z0-9_-]{10,})`)

// ParseFolderLink extracts the folder ID from a Google Drive link.
// Supports: https://drive.google.com/drive/folders/XXXXX
//
//	https://drive.google.com/drive/folders/XXXXX?usp=sharing
//	or a raw folder ID
func ParseFolderLink(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}

	// Try to extract from URL
	match := folderIDRegex.FindStringSubmatch(link)
	if len(match) >= 2 {
		return match[1]
	}

	// Maybe it's a raw folder ID already
	if len(link) > 10 && !strings.Contains(link, "/") && !strings.Contains(link, " ") {
		return link
	}

	return ""
}

// SetFolder configures the folder to watch.
func (c *Client) SetFolder(folderID, folderName string) {
	c.mu.Lock()
	c.folderID = folderID
	c.folderName = folderName
	c.mu.Unlock()
}

// FolderID returns the current folder ID.
func (c *Client) FolderID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.folderID
}

// IsConfigured returns true if a folder is set.
func (c *Client) IsConfigured() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.folderID != ""
}

// ListFiles lists .xlsx files in the public folder.
// Uses Google's embedded folder view which works for "Anyone with the link" shares.
func (c *Client) ListFiles() ([]FileInfo, error) {
	c.mu.RLock()
	folderID := c.folderID
	c.mu.RUnlock()

	if folderID == "" {
		return nil, fmt.Errorf("no folder configured")
	}

	// Use Google's export endpoint for public folders
	url := fmt.Sprintf("https://drive.google.com/embeddedfolderview?id=%s", folderID)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MocaConsult/1.0)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("folder not accessible (HTTP %d) — make sure sharing is set to 'Anyone with the link'", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	html := string(body)
	return parseFilesFromHTML(html), nil
}

// parseFilesFromHTML extracts file IDs and names from the embedded folder view HTML.
// We target the flip-entry blocks which contain the true ID and the true title.
var fileEntryRegex = regexp.MustCompile(`id="entry-([a-zA-Z0-9_-]{25,})"[^>]*>.*?<div class="flip-entry-title">([^<]+\.xlsx)</div>`)

// Alternative regex patterns for different Drive HTML formats
var altFileRegex1 = regexp.MustCompile(`\["([a-zA-Z0-9_-]{25,})"[^]]*"([^"]+\.xlsx)"`)
var altFileRegex2 = regexp.MustCompile(`href="[^"]*?/d/([a-zA-Z0-9_-]{25,})/[^"]*".*?<div[^>]*title="([^"]*\.xlsx)"`)
var altFileRegex3 = regexp.MustCompile(`/file/d/([a-zA-Z0-9_-]{25,})[^"]*"[^>]*>[^<]*([^<]*\.xlsx)`)

func parseFilesFromHTML(html string) []FileInfo {
	var files []FileInfo
	seen := make(map[string]bool)

	// Try multiple regex patterns since Google changes their HTML format
	patterns := []*regexp.Regexp{fileEntryRegex, altFileRegex1, altFileRegex2, altFileRegex3}

	for _, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(html, -1)
		for _, m := range matches {
			if len(m) >= 3 {
				id := m[1]
				name := strings.TrimSpace(m[2])
				if !seen[id] && strings.HasSuffix(strings.ToLower(name), ".xlsx") {
					seen[id] = true
					files = append(files, FileInfo{ID: id, Name: name})
				}
			}
		}
	}

	// Fallback: try to find any xlsx references with file IDs
	if len(files) == 0 {
		// Just look for the view links directly
		xlsxRegex := regexp.MustCompile(`https://drive\.google\.com/file/d/([a-zA-Z0-9_-]{25,})/[^"']*[>"\']`)
		matches := xlsxRegex.FindAllStringSubmatch(html, -1)
		for _, m := range matches {
			id := m[1]
			if !seen[id] {
				seen[id] = true
				files = append(files, FileInfo{ID: id, Name: "Fallback Excel File.xlsx"})
			}
		}
	}

	return files
}

// DownloadFile downloads a file from a public Google Drive link.
func (c *Client) DownloadFile(fileID, destPath string) error {
	// Use the export/download URL for public files
	downloadURL := fmt.Sprintf("https://drive.google.com/uc?export=download&id=%s", fileID)

	req, _ := http.NewRequest("GET", downloadURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MocaConsult/1.0)")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	// Google may redirect to a confirmation page for large files
	if resp.StatusCode == 200 {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// Check if this is the "virus scan" confirmation page
		if strings.Contains(bodyStr, "confirm=") && strings.Contains(bodyStr, "download") {
			// Extract the confirm token
			confirmRegex := regexp.MustCompile(`confirm=([a-zA-Z0-9_-]+)`)
			match := confirmRegex.FindStringSubmatch(bodyStr)
			if len(match) >= 2 {
				confirmURL := fmt.Sprintf("https://drive.google.com/uc?export=download&confirm=%s&id=%s", match[1], fileID)
				req2, _ := http.NewRequest("GET", confirmURL, nil)
				req2.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MocaConsult/1.0)")
				// Copy cookies from first response
				for _, cookie := range resp.Cookies() {
					req2.AddCookie(cookie)
				}
				resp2, err := c.httpClient.Do(req2)
				if err != nil {
					return fmt.Errorf("confirm download: %w", err)
				}
				defer resp2.Body.Close()
				return saveToFile(resp2.Body, destPath)
			}
		}

		// Not a confirmation page — it's the actual file
		// We already read the body, write it directly
		os.MkdirAll(filepath.Dir(destPath), 0755)
		return os.WriteFile(destPath, body, 0644)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed (HTTP %d)", resp.StatusCode)
	}

	return saveToFile(resp.Body, destPath)
}

func saveToFile(reader io.Reader, destPath string) error {
	os.MkdirAll(filepath.Dir(destPath), 0755)
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, reader); err != nil {
		os.Remove(destPath)
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// SyncFolder downloads/updates .xlsx files from Drive to localDir.
// Files are named YYYY-MM-DD.xlsx based on the actual date extracted from their content.
func (c *Client) SyncFolder(localDir string) ([]string, error) {
	files, err := c.ListFiles()
	if err != nil {
		return nil, err
	}

	log.Printf("[gdrive] Found %d .xlsx file(s) in Drive folder", len(files))

	var downloaded []string
	for i, f := range files {
		// Download to a temp path first.
		tmpPath := filepath.Join(localDir, fmt.Sprintf("sync_tmp_%d.xlsx", i))

		log.Printf("[gdrive] Downloading temp file: %s (ID: %s)", f.Name, f.ID)
		if err := c.DownloadFile(f.ID, tmpPath); err != nil {
			log.Printf("[gdrive] Error downloading %s: %v", f.Name, err)
			os.Remove(tmpPath)
			continue
		}

		// Parse the downloaded file to find its true date.
		stats, err := excel.Parse(tmpPath)
		if err != nil || stats == nil {
			log.Printf("[gdrive] Error parsing downloaded file %s: %v", f.Name, err)
			os.Remove(tmpPath)
			continue
		}

		// The target date is formatted as YYYY-MM-DD.xlsx
		targetName := stats.Date + ".xlsx"
		if targetName == ".xlsx" {
			targetName = time.Now().Format("2006-01-02") + ".xlsx"
		}
		targetPath := filepath.Join(localDir, targetName)

		// Compare size: skip rename if identical to what's already on disk.
		newInfo, _ := os.Stat(tmpPath)
		oldInfo, oldErr := os.Stat(targetPath)
		if oldErr == nil && newInfo != nil && oldInfo.Size() == newInfo.Size() {
			os.Remove(tmpPath)
			log.Printf("[gdrive] %s: no change (same size %d bytes)", targetName, oldInfo.Size())
			continue
		}

		// Rename temp file to its true date name.
		if err := os.Rename(tmpPath, targetPath); err != nil {
			log.Printf("[gdrive] Error renaming to %s: %v", targetName, err)
			os.Remove(tmpPath)
			continue
		}

		log.Printf("[gdrive] Successfully synced %s", targetPath)
		downloaded = append(downloaded, targetPath)
	}

	return downloaded, nil
}

// TestConnection checks if the folder is accessible.
func (c *Client) TestConnection() error {
	files, err := c.ListFiles()
	if err != nil {
		return err
	}
	log.Printf("[gdrive] Connection OK — %d .xlsx file(s) found", len(files))
	return nil
}
