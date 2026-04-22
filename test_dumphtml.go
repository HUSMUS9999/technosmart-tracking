package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"fiber-tracker/internal/config"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatal(err)
	}

	url := fmt.Sprintf("https://drive.google.com/embeddedfolderview?id=%s", cfg.GDriveFolderID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; MocaConsult/1.0)")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	os.WriteFile("drive_test.html", body, 0644)
	fmt.Println("Wrote drive_test.html")
}
