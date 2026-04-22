package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"fiber-tracker/internal/auth"
	"fiber-tracker/internal/config"
	"fiber-tracker/internal/db"
	"fiber-tracker/internal/excel"
	"fiber-tracker/internal/gdrive"
	"fiber-tracker/internal/models"
	"fiber-tracker/internal/scheduler"
	"fiber-tracker/internal/smtp"
	"fiber-tracker/internal/watcher"
	"fiber-tracker/internal/whatsapp"
	"fiber-tracker/web"
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile)

	fmt.Println("══════════════════════════════════════════")
	fmt.Println("  Moca Consult — Fiber Tracker")
	fmt.Println("══════════════════════════════════════════")

	// Load config
	configPath := "config/config.json"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Fallback to local config.json if config directory doesn't exist (e.g. outside docker)
		configPath = "config.json"
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Config loaded (watching: %s)", cfg.WatchFolder)

	// Ensure watch folder exists
	if err := os.MkdirAll(cfg.WatchFolder, 0750); err != nil {
		log.Fatalf("Cannot create watch folder: %v", err)
	}

	// Initialize GORM Database
	if err := db.InitDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}

	// Migrate technicians to DB if any exist in config.json
	dbTechs := db.GetTechniciansMap()
	if len(dbTechs) == 0 && len(cfg.Technicians) > 0 {
		log.Printf("[db] Migrating %d technicians from config to DB", len(cfg.Technicians))
		for name, phone := range cfg.Technicians {
			db.EnsureTechnician(name, phone)
		}
	}
	
	// Ensure config.Technicians holds the exact true state from DB 
	cfg.Technicians = db.GetTechniciansMap()

	// ---- Authentication (Postgres) ----
	dbHost := os.Getenv("DB_HOST")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	dbPort := os.Getenv("DB_PORT")

	if dbHost == "" || dbUser == "" || dbPass == "" || dbName == "" || dbPort == "" {
		log.Fatal("FATAL: Database environment variables (DB_HOST, DB_USER, DB_PASSWORD, DB_NAME, DB_PORT) must all be set")
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", dbHost, dbPort, dbUser, dbPass, dbName)
	authStore, err := auth.NewStore(connStr)
	if err != nil {
		log.Fatalf("Auth store Postgres connection error: %v", err)
	}
	defer authStore.Close()

	// ---- Admin bootstrap / password sync ----
	// adminEmail and adminPass are always read from config/env so they can be
	// used for both first-boot seeding AND live password rotation.
	adminEmail := cfg.AdminEmail
	if adminEmail == "" {
		adminEmail = os.Getenv("ADMIN_EMAIL")
	}
	adminPass := cfg.AdminPassword
	if adminPass == "" {
		adminPass = os.Getenv("ADMIN_PASSWORD")
	}

	if !authStore.HasUsers() {
		// First boot: no users in DB — seed the admin account.
		if adminEmail == "" || adminPass == "" {
			log.Println("Warning: no ADMIN_EMAIL/ADMIN_PASSWORD set — skipping default admin creation")
		} else {
			_, err := authStore.CreateUser(adminEmail, "Admin", adminPass, "admin")
			if err != nil {
				log.Printf("Warning: could not create admin user: %v", err)
			} else {
				log.Printf("[auth] Default admin created: %s", adminEmail)
			}
		}
	} else if adminEmail != "" && adminPass != "" {
		// Users already exist — treat ADMIN_PASSWORD as a live password override.
		// This lets the operator rotate the password by changing the env var and restarting.
		if err := authStore.UpdatePassword(adminEmail, adminPass); err != nil {
			log.Printf("[auth] Could not sync password for %s: %v (user may not exist locally)", adminEmail, err)
		}
		// Also push the new password to Zitadel so the primary auth path stays in sync.
		go syncAdminPasswordToZitadel(cfg, adminEmail, adminPass)
	}

	// ---- SMTP Mailer ----
	mailer := smtp.New(smtp.Config{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
	})

	// ---- Google Drive (public link) ----
	driveClient := gdrive.New()
	if cfg.GDriveFolderID != "" {
		driveClient.SetFolder(cfg.GDriveFolderID, cfg.GDriveFolderName)
		log.Printf("Google Drive: folder configured (%s)", cfg.GDriveFolderName)
	} else {
		log.Println("Google Drive: not configured (paste folder link in Settings)")
	}

	// WhatsApp client (whatsmeow backed by Postgres)
	waClient, err := whatsapp.New(connStr)
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
	srv.SetAuthStore(authStore)
	srv.SetDriveClient(driveClient)
	srv.SetMailer(mailer)


	// File watcher
	fw := watcher.New(cfg.WatchFolder, func(path string) {
		log.Printf("Processing new file: %s", filepath.Base(path))
		stats, err := excel.Parse(path)
		if err != nil {
			log.Printf("Error parsing %s: %v", filepath.Base(path), err)
			return
		}
		srv.UpdateStats(stats)
		syncTechniciansFromStats(stats)
		log.Printf("Stats updated: %d OK, %d NOK (%.1f%%)", stats.TotalOK, stats.TotalNOK, stats.RateOK)

		// After every file update, check for unstarted interventions whose RDV time has passed.
		go checkLateStarts(config.Get(), stats, waClient, srv)
	})
	fw.MarkExisting()

	// Try to load the latest existing file
	if latestStats := loadLatest(cfg, srv); latestStats != nil {
		syncTechniciansFromStats(latestStats)
	}

	// Set upload callback
	srv.SetOnNewFile(func(path string) {
		stats, err := excel.Parse(path)
		if err == nil {
			srv.UpdateStats(stats)
			syncTechniciansFromStats(stats)
		}
	})

	// Set tech sync callback for UI file selection
	srv.SetOnSyncTechs(func(stats *models.DailyStats) {
		syncTechniciansFromStats(stats)
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

	// Define job functions as named closures so they can be registered for testing
	morningJob := func() {
		if waClient == nil {
			return
		}
		// Re-read config each time so updated templates/technicians are used
		c := config.Get()
		for tech, number := range c.Technicians {
			if number == "" {
				srv.AddNotification("warning", "Système", fmt.Sprintf("⚠️ Le technicien %s n'a pas de numéro, il ne recevra aucun message.", tech), false)
				continue
			}
			// Skip supervisor — they don't get morning greetings
			if normalizePhone(number) == normalizePhone(c.MyNumber) {
				continue
			}
			msg := formatMorningMessage(c, tech)
			if err := waClient.SendMessage(number, msg); err != nil {
				log.Printf("[morning] Failed to send to %s: %v", tech, err)
				srv.AddNotification("morning", tech, msg, false)
			} else {
				srv.AddNotification("morning", tech, msg, true)
			}
		}
	}

	statsJob := func() {
		c := config.Get()
		stats := loadLatest(c, srv)
		if stats == nil || waClient == nil {
			return
		}
		// Send supervisor-specific stats message
		msg := formatSupervisorStatsMessage(stats)
		if err := waClient.SendMessage(c.MyNumber, msg); err != nil {
			log.Printf("[stats] Failed to send to supervisor: %v", err)
			srv.AddNotification("stats", c.MyNumber, msg, false)
		} else {
			srv.AddNotification("stats", c.MyNumber, msg, true)
		}
	}

	eodJob := func() {
		c := config.Get()
		stats := loadLatest(c, srv)
		if stats == nil || waClient == nil {
			return
		}
		// Send final report to supervisor
		report := formatEODReport(stats)
		if err := waClient.SendMessage(c.MyNumber, report); err != nil {
			log.Printf("[eod] Failed to send report to supervisor: %v", err)
			srv.AddNotification("eod_report", c.MyNumber, report, false)
		} else {
			srv.AddNotification("eod_report", c.MyNumber, report, true)
		}

		// Build a lookup of stats by technician name for personalized messages
		statsByName := make(map[string]models.TechStats)
		for _, t := range stats.ByTechnician {
			statsByName[t.Name] = t
		}

		// Send thank-you to ALL technicians with phone numbers (not just those in the Excel)
		for tech, phone := range c.Technicians {
			if phone == "" {
				srv.AddNotification("warning", "Système", fmt.Sprintf("⚠️ Le technicien %s n'a pas de numéro, il ne recevra aucun message.", tech), false)
				continue
			}
			// Skip supervisor — they get the EOD report instead
			if normalizePhone(phone) == normalizePhone(c.MyNumber) {
				continue
			}
			// Personalized message if we have stats for this tech, generic otherwise
			var msg string
			if ts, ok := statsByName[tech]; ok {
				msg = formatThankYouMessage(c, ts)
			} else {
				msg = formatGenericThankYouMessage(c, tech)
			}
			if err := waClient.SendMessage(phone, msg); err != nil {
				log.Printf("[eod-thanks] Failed to send to %s: %v", tech, err)
				srv.AddNotification("eod_thanks", tech, msg, false)
			} else {
				srv.AddNotification("eod_thanks", tech, msg, true)
			}
		}
	}

	// lateStartJob: thin wrapper used for on-demand testing and noon failsafe cron.
	// Normally driven by file-events (watcher + GDrive sync).
	lateStartJob := func() {
		c := config.Get()
		stats := loadLatest(c, srv)
		if stats == nil {
			return
		}
		checkLateStarts(c, stats, waClient, srv)
	}

	morningSpec := fmt.Sprintf("0 %d %d * * *", cfg.MorningMinute, cfg.MorningHour)
	sched.AddJob("morning", morningSpec, morningJob)

	// Stats at regular intervals (HH:MM), stopping at or before EOD
	intervalMinutes := cfg.StatsIntervalH*60 + cfg.StatsIntervalM
	if intervalMinutes <= 0 {
		intervalMinutes = 120 // default 2h
	}
	// EOD limit in minutes from midnight
	eodLimitMin := cfg.EODHour*60 + cfg.EODMinute
	if eodLimitMin == 0 {
		eodLimitMin = 17 * 60
	}
	// Generate stats send times starting at 09:00, spaced by intervalMinutes
	for t := 9 * 60; t < eodLimitMin; t += intervalMinutes {
		sh := t / 60
		sm := t % 60
		spec := fmt.Sprintf("0 %d %d * * *", sm, sh)
		jobName := fmt.Sprintf("stats_%02d%02d", sh, sm)
		sched.AddJob(jobName, spec, statsJob)
	}

	// Noon failsafe: re-check late-starts once at noon in case watcher/Drive
	// events were missed (e.g. app restart). The function deduplicates itself.
	sched.AddJob("late_start_noon", "0 0 12 * * *", lateStartJob)

	// End-of-day report + thank you (configurable hour)
	eodSpec := fmt.Sprintf("0 %d %d * * *", cfg.EODMinute, cfg.EODHour)
	sched.AddJob("eod_report", eodSpec, eodJob)

	// Register job functions with the web server for on-demand testing
	if os.Getenv("DEBUG_MODE") == "true" {
		srv.SetJobFunctions(map[string]func(){
			"morning":    morningJob,
			"stats":      statsJob,
			"eod":        eodJob,
			"late_start": lateStartJob,
		})
	}

	sched.Start()


	// ---- Google Drive Sync (periodic) ----
	if driveClient != nil {
		go func() {
			for {
				c := config.Get()
				interval := c.GDriveSyncMinutes
				if interval <= 0 {
					interval = 5
				}
				time.Sleep(time.Duration(interval) * time.Minute)

				if !driveClient.IsConfigured() {
					continue
				}

				downloaded, err := driveClient.SyncFolder(c.WatchFolder)
				if err != nil {
					log.Printf("[gdrive-sync] Error: %v", err)
					continue
				}
				if len(downloaded) > 0 {
					log.Printf("[gdrive-sync] Downloaded %d new files", len(downloaded))
					var latestStats *models.DailyStats
					for _, p := range downloaded {
						stats, err := excel.Parse(p)
						if err == nil {
							srv.UpdateStats(stats)
							syncTechniciansFromStats(stats)
							latestStats = stats
						}
					}
					// After each Drive sync, check for unstarted interventions.
					if latestStats != nil {
						checkLateStarts(config.Get(), latestStats, waClient, srv)
					}
				}
			}
		}()
	}

	fmt.Println()
	fmt.Printf("  🌐  Dashboard exposed on: http://0.0.0.0:%d (Accessible via Public IP)\n", cfg.WebPort)
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
	pattern := cfg.ExcelPattern
	if pattern == "" {
		pattern = "*.xlsx"
	}
	matches, _ := filepath.Glob(filepath.Join(cfg.WatchFolder, pattern))
	log.Printf("[loadLatest] Found %d matches for pattern %s in %s", len(matches), pattern, cfg.WatchFolder)
	if len(matches) == 0 {
		return nil
	}

	// Prefer today's date file (YYYY-MM-DD.xlsx) first.
	todayFile := filepath.Join(cfg.WatchFolder, time.Now().Format("2006-01-02")+".xlsx")
	target := ""
	for _, m := range matches {
		if m == todayFile {
			target = m
			break
		}
	}

	// Fall back to newest by mod-time (for manually uploaded or legacy-named files).
	if target == "" {
		var latestTime int64
		for _, m := range matches {
			info, err := os.Stat(m)
			if err == nil && info.ModTime().Unix() > latestTime {
				latestTime = info.ModTime().Unix()
				target = m
			}
		}
	}
	log.Printf("[loadLatest] Selected target: %s", target)

	if target == "" {
		return nil
	}

	stats, err := excel.Parse(target)
	if err != nil {
		log.Printf("Error parsing latest file: %v", err)
		return nil
	}
	srv.UpdateStats(stats)
	return stats
}

// syncTechniciansFromStats discovers all technician names from the parsed Excel
// data and adds any new ones to the config (with an empty phone number).
// Scans both ByTechnician (GANTT-derived) and AllRecords.Tech (Tableau récap)
// so technicians present in the sheet but with 0 stats are still captured.
func syncTechniciansFromStats(stats *models.DailyStats) {
	if stats == nil {
		return
	}

	cfg := config.Get()
	if cfg.Technicians == nil {
		cfg.Technicians = make(map[string]string)
	}

	added := 0

	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, exists := cfg.Technicians[name]; !exists {
			cfg.Technicians[name] = ""
			added++
			log.Printf("[auto-sync] New technician discovered: %s", name)
		}
	}

	// From GANTT-derived stats
	for _, t := range stats.ByTechnician {
		add(t.Name)
	}
	// From Tableau récap (catches techs with no completed interventions yet)
	for _, r := range stats.AllRecords {
		add(r.Tech)
	}

	if added > 0 {
		if err := config.Save(cfg); err != nil {
			log.Printf("[auto-sync] Error saving config: %v", err)
		} else {
			log.Printf("[auto-sync] Added %d new technician(s) to config", added)
		}
	}
}

// normalizePhone strips leading '+' and spaces for reliable phone comparison.
func normalizePhone(phone string) string {
	p := strings.TrimSpace(phone)
	p = strings.TrimPrefix(p, "+")
	return p
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

// formatSupervisorStatsMessage builds a dedicated, actionable stats message for the supervisor.
func formatSupervisorStatsMessage(s *models.DailyStats) string {
	total := s.TotalOK + s.TotalNOK
	msg := "📋 *Moca Consult — Rapport Superviseur*\n"
	msg += fmt.Sprintf("📅 %s\n", s.Date)
	msg += "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n"

	// Global figures
	msg += "📊 *Vue d'ensemble:*\n"
	msg += fmt.Sprintf("  🔧 Total interventions: *%d*\n", total)
	msg += fmt.Sprintf("  ✅ Réussies: *%d*\n", s.TotalOK)
	msg += fmt.Sprintf("  ❌ Échouées: *%d*\n", s.TotalNOK)
	msg += fmt.Sprintf("  📈 Taux de réussite: *%.1f%%*\n\n", s.RateOK)

	// Performance by technician with color-coded status
	msg += "👥 *Performance équipe:*\n"
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

	// Alerts section
	if s.TotalNOK > 0 {
		msg += "\n⚠️ *Points d'attention:*\n"
		// Show top NOK technicians
		for _, t := range s.ByTechnician {
			if t.NOK > 0 {
				techTotal := t.OK + t.NOK
				rate := float64(0)
				if techTotal > 0 {
					rate = float64(t.OK) / float64(techTotal) * 100
				}
				if rate < 70 {
					msg += fmt.Sprintf("  ⚡ %s — %d NOK (%.0f%% réussite)\n", t.Name, t.NOK, rate)
				}
			}
		}
	}

	msg += "\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
	msg += "🔄 Prochain rapport dans 2h"
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

func formatMorningMessage(cfg *config.Config, techName string) string {
	msg := cfg.MsgMorning
	msg = strings.ReplaceAll(msg, "{prenom}", techName)
	return msg
}

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

func formatThankYouMessage(cfg *config.Config, t models.TechStats) string {
	total := t.OK + t.NOK
	rate := float64(0)
	if total > 0 {
		rate = float64(t.OK) / float64(total) * 100
	}

	// Start with configurable greeting
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
	return msg
}

// formatGenericThankYouMessage is for techs who have a phone but aren't in the Excel stats.
func formatGenericThankYouMessage(cfg *config.Config, techName string) string {
	msg := strings.ReplaceAll(cfg.MsgEODThanks, "{prenom}", techName)
	return msg
}

// formatLateStartMessage builds the late-start alert for a technician.
// Supports {prenom} and {jeton} placeholders.
func formatLateStartMessage(cfg *config.Config, techName, reference string) string {
	msg := cfg.MsgLateStart
	msg = strings.ReplaceAll(msg, "{prenom}", techName)
	msg = strings.ReplaceAll(msg, "{jeton}", reference)
	return msg
}

// checkLateStarts scans all intervention records and sends a late-start alert
// to every technician whose RDV appointment time has passed but whose intervention
// has not yet started. Guarantees:
//   - Only fires within the operating window: morningTime → (EOD - 1h)
//   - Only if the intervention RDV/appointment time has already passed
//   - Never re-sends to the same technician today (checks notification log)
//   - One alert per jeton per day regardless of how many triggers fire
func checkLateStarts(c *config.Config, stats *models.DailyStats, waClient *whatsapp.Client, srv *web.Server) {
	if waClient == nil || !waClient.IsConnected() || stats == nil {
		return
	}

	// Load the configured timezone
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

	// Operating window: morningHour:morningMinute  →  (EODHour:EODMinute − 1 hour)
	morningMins := c.MorningHour*60 + c.MorningMinute
	eodMins := c.EODHour*60 + c.EODMinute
	if eodMins == 0 {
		eodMins = 17 * 60
	}
	cutoffMins := eodMins - 60 // 1h before EOD
	if cutoffMins < 0 {
		cutoffMins = 0
	}
	nowMins := now.Hour()*60 + now.Minute()

	if nowMins < morningMins || nowMins >= cutoffMins {
		log.Printf("[late-start] Outside operating window (%02d:%02d–%02d:%02d) — skipping",
			morningMins/60, morningMins%60, cutoffMins/60, cutoffMins%60)
		return
	}

	for _, r := range stats.AllRecords {
		techName := strings.TrimSpace(r.Tech)
		reference := strings.TrimSpace(r.Reference)
		if techName == "" || reference == "" {
			continue
		}

		// Intervention must be genuinely unstarted
		stateUpper := strings.ToUpper(strings.TrimSpace(r.State))
		terminee := strings.ToUpper(strings.TrimSpace(r.Finished))
		startEmpty := strings.TrimSpace(r.StartTime) == ""

		if !startEmpty || terminee == "OUI" || stateUpper == "OK" || stateUpper == "NOK" {
			continue // already started or completed
		}

		// The RDV appointment time must have already passed
		rdvRaw := strings.TrimSpace(r.RDVDate)
		if rdvRaw == "" {
			continue
		}
		rdvTime, ok := parseRDVDateTime(rdvRaw, loc, now)
		if !ok {
			log.Printf("[late-start] Cannot parse RDV date %q for jeton %s — skipping", rdvRaw, reference)
			continue
		}
		if now.Before(rdvTime) {
			// Appointment time hasn't arrived yet — do not alert
			continue
		}

		// --- Deduplication: keyed per jeton (not per tech) ---
		// Recipient stored as "TechName — Jeton: XXXXX" for UI clarity and dedup.
		recipientKey := fmt.Sprintf("%s — Jeton: %s", techName, reference)
		if srv.HasSentNotificationToday("late_start", recipientKey) {
			log.Printf("[late-start] Already sent for jeton %s today — skipping", reference)
			continue
		}

		// Look up phone; skip if missing or is the supervisor
		phone, exists := c.Technicians[techName]
		if !exists || phone == "" {
			srv.AddNotification("warning", "Système",
				fmt.Sprintf("⚠️ Relance démarrage: %s (Jeton: %s) — pas de numéro enregistré", techName, reference), false)
			continue
		}
		if normalizePhone(phone) == normalizePhone(c.MyNumber) {
			continue // supervisor gets EOD report, not this alert
		}

		msg := formatLateStartMessage(c, techName, reference)
		if err := waClient.SendMessage(phone, msg); err != nil {
			log.Printf("[late-start] Failed to send to %s for jeton %s (RDV %s): %v", techName, reference, rdvRaw, err)
			srv.AddNotification("late_start", recipientKey, msg, false)
		} else {
			log.Printf("[late-start] ✅ Alert sent to %s — jeton %s (RDV %s, now %s)", techName, reference, rdvRaw, now.Format("15:04"))
			srv.AddNotification("late_start", recipientKey, msg, true)
		}
	}
}

// parseRDVDateTime parses an RDV date string from the Excel into a time.Time.
// Handles the following formats produced by the parser:
//   - "30/03/2026 09:00"  (DD/MM/YYYY HH:MM  — most common from Tableau récap)
//   - "09:00"             (HH:MM only  — assumes today's date)
//   - ISO variants already handled by formatTime in parser (returned as "HH:MM")
//
// Returns (parsed time, true) or (zero, false) on failure.
func parseRDVDateTime(raw string, loc *time.Location, now time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}

	// Try full DD/MM/YYYY HH:MM
	if t, err := time.ParseInLocation("02/01/2006 15:04", raw, loc); err == nil {
		return t, true
	}
	// Try full DD/MM/YYYY HH:MM:SS
	if t, err := time.ParseInLocation("02/01/2006 15:04:05", raw, loc); err == nil {
		return t, true
	}
	// Try ISO full datetime
	if t, err := time.ParseInLocation("2006-01-02 15:04", raw, loc); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", raw, loc); err == nil {
		return t, true
	}
	// Fallback: HH:MM only — assume today's date
	if t, err := time.ParseInLocation("15:04", raw, loc); err == nil {
		return time.Date(now.Year(), now.Month(), now.Day(),
			t.Hour(), t.Minute(), 0, 0, loc), true
	}

	return time.Time{}, false
}

// syncAdminPasswordToZitadel pushes the new password to Zitadel via the Admin API
// so the primary headless-session auth path stays in sync with the env var.
// Runs in a goroutine — failures are logged but never fatal.
func syncAdminPasswordToZitadel(cfg *config.Config, email, password string) {
	if cfg.ZitadelPAT == "" || cfg.OIDCIssuerURL == "" {
		log.Println("[auth-sync] Zitadel not configured — skipping Zitadel password sync")
		return
	}

	// 1. Find the user in Zitadel by email
	searchPayload, _ := json.Marshal(map[string]interface{}{
		"queries": []map[string]interface{}{
			{"emailQuery": map[string]interface{}{
				"emailAddress": email,
				"method":       "TEXT_QUERY_METHOD_EQUALS",
			}},
		},
	})
	searchReq, _ := http.NewRequest("POST", cfg.OIDCIssuerURL+"/v2/users", bytes.NewReader(searchPayload))
	searchReq.Header.Set("Content-Type", "application/json")
	searchReq.Header.Set("Authorization", "Bearer "+cfg.ZitadelPAT)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(searchReq)
	if err != nil {
		log.Printf("[auth-sync] Could not reach Zitadel to sync password for %s: %v", email, err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var searchResult struct {
		Result []struct {
			UserID string `json:"userId"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &searchResult); err != nil || len(searchResult.Result) == 0 {
		log.Printf("[auth-sync] User %s not found in Zitadel — skipping Zitadel password sync", email)
		return
	}
	userID := searchResult.Result[0].UserID

	// 2. Set the new password directly (no verification code — IAM_OWNER PAT bypasses it)
	passPayload, _ := json.Marshal(map[string]interface{}{
		"newPassword": map[string]interface{}{
			"password":       password,
			"changeRequired": false,
		},
	})
	passReq, _ := http.NewRequest("POST",
		cfg.OIDCIssuerURL+"/v2/users/"+userID+"/password",
		bytes.NewReader(passPayload),
	)
	passReq.Header.Set("Content-Type", "application/json")
	passReq.Header.Set("Authorization", "Bearer "+cfg.ZitadelPAT)

	passResp, err := client.Do(passReq)
	if err != nil {
		log.Printf("[auth-sync] Zitadel password update request failed for %s: %v", email, err)
		return
	}
	defer passResp.Body.Close()

	if passResp.StatusCode == http.StatusOK {
		log.Printf("[auth-sync] ✅ Password synced to Zitadel for %s", email)
	} else {
		errBody, _ := io.ReadAll(passResp.Body)
		log.Printf("[auth-sync] Zitadel password sync failed for %s (HTTP %d): %s", email, passResp.StatusCode, string(errBody))
	}
}
