package main

import (
	"fmt"
	"fiber-tracker/internal/excel"
)

func main() {
	stats, err := excel.Parse("/home/hus/Downloads/GANTT Moka.xlsx")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	
	fmt.Println("HAMDI Hassen from AllRecords (Tableau recap):")
	for _, r := range stats.AllRecords {
		if r.Tech == "HAMDI Hassen" {
			fmt.Printf("- State: %s, Finish: %s, Dur: %s\n", r.State, r.Finished, r.Duration)
		}
	}
}
