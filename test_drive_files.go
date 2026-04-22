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
	if !client.IsConfigured() {
		log.Fatal("Drive not configured")
	}

	files, err := client.ListFiles()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Total files in drive folder (%s): %d\n", client.FolderID(), len(files))
	for _, f := range files {
		fmt.Printf("- %s (ID: %s)\n", f.Name, f.ID)
	}
}
