package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"

	"fiber-tracker/internal/config"
)

func main() {
	cfg, _ := config.Load("config.json")
	url := fmt.Sprintf("https://drive.google.com/embeddedfolderview?id=%s", cfg.GDriveFolderID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, _ := http.DefaultClient.Do(req)
	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	xlsxRegex := regexp.MustCompile(`([a-zA-Z0-9_-]{25,45})[^a-zA-Z0-9_-][^.]{0,200}?([A-Za-z0-9 _().-]+\.xlsx)`)
	matches := xlsxRegex.FindAllStringSubmatch(html, -1)
	for _, m := range matches {
		fmt.Printf("Fallback Match -> ID: %s, Name: %s\n", m[1], m[2])
	}
}
