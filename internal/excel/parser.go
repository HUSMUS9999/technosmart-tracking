package excel

import (
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"fiber-tracker/internal/models"

	"github.com/xuri/excelize/v2"
)

// Parse reads the Excel file and returns full DailyStats.
func Parse(filePath string) (*models.DailyStats, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("excel: cannot open %s: %w", filePath, err)
	}
	defer f.Close()

	stats := &models.DailyStats{
		Date:               time.Now().Format("2006-01-02"),
		SourceFile:         filePath,
		UpdatedAt:          time.Now(),
		FailuresByCategory: make(map[string]int),
		FailuresByType:     make(map[string]int),
		ByDepartment:       make(map[string]int),
		ByZone:             make(map[string]int),
	}

	// Parse GANTT sheet for summary + per-tech hourly view
	parseGanttSheet(f, stats)

	// Parse "Tableau récap" for detailed records
	parseRecapSheet(f, stats)

	// Fallback: if GANTT sheet was missing, compute totals from AllRecords
	if stats.Total == 0 && len(stats.AllRecords) > 0 {
		computeSummaryFromRecords(stats)
	}

	return stats, nil
}

// parseGanttSheet extracts the top-level RACC/SAV stats and per-tech GANTT slots
func parseGanttSheet(f *excelize.File, stats *models.DailyStats) {
	sheetName := ""
	for _, s := range f.GetSheetList() {
		if strings.EqualFold(s, "GANTT") {
			sheetName = s
			break
		}
	}
	if sheetName == "" {
		return
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		log.Printf("[excel] error reading GANTT sheet: %v", err)
		return
	}

	// Row indices based on the Excel structure analysis:
	// Row 2 (idx 1): RACC summary
	// Row 3 (idx 2): SAV summary
	// Row 5+ (idx 4+): column headers then per-tech data
	for i, row := range rows {
		if len(row) < 5 {
			continue
		}

		// Detect summary rows: "RACC" or "SAV" in column B
		label := strings.TrimSpace(getCell(row, 1))
		if label == "RACC" || label == "SAV" {
			ok := parseInt(getCell(row, 3))
			nok := parseInt(getCell(row, 4))
			inProgress := parseInt(getCell(row, 5))
			atRisk := parseInt(getCell(row, 6))
			remaining := parseInt(getCell(row, 7))
			pdc := parseInt(getCell(row, 8))

			if label == "RACC" {
				stats.RACC_OK = ok
				stats.RACC_NOK = nok
				if ok+nok > 0 {
					stats.RACC_Rate = float64(ok) / float64(ok+nok)
				}
			} else {
				stats.SAV_OK = ok
				stats.SAV_NOK = nok
				if ok+nok > 0 {
					stats.SAV_Rate = float64(ok) / float64(ok+nok)
				}
			}
			stats.InProgress += inProgress
			stats.AtRisk += atRisk
			stats.Remaining += remaining
			stats.PDC += pdc
			continue
		}

		// Detect technician data rows (have a sector number in col B)
		sector := strings.TrimSpace(getCell(row, 1))
		techName := strings.TrimSpace(getCell(row, 2))
		if sector == "" || techName == "" || sector == "Secteur" {
			continue
		}
		// Verify sector is numeric (department number)
		if _, err := strconv.Atoi(sector); err != nil {
			continue
		}

		// Collect slots from columns D onwards (time slots)
		var slots []string
		for j := 3; j < len(row) && j < 13; j++ {
			slots = append(slots, strings.TrimSpace(row[j]))
		}

		// Parse rate and fill from "Taux de transfo" / "Taux de remplissage" columns
		rate := parseFloat(getCell(row, 16))   // Column Q
		fill := parseFloat(getCell(row, 18))   // Column S

		ganttSlot := models.GanttSlot{
			Tech:     techName,
			Sector:   sector,
			Slots:    slots,
			Rate:     rate,
			FillRate: fill,
		}
		stats.GanttData = append(stats.GanttData, ganttSlot)

		// Also build TechStats from GANTT slots
		techOK, techNOK := 0, 0
		raccOK, raccNOK, savOK, savNOK := 0, 0, 0, 0
		for _, slot := range slots {
			upper := strings.ToUpper(slot)
			switch {
			case strings.Contains(upper, "OK RACC"):
				techOK++
				raccOK++
			case strings.Contains(upper, "NOK RACC"):
				techNOK++
				raccNOK++
			case strings.Contains(upper, "OK SAV"):
				techOK++
				savOK++
			case strings.Contains(upper, "NOK SAV"):
				techNOK++
				savNOK++
			}
		}

		found := false
		for idx := range stats.ByTechnician {
			if stats.ByTechnician[idx].Name == techName {
				stats.ByTechnician[idx].OK += techOK
				stats.ByTechnician[idx].NOK += techNOK
				stats.ByTechnician[idx].Total += techOK + techNOK
				stats.ByTechnician[idx].RACC_OK += raccOK
				stats.ByTechnician[idx].RACC_NOK += raccNOK
				stats.ByTechnician[idx].SAV_OK += savOK
				stats.ByTechnician[idx].SAV_NOK += savNOK
				stats.ByTechnician[idx].Sector = sector
				if stats.ByTechnician[idx].Total > 0 {
					stats.ByTechnician[idx].RateOK = float64(stats.ByTechnician[idx].OK) / float64(stats.ByTechnician[idx].Total)
				}
				found = true
				break
			}
		}
		if !found {
			ts := models.TechStats{
				Name:     techName,
				OK:       techOK,
				NOK:      techNOK,
				Total:    techOK + techNOK,
				RACC_OK:  raccOK,
				RACC_NOK: raccNOK,
				SAV_OK:   savOK,
				SAV_NOK:  savNOK,
				Sector:   sector,
			}
			if ts.Total > 0 {
				ts.RateOK = float64(ts.OK) / float64(ts.Total)
			}
			stats.ByTechnician = append(stats.ByTechnician, ts)
		}

		_ = i // suppress unused
	}

	stats.TotalOK = stats.RACC_OK + stats.SAV_OK
	stats.TotalNOK = stats.RACC_NOK + stats.SAV_NOK
	stats.Total = stats.TotalOK + stats.TotalNOK
	if stats.Total > 0 {
		stats.RateOK = float64(stats.TotalOK) / float64(stats.Total)
	}
}

// parseRecapSheet extracts from "Tableau récap" all detailed intervention records
func parseRecapSheet(f *excelize.File, stats *models.DailyStats) {
	sheetName := ""
	for _, s := range f.GetSheetList() {
		lower := strings.ToLower(s)
		if strings.Contains(lower, "tableau") || strings.Contains(lower, "récap") || strings.Contains(lower, "recap") {
			sheetName = s
			break
		}
	}
	if sheetName == "" {
		return
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		log.Printf("[excel] error reading recap sheet: %v", err)
		return
	}

	// Find header row and map column indices
	headerMap := map[string]int{}
	headerRow := -1
	for i, row := range rows {
		for j, cell := range row {
			lower := strings.ToLower(strings.TrimSpace(cell))
			if lower == "jeton de commande" || lower == "jeton" {
				headerRow = i
				for jj, hcell := range row {
					headerMap[strings.ToLower(strings.TrimSpace(hcell))] = jj
				}
				_ = j
				break
			}
		}
		if headerRow >= 0 {
			break
		}
	}
	if headerRow < 0 {
		return
	}

	getCol := func(row []string, name string) string {
		if idx, ok := headerMap[name]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	for i := headerRow + 1; i < len(rows); i++ {
		row := rows[i]
		if len(row) < 5 {
			continue
		}

		ref := getCol(row, "jeton de commande")
		if ref == "" {
			continue
		}

		state := strings.TrimSpace(getCol(row, "état de l'intervention"))
		tech := getCol(row, "tech sans binome")
		interType := getCol(row, "type d'inter")
		dept := getCol(row, "département")
		pm := getCol(row, "pm")
		duration := getCol(row, "durée d'intervention")
		zone := getCol(row, "zone")
		zoneType := getCol(row, "type de zone")
		delay := getCol(row, "retard/avance")
		delayType := getCol(row, "type retard/avance")
		ptoStatus := getCol(row, "statut pto")
		photoCtrl := getCol(row, "commentaire ctrl photo")
		failCode := getCol(row, "code clôture si échec")
		failDiag := getCol(row, "diagnostic échec")
		failCat := getCol(row, "catégorie d'échec")
		startTime := getCol(row, "début d'intervention")
		endTime := getCol(row, "fin d'intervention")
		rdvDate := getCol(row, "date du rendez-vous")
		finished := getCol(row, "terminée")

		record := models.InterventionRecord{
			Reference:  ref,
			StartTime:  formatTime(startTime),
			EndTime:    formatTime(endTime),
			Finished:   finished,
			State:      state,
			Department: dept,
			PM:         pm,
			Tech:       tech,
			Type:       interType,
			Duration:   cleanDuration(duration),
			RDVDate:    formatTime(rdvDate),
			Zone:       zone,
			ZoneType:   zoneType,
			Delay:      delay,
			DelayType:  delayType,
			PTOStatus:  ptoStatus,
			PhotoCtrl:  photoCtrl,
			FailCode:   failCode,
			FailDiag:   failDiag,
			FailCat:    failCat,
		}
		stats.AllRecords = append(stats.AllRecords, record)

		// Track department
		if dept != "" && dept != "--" && dept != "-" {
			stats.ByDepartment[dept]++
		}

		// Track zone
		if zone != "" && zone != "--" && zone != "-" {
			stats.ByZone[zone]++
		}

		// Collect NOK records with enriched detail
		if strings.EqualFold(state, "NOK") {
			reason := failCode
			if reason == "" || reason == "OK" || reason == "--" || reason == "-" {
				reason = failDiag
			}
			if reason == "" || reason == "--" || reason == "-" {
				reason = "Non spécifié"
			}

			nok := models.NOKRecord{
				Tech:       tech,
				Type:       interType,
				Reason:     reason,
				Reference:  ref,
				Department: dept,
				PM:         pm,
				Duration:   cleanDuration(duration),
				FailCode:   failCode,
				Category:   failCat,
				StartTime:  formatTime(startTime),
				EndTime:    formatTime(endTime),
			}
			stats.NOKRecords = append(stats.NOKRecords, nok)

			// Track failure categories
			if failCat != "" && failCat != "--" && failCat != "-" {
				stats.FailuresByCategory[failCat]++
			} else {
				stats.FailuresByCategory["Non catégorisé"]++
			}

			// Track failure types
			if interType != "" {
				stats.FailuresByType[interType]++
			}
		}
	}
}

// Helper functions
func getCell(row []string, idx int) string {
	if idx < len(row) {
		return strings.TrimSpace(row[idx])
	}
	return ""
}

func parseInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	val, err := strconv.Atoi(s)
	if err != nil {
		// Try float conversion
		fval, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return 0
		}
		return int(math.Round(fval))
	}
	return val
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return val
}

func formatTime(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" || s == "-" {
		return ""
	}
	// Try parsing ISO datetime
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.000",
		"01/02/2006 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s[:min(len(s), len(layout))]); err == nil {
			return t.Format("15:04")
		}
	}
	// If it looks like it contains a time, extract it
	if len(s) >= 19 {
		// "2026-03-30 09:09:59.616" → "09:09"
		parts := strings.Fields(s)
		if len(parts) >= 2 {
			tparts := strings.Split(parts[1], ":")
			if len(tparts) >= 2 {
				return tparts[0] + ":" + tparts[1]
			}
		}
	}
	return s
}

func cleanDuration(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" || s == "-" {
		return ""
	}
	// "0 days 01:54:00" → "01:54"
	s = strings.Replace(s, "0 days ", "", 1)
	parts := strings.Split(s, ":")
	if len(parts) >= 2 {
		return parts[0] + ":" + parts[1]
	}
	return s
}

// computeSummaryFromRecords builds TotalOK/TotalNOK/ByTechnician from AllRecords
// when no GANTT sheet is available.
func computeSummaryFromRecords(stats *models.DailyStats) {
	techMap := map[string]*models.TechStats{}

	for _, r := range stats.AllRecords {
		upper := strings.ToUpper(r.State)
		isOK := upper == "OK"
		isNOK := upper == "NOK"

		if isOK {
			stats.TotalOK++
		}
		if isNOK {
			stats.TotalNOK++
		}
		stats.Total++

		// By type
		typeUpper := strings.ToUpper(r.Type)
		if isOK {
			if typeUpper == "RACC" {
				stats.RACC_OK++
			} else {
				stats.SAV_OK++
			}
		}
		if isNOK {
			if typeUpper == "RACC" {
				stats.RACC_NOK++
			} else {
				stats.SAV_NOK++
			}
		}

		// By technician
		if r.Tech != "" {
			ts, ok := techMap[r.Tech]
			if !ok {
				ts = &models.TechStats{Name: r.Tech}
				techMap[r.Tech] = ts
			}
			if isOK {
				ts.OK++
				if typeUpper == "RACC" {
					ts.RACC_OK++
				} else {
					ts.SAV_OK++
				}
			}
			if isNOK {
				ts.NOK++
				if typeUpper == "RACC" {
					ts.RACC_NOK++
				} else {
					ts.SAV_NOK++
				}
			}
			ts.Total++
		}
	}

	// Compute rates
	if stats.Total > 0 {
		stats.RateOK = float64(stats.TotalOK) / float64(stats.Total)
	}
	if racc := stats.RACC_OK + stats.RACC_NOK; racc > 0 {
		stats.RACC_Rate = float64(stats.RACC_OK) / float64(racc)
	}
	if sav := stats.SAV_OK + stats.SAV_NOK; sav > 0 {
		stats.SAV_Rate = float64(stats.SAV_OK) / float64(sav)
	}

	// Build ByTechnician slice
	if len(stats.ByTechnician) == 0 {
		for _, ts := range techMap {
			if ts.Total > 0 {
				ts.RateOK = float64(ts.OK) / float64(ts.Total)
			}
			stats.ByTechnician = append(stats.ByTechnician, *ts)
		}
	}
}
