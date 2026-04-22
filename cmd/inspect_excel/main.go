package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"
)

func main() {
	f, err := excelize.OpenFile("GANTT Moka.xlsx")
	if err != nil { panic(err) }
	defer f.Close()

	for _, s := range f.GetSheetList() {
		lower := strings.ToLower(s)
		if !strings.Contains(lower, "tableau") && !strings.Contains(lower, "recap") && !strings.Contains(lower, "récap") {
			continue
		}
		rows, _ := f.GetRows(s)
		headerRow := -1
		headerMap := map[string]int{}
		for i, row := range rows {
			for _, cell := range row {
				if strings.EqualFold(strings.TrimSpace(cell), "jeton de commande") {
					headerRow = i
					for j, h := range row { headerMap[strings.ToLower(strings.TrimSpace(h))] = j }
					break
				}
			}
			if headerRow >= 0 { break }
		}
		if headerRow < 0 { continue }

		getCol := func(row []string, name string) string {
			if idx, ok := headerMap[name]; ok && idx < len(row) { return strings.TrimSpace(row[idx]) }
			return ""
		}

		stateVals   := map[string]int{}
		techVals    := map[string]int{}
		termVals    := map[string]int{}
		startVals   := map[string]int{} // "" vs non-empty
		relanceVals := map[string]int{}

		for i := headerRow + 1; i < len(rows); i++ {
			row := rows[i]
			if getCol(row, "jeton de commande") == "" { continue }

			state   := getCol(row, "état de l'intervention")
			tech    := getCol(row, "tech sans binome")
			term    := getCol(row, "terminée")
			start   := getCol(row, "début d'intervention")
			relance := getCol(row, "statut relance démarrage")

			stateVals[state]++
			techVals[tech]++
			termVals[term]++
			if start == "" { startVals["(vide)"]++ } else { startVals["rempli"]++ }
			relanceVals[relance]++
		}

		printMap := func(label string, m map[string]int) {
			fmt.Printf("\n=== %s ===\n", label)
			keys := make([]string, 0, len(m))
			for k := range m { keys = append(keys, k) }
			sort.Strings(keys)
			for _, k := range keys {
				disp := k
				if disp == "" { disp = "(vide)" }
				fmt.Printf("  %-40s  %d lignes\n", disp, m[k])
			}
		}

		printMap("État de l'intervention  — toutes les valeurs", stateVals)
		printMap("Terminée                — toutes les valeurs", termVals)
		printMap("Début d'intervention    — vide / rempli", startVals)
		printMap("Statut relance démarrage", relanceVals)

		// Show a few sample tech names  
		fmt.Println("\n=== tech sans binome — 10 exemples ===")
		shown := 0
		for t, n := range techVals {
			if shown >= 10 { break }
			fmt.Printf("  %-30s  %d lignes\n", t, n)
			shown++
		}

		// Cross-tab: start empty × state
		fmt.Println("\n=== Croisement: StartTime vide × État ===")
		cross := map[string]int{}
		for i := headerRow + 1; i < len(rows); i++ {
			row := rows[i]
			if getCol(row, "jeton de commande") == "" { continue }
			start := getCol(row, "début d'intervention")
			state := getCol(row, "état de l'intervention")
			term  := getCol(row, "terminée")
			key := fmt.Sprintf("start=%-7s | état=%-20s | terminée=%s", 
				func() string { if start == "" { return "VIDE" }; return "REMPLI" }(),
				func() string { if state == "" { return "(vide)" }; return state }(),
				func() string { if term == "" { return "(vide)" }; return term }(),
			)
			cross[key]++
		}
		ckeys := make([]string, 0, len(cross))
		for k := range cross { ckeys = append(ckeys, k) }
		sort.Strings(ckeys)
		for _, k := range ckeys { fmt.Printf("  %s  →  %d\n", k, cross[k]) }
	}
}
