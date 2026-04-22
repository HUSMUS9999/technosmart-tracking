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
	
	for _, t := range stats.ByTechnician {
		if t.Name == "HAMDI Hassen" {
			fmt.Printf("HAMDI Hassen from stats: OK=%d, NOK=%d, Total=%d\n", t.OK, t.NOK, t.Total)
		}
	}
}
