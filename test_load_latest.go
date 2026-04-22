package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	pattern := "*.xlsx"
	folder := "/home/hus/Downloads"
	matches, _ := filepath.Glob(filepath.Join(folder, pattern))

	fmt.Printf("Matches found: %v\n", matches)

	var target string
	var latestTime int64
	for _, m := range matches {
		info, err := os.Stat(m)
		if err == nil {
			fmt.Printf("File: %s, ModTime: %d\n", m, info.ModTime().Unix())
			if info.ModTime().Unix() > latestTime {
				latestTime = info.ModTime().Unix()
				target = m
			}
		} else {
			fmt.Printf("Err on %s: %v\n", m, err)
		}
	}
	fmt.Printf("\nSelected target: %s\n", target)
}
