package main

import (
	"fmt"
	"fiber-tracker/internal/config"
	"fiber-tracker/internal/excel"
	"strings"
)

func main() {
	config.Load("/home/hus/Downloads/fiber-tracker/config.json")
	cfg := config.Get()
	stats, err := excel.Parse("/home/hus/Downloads/GANTT Moka.xlsx")
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	
	for _, t := range stats.ByTechnician {
		if t.Name == "HAMDI Hassen" {
			total := t.OK + t.NOK
			rate := float64(0)
			if total > 0 {
				rate = float64(t.OK) / float64(total) * 100
			}
			msg := strings.ReplaceAll(cfg.MsgEODThanks, "{prenom}", t.Name)
			msg += "\n\n"
			msg += "📊 *Ton bilan du jour:*\n"
			msg += fmt.Sprintf("  🔧 Interventions: %d\n", total)
			msg += fmt.Sprintf("  ✅ Réussies: %d\n", t.OK)
			if t.NOK > 0 {
				msg += fmt.Sprintf("  ❌ Échecs: %d\n", t.NOK)
			}
			msg += fmt.Sprintf("  📈 Taux: *%.0f%%*\n\n", rate)

			if rate >= 90 {
				msg += "🌟 Excellent travail ! Continue comme ça !"
			} else if rate >= 70 {
				msg += "👍 Bon travail ! On peut encore s'améliorer !"
			} else {
				msg += "💪 Demain sera meilleur ! On lâche rien !"
			}
			fmt.Println(msg)
		}
	}
}
