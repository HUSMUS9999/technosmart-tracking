package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/xuri/excelize/v2"
)

func main() {
	f := excelize.NewFile()
	defer f.Close()

	sheet := "Tableau récap"
	idx, _ := f.NewSheet(sheet)
	f.SetActiveSheet(idx)
	// Remove default Sheet1
	f.DeleteSheet("Sheet1")

	// Headers matching the GANTT Moka format
	headers := []string{
		"Jeton de commande",
		"Début d'intervention",
		"Fin d'intervention",
		"Intervention terminée",
		"État de l'intervention",
		"Type d'inter",
		"Durée d'intervention",
		"tech sans binome",
		"Département",
		"Zone",
		"Type retard/avance",
		"Code clôture si échec",
		"Catégorie si échec",
	}

	for i, h := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetCellValue(sheet, fmt.Sprintf("%s1", col), h)
	}

	// Different technicians for this test file
	techs := []string{
		"DUPONT Pierre", "MARTIN Lucas", "BERNARD Sarah",
		"THOMAS Julien", "ROBERT Emma", "DURAND Hugo",
	}

	types := []string{"RACC", "SAV"}
	depts := []string{"75", "92", "93", "94", "78"}
	zones := []string{"Zone A", "Zone B", "Zone C"}
	retards := []string{"RAS", "Retard 10-30 min", "Retard 30 min - 1h", "Avance 10-30 min"}
	failCodes := []string{"", "", "", "", "1303 - Abonné doit réaliser une action", "1201 - Pas d'accès"}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	date := "01/04/2026"
	numRows := 120

	for row := 2; row <= numRows+1; row++ {
		tech := techs[rng.Intn(len(techs))]
		typ := types[rng.Intn(len(types))]
		dept := depts[rng.Intn(len(depts))]
		zone := zones[rng.Intn(len(zones))]
		retard := retards[rng.Intn(len(retards))]
		failCode := failCodes[rng.Intn(len(failCodes))]

		// Random state
		state := "OK"
		if failCode != "" {
			state = "NOK"
		} else if rng.Float64() < 0.15 {
			state = "NOK"
			failCode = "9999 - Autre"
		}

		// Times
		startH := 7 + rng.Intn(10)
		startM := rng.Intn(60)
		dur := 15 + rng.Intn(120)
		endH := startH + dur/60
		endM := startM + dur%60
		if endM >= 60 {
			endH++
			endM -= 60
		}

		ref := 22700000 + rng.Intn(100000)
		durStr := fmt.Sprintf("%02d:%02d", dur/60, dur%60)

		vals := []interface{}{
			ref,
			fmt.Sprintf("%s %02d:%02d", date, startH, startM),
			fmt.Sprintf("%s %02d:%02d", date, endH, endM),
			"Oui",
			state,
			typ,
			durStr,
			tech,
			dept,
			zone,
			retard,
			failCode,
			"",
		}

		for i, v := range vals {
			col, _ := excelize.ColumnNumberToName(i + 1)
			f.SetCellValue(sheet, fmt.Sprintf("%s%d", col, row), v)
		}
	}

	outPath := os.Args[1]
	if err := f.SaveAs(outPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Created %s with %d rows\n", outPath, numRows)
}
