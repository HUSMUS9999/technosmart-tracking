package main

import (
	"fmt"
	"log"

	"fiber-tracker/internal/config"
	"fiber-tracker/internal/gdrive"
)

func main() {
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatal(err)
	}

	client := gdrive.New()
	client.SetFolder(cfg.GDriveFolderID, cfg.GDriveFolderName)

	downloaded, err := client.SyncFolder("/home/hus/Downloads")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Downloaded files: %v\n", downloaded)
}
