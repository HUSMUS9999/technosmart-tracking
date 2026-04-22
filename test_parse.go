package main

import (
	"fmt"
	"log"
	
	"fiber-tracker/internal/excel"
)

func main() {
	stats, err := excel.Parse("/home/hus/Downloads/GANTT Moka (1).xlsx")
	if err != nil {
		log.Fatalf("Parse err: %v", err)
	}
	if len(stats.AllRecords) > 0 {
		fmt.Printf("RDVDate of first record: %s\n", stats.AllRecords[0].RDVDate)
	}
	fmt.Printf("Parsed Date from stats: %s\n", stats.Date)
}
