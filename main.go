package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"fiber-tracker/internal/config"
	"fiber-tracker/internal/excel"
	"fiber-tracker/internal/models"
	"fiber-tracker/internal/scheduler"
	"fiber-tracker/internal/watcher"
	"fiber-tracker/internal/whatsapp"
	"fiber-tracker/web"
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile)

	fmt.Println("══════════════════════════════════════════")
	fmt.Println("  Technosmart — Fiber Tracker")
	fmt.Println("══════════════════════════════════════════")

	// Load config
	cfg, err := config.Load("config.json")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Config loaded (watching: %s)", cfg.WatchFolder)

	// Ensure watch folder exists
	if err := os.MkdirAll(cfg.WatchFolder, 0755); err != nil {
		log.Fatalf("Cannot create watch folder: %v", err)
	}

	// WhatsApp client (real whatsmeow)
	waClient, err := whatsapp.New("whatsapp.db")
	if err != nil {
		log.Printf("WhatsApp init error: %v (continuing without WhatsApp)", err)
		waClient = nil
	} else {
		if waClient.IsConnected() {
			log.Printf("WhatsApp: connected as %s", waClient.PhoneNumber())
		} else {
			log.Println("WhatsApp: initialized — scan QR from Settings page to link")
		}
	}

	// Web server
	srv := web.New(cfg.WebPort)
	srv.SetWhatsAppClient(waClient)

	// File watcher
	fw := watcher.New(cfg.WatchFolder, func(path string) {
		log.Printf("Processing new file: %s", filepath.Base(path))
		stats, err := excel.Parse(path)
		if err != nil {
			log.Printf("Error parsing %s: %v", filepath.Base(path), err)
			return
		}
		srv.UpdateStats(stats)
		log.Printf("Stats updated: %d OK, %d NOK (%.1f%%)", stats.TotalOK, stats.TotalNOK, stats.RateOK)

		// Send NOK alert
		if stats.TotalNOK > 0 && waClient != nil {
			msg := formatNOKAlert(stats)
			waClient.SendMessage(cfg.MyNumber, msg)
			srv.AddNotification("nok_alert", cfg.MyNumber, msg, true)
		}

		// Send stats
		if waClient != nil {
			msg := formatStatsMessage(stats)
			waClient.SendMessage(cfg.MyNumber, msg)
			srv.AddNotification("stats", cfg.MyNumber, msg, true)
		}
	})
	fw.MarkExisting()

	// Try to load the latest existing file
	loadLatest(cfg, srv)

	// Set upload callback
	srv.SetOnNewFile(func(path string) {
		stats, err := excel.Parse(path)
		if err == nil {
			srv.UpdateStats(stats)
		}
	})

	// Start all services
	if err := fw.Start(); err != nil {
		log.Fatalf("Watcher error: %v", err)
	}

	if err := srv.Start(); err != nil {
		log.Fatalf("Web server error: %v", err)
	}

	// Scheduler
	sched, err := scheduler.New(cfg.Timezone)
	if err != nil {
		log.Fatalf("Scheduler error: %v", err)
	}

	// Morning message
	morningSpec := fmt.Sprintf("0 %d %d * * *", cfg.MorningMinute, cfg.MorningHour)
	sched.AddJob("morning", morningSpec, func() {
		if waClient == nil {
			return
		}
		msg := formatMorningMessage()
		for tech, number := range cfg.Technicians {
			waClient.SendMessage(number, msg)
			srv.AddNotification("morning", tech, msg, true)
		}
	})

	// Stats every N hours
	for hour := 9; hour < 18; hour += cfg.StatsIntervalH {
		h := hour
		spec := fmt.Sprintf("0 0 %d * * *", h)
		sched.AddJob(fmt.Sprintf("stats_%d", h), spec, func() {
			stats := loadLatest(cfg, srv)
			if stats != nil && waClient != nil {
				msg := formatStatsMessage(stats)
				waClient.SendMessage(cfg.MyNumber, msg)
				srv.AddNotification("stats", cfg.MyNumber, msg, true)
			}
		})
	}

	// End-of-day report + thank you (configurable hour)
	eodSpec := fmt.Sprintf("0 0 %d * * *", cfg.EODHour)
	sched.AddJob("eod_report", eodSpec, func() {
		stats := loadLatest(cfg, srv)
		if stats == nil || waClient == nil {
			return
		}
		// Send final report to owner
		report := formatEODReport(stats)
		waClient.SendMessage(cfg.MyNumber, report)
		srv.AddNotification("eod_report", cfg.MyNumber, report, true)

		// Send thank-you to each technician
		for _, t := range stats.ByTechnician {
			phone, ok := cfg.Technicians[t.Name]
			if !ok || phone == "" {
				continue
			}
			msg := formatThankYouMessage(t)
			waClient.SendMessage(phone, msg)
			srv.AddNotification("eod_thanks", t.Name, msg, true)
		}
	})

	sched.Start()

	fmt.Println()
	fmt.Printf("  🌐  Dashboard: http://localhost:%d\n", cfg.WebPort)
	fmt.Printf("  📁  Watching:   %s\n", cfg.WatchFolder)
	fmt.Println("  Press Ctrl+C to stop")
	fmt.Println()

	// Wait for shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down...")
	fw.Stop()
	sched.Stop()
	if waClient != nil {
		waClient.Disconnect()
	}
}

func loadLatest(cfg *config.Config, srv *web.Server) *models.DailyStats {
	matches, _ := filepath.Glob(filepath.Join(cfg.WatchFolder, "*.xlsx"))
	if len(matches) == 0 {
		return nil
	}

	var latest string
	var latestTime int64
	for _, m := range matches {
		info, err := os.Stat(m)
		if err == nil && info.ModTime().Unix() > latestTime {
			latestTime = info.ModTime().Unix()
			latest = m
		}
	}

	if latest == "" {
		return nil
	}

	stats, err := excel.Parse(latest)
	if err != nil {
		log.Printf("Error parsing latest file: %v", err)
		return nil
	}
	srv.UpdateStats(stats)
	return stats
}

func formatStatsMessage(s *models.DailyStats) string {
	msg := fmt.Sprintf("📊 *Stats du jour* (%s)\n\n", s.Date)
	msg += fmt.Sprintf("✅ OK: %d\n", s.TotalOK)
	msg += fmt.Sprintf("❌ NOK: %d\n", s.TotalNOK)
	msg += fmt.Sprintf("📈 Taux OK: %.1f%%\n\n", s.RateOK)
	msg += "*Par technicien:*\n"
	for _, t := range s.ByTechnician {
		msg += fmt.Sprintf("- %s: %d OK / %d NOK\n", t.Name, t.OK, t.NOK)
	}
	return msg
}

func formatNOKAlert(s *models.DailyStats) string {
	msg := fmt.Sprintf("🚨 *%d opération(s) NOK détectées*\n\n", s.TotalNOK)
	limit := 5
	if len(s.NOKRecords) < limit {
		limit = len(s.NOKRecords)
	}
	for _, r := range s.NOKRecords[:limit] {
		msg += fmt.Sprintf("• %s - %s\n", r.Tech, r.Type)
		if r.Reason != "" {
			msg += fmt.Sprintf("  Raison: %s\n", r.Reason)
		}
	}
	if len(s.NOKRecords) > 5 {
		msg += fmt.Sprintf("\n...et %d autres", len(s.NOKRecords)-5)
	}
	return msg
}

func formatMorningMessage() string {
	return "🌅 *Bonjour équipe !* 💪\n\nBonne journée ! N'oubliez pas de:\n✅ Scanner les équipements\n✅ Mettre à jour le tableau\n✅ Signaler tout problème\n\nAllez, on attaque ! 🔧"
}

func formatEODReport(s *models.DailyStats) string {
	msg := fmt.Sprintf("📋 *Rapport de fin de journée* (%s)\n\n", s.Date)
	msg += fmt.Sprintf("📊 *Résumé global:*\n")
	msg += fmt.Sprintf("  Total: %d interventions\n", s.TotalOK+s.TotalNOK)
	msg += fmt.Sprintf("  ✅ OK: %d\n", s.TotalOK)
	msg += fmt.Sprintf("  ❌ NOK: %d\n", s.TotalNOK)
	msg += fmt.Sprintf("  📈 Taux de réussite: *%.1f%%*\n\n", s.RateOK)
	msg += "👥 *Performance par technicien:*\n"
	for _, t := range s.ByTechnician {
		total := t.OK + t.NOK
		rate := float64(0)
		if total > 0 {
			rate = float64(t.OK) / float64(total) * 100
		}
		emoji := "🟢"
		if rate < 50 {
			emoji = "🔴"
		} else if rate < 80 {
			emoji = "🟡"
		}
		msg += fmt.Sprintf("%s %s: %d/%d (%.0f%%)\n", emoji, t.Name, t.OK, total, rate)
	}
	msg += "\n✅ Fin de journée. Merci à toute l'équipe !"
	return msg
}

func formatThankYouMessage(t models.TechStats) string {
	total := t.OK + t.NOK
	rate := float64(0)
	if total > 0 {
		rate = float64(t.OK) / float64(total) * 100
	}

	msg := fmt.Sprintf("👋 *Bonsoir %s !*\n\n", t.Name)
	msg += "Merci pour ton travail aujourd'hui ! 🙏\n\n"
	msg += fmt.Sprintf("📊 *Ton bilan du jour:*\n")
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
	msg += "\n\nBonne soirée ! 🌙"
	return msg
}
