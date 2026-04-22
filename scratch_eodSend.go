package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"bytes"

	"fiber-tracker/internal/models"
	"fiber-tracker/internal/config"
	"fiber-tracker/internal/excel"
)

func formatEODReport(s *models.DailyStats) string {
	total := s.TotalOK + s.TotalNOK
	msg := "📋 *Moca Consult — Rapport de Fin de Journée*\n"
	msg += fmt.Sprintf("📅 %s\n", s.Date)
	msg += "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n"

	msg += "📊 *Bilan global:*\n"
	msg += fmt.Sprintf("  🔧 Total: *%d interventions*\n", total)
	msg += fmt.Sprintf("  ✅ OK: *%d*\n", s.TotalOK)
	msg += fmt.Sprintf("  ❌ NOK: *%d*\n", s.TotalNOK)
	msg += fmt.Sprintf("  📈 Taux de réussite: *%.1f%%*\n\n", s.RateOK)

	msg += "👥 *Performance par technicien:*\n"
	for _, t := range s.ByTechnician {
		techTotal := t.OK + t.NOK
		rate := float64(0)
		if techTotal > 0 {
			rate = float64(t.OK) / float64(techTotal) * 100
		}
		emoji := "🟢"
		if rate < 50 {
			emoji = "🔴"
		} else if rate < 80 {
			emoji = "🟡"
		}
		msg += fmt.Sprintf("  %s %s: %d/%d (%.0f%%)\n", emoji, t.Name, t.OK, techTotal, rate)
	}

	msg += "\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
	msg += "✅ Bonne soirée. Merci à toute l'équipe !"
	return msg
}

func main() {
	config.Load("config.json")
	stats, err := excel.Parse("/home/hus/Downloads/GANTT Moka.xlsx")
	if err != nil {
		fmt.Println("Error parsing:", err)
		return
	}

	msg := formatEODReport(stats)

	payload := map[string]string{
		"to":      "+33764474619",
		"message": msg,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", "http://127.0.0.1:9510/api/whatsapp/send", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "ft_session=supersession")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("Error sending:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		fmt.Println("SUCCESS")
	} else {
		fmt.Println("HTTP", resp.StatusCode)
	}
}
