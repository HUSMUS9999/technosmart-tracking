package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fiber-tracker/internal/config"
	"fiber-tracker/internal/db"
	"fiber-tracker/internal/excel"
	"fiber-tracker/internal/models"
	"fiber-tracker/internal/whatsapp"
	
	"github.com/joho/godotenv"
)

func main() {
	log.SetFlags(log.Ltime | log.Lshortfile)

	jobFlag := flag.String("job", "all", "Which job to fire: morning, stats, eod, all")
	targetFlag := flag.String("target", "", "Send only to this technician name (default: all techs for morning/eod, MY_NUMBER for stats)")
	dryRun := flag.Bool("dry", false, "Dry run — print messages but don't actually send")
	configPath := flag.String("config", "config.json", "Path to config.json")
	flag.Parse()

	fmt.Println("══════════════════════════════════════════")
	fmt.Println("  🕐 Fiber Tracker — Clock Test Tool")
	fmt.Println("══════════════════════════════════════════")
	fmt.Println()

	// Load .env first
	godotenv.Load("../../.env")
	godotenv.Load(".env")

	// Initialize DB first
	if err := db.InitDB(); err != nil {
		log.Fatalf("❌ Failed to init DB: %v", err)
	}

	// Load the LATEST config
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("❌ Failed to load config: %v", err)
	}
	fmt.Printf("✅ Config loaded from %s\n", *configPath)
	fmt.Printf("   WhatsApp enabled: %v\n", cfg.WhatsAppEnabled)
	fmt.Printf("   Morning: %02d:%02d\n", cfg.MorningHour, cfg.MorningMinute)
	fmt.Printf("   EOD:     %02d:%02d\n", cfg.EODHour, cfg.EODMinute)
	fmt.Printf("   Stats interval: %dh%02dm\n", cfg.StatsIntervalH, cfg.StatsIntervalM)
	fmt.Printf("   MY_NUMBER (supervisor): %s\n", cfg.MyNumber)
	fmt.Printf("   Technicians: %d entries\n", len(cfg.Technicians))
	fmt.Println()

	// Connect to Postgres (same as running app)
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "fiber_admin"
	}
	dbPass := os.Getenv("DB_PASSWORD")
	if dbPass == "" {
		dbPass = "secret_fiber_password"
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "fiber_tracker"
	}
	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" {
		dbPort = "5444" // external docker port
	}

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", dbHost, dbPort, dbUser, dbPass, dbName)
	fmt.Printf("📡 Connecting to Postgres (%s:%s)...\n", dbHost, dbPort)

	var waClient *whatsapp.Client
	if !*dryRun {
		waClient, err = whatsapp.New(connStr)
		if err != nil {
			log.Fatalf("❌ WhatsApp init error: %v", err)
		}
		if !waClient.IsConnected() {
			log.Fatalf("❌ WhatsApp is NOT connected. Please link WhatsApp from the Settings page first.")
		}
		fmt.Printf("✅ WhatsApp connected as %s\n", waClient.PhoneNumber())
	} else {
		fmt.Println("🏜️  DRY RUN mode — will print messages but not send them")
	}
	fmt.Println()

	// Load latest Excel stats
	stats := loadLatestExcel(cfg)
	if stats != nil {
		fmt.Printf("📊 Latest stats loaded: %s — %d OK / %d NOK (%.1f%%)\n", stats.Date, stats.TotalOK, stats.TotalNOK, stats.RateOK)
	} else {
		fmt.Println("⚠️  No Excel files found — stats/EOD jobs will be skipped")
	}
	fmt.Println()

	// Determine which jobs to fire
	job := strings.ToLower(*jobFlag)
	fired := 0

	// ─── MORNING JOB ───
	if job == "all" || job == "morning" {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("🌅 FIRING: Morning Message (supervisor excluded)")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		for tech, number := range cfg.Technicians {
			if number == "" {
				continue
			}
			// Skip supervisor
			if normalizePhone(number) == normalizePhone(cfg.MyNumber) {
				fmt.Printf("  ⏭️  %s (%s) — SKIPPED (supervisor)\n", tech, number)
				continue
			}
			if *targetFlag != "" && !strings.EqualFold(tech, *targetFlag) {
				continue
			}
			msg := formatMorningMessage(cfg, tech)
			fmt.Printf("  → %s (%s)\n", tech, number)
			fmt.Printf("    Message: %s\n", truncate(msg, 120))
			if !*dryRun {
				if err := waClient.SendMessage(number, msg); err != nil {
					fmt.Printf("    ❌ Error: %v\n", err)
				} else {
					fmt.Printf("    ✅ Sent!\n")
				}
				time.Sleep(2 * time.Second)
			}
			fired++
		}
		fmt.Println()
	}

	// ─── STATS JOB ───
	if job == "all" || job == "stats" {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("📊 FIRING: Supervisor Stats Message")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		if stats != nil {
			msg := formatSupervisorStatsMessage(stats)
			fmt.Printf("  → Sending to supervisor %s\n", cfg.MyNumber)
			fmt.Printf("    Message:\n%s\n", indent(msg, "    "))
			if !*dryRun {
				if err := waClient.SendMessage(cfg.MyNumber, msg); err != nil {
					fmt.Printf("    ❌ Error: %v\n", err)
				} else {
					fmt.Printf("    ✅ Sent!\n")
				}
			}
			fired++
		} else {
			fmt.Println("  ⚠️  Skipped — no stats data available")
		}
		fmt.Println()
	}

	// ─── EOD JOB ───
	if job == "all" || job == "eod" {
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		fmt.Println("🌙 FIRING: End-of-Day Report + Thank You")
		fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
		if stats != nil {
			// EOD Report to supervisor
			report := formatEODReport(stats)
			fmt.Printf("  → EOD Report to supervisor (%s)\n", cfg.MyNumber)
			fmt.Printf("    Message:\n%s\n", indent(report, "    "))
			if !*dryRun {
				if err := waClient.SendMessage(cfg.MyNumber, report); err != nil {
					fmt.Printf("    ❌ Error: %v\n", err)
				} else {
					fmt.Printf("    ✅ Sent!\n")
				}
				time.Sleep(2 * time.Second)
			}
			fired++

			// Build stats lookup
			statsByName := make(map[string]models.TechStats)
			for _, t := range stats.ByTechnician {
				statsByName[t.Name] = t
			}

			// Thank-you to ALL technicians (not just those in Excel)
			fmt.Println()
			fmt.Println("  👋 Thank-you messages (all techs, supervisor excluded):")
			for tech, phone := range cfg.Technicians {
				if phone == "" {
					continue
				}
				// Skip supervisor
				if normalizePhone(phone) == normalizePhone(cfg.MyNumber) {
					fmt.Printf("  ⏭️  %s — SKIPPED (supervisor)\n", tech)
					continue
				}
				if *targetFlag != "" && !strings.EqualFold(tech, *targetFlag) {
					continue
				}
				var msg string
				if ts, ok := statsByName[tech]; ok {
					msg = formatThankYouMessage(cfg, ts)
				} else {
					msg = formatGenericThankYouMessage(cfg, tech)
				}
				fmt.Printf("  → %s (%s)\n", tech, phone)
				fmt.Printf("    Message: %s\n", truncate(msg, 120))
				if !*dryRun {
					if err := waClient.SendMessage(phone, msg); err != nil {
						fmt.Printf("    ❌ Error: %v\n", err)
					} else {
						fmt.Printf("    ✅ Sent!\n")
					}
					time.Sleep(2 * time.Second)
				}
				fired++
			}
		} else {
			fmt.Println("  ⚠️  Skipped — no stats data available")
		}
		fmt.Println()
	}

	fmt.Println("══════════════════════════════════════════")
	if *dryRun {
		fmt.Printf("🏜️  DRY RUN complete. %d message(s) would have been sent.\n", fired)
	} else {
		fmt.Printf("✅ Done! Fired %d message(s) via WhatsApp.\n", fired)
	}
	fmt.Println("══════════════════════════════════════════")

	if waClient != nil {
		waClient.Disconnect()
	}
}

// ─── Message formatters ───

func normalizePhone(phone string) string {
	p := strings.TrimSpace(phone)
	p = strings.TrimPrefix(p, "+")
	return p
}

func formatMorningMessage(cfg *config.Config, techName string) string {
	msg := cfg.MsgMorning
	msg = strings.ReplaceAll(msg, "{prenom}", techName)
	return msg
}

func formatSupervisorStatsMessage(s *models.DailyStats) string {
	total := s.TotalOK + s.TotalNOK
	msg := "📋 *Moca Consult — Rapport Superviseur*\n"
	msg += fmt.Sprintf("📅 %s\n", s.Date)
	msg += "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n"
	msg += "📊 *Vue d'ensemble:*\n"
	msg += fmt.Sprintf("  🔧 Total interventions: *%d*\n", total)
	msg += fmt.Sprintf("  ✅ Réussies: *%d*\n", s.TotalOK)
	msg += fmt.Sprintf("  ❌ Échouées: *%d*\n", s.TotalNOK)
	msg += fmt.Sprintf("  📈 Taux de réussite: *%.1f%%*\n\n", s.RateOK)
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
	if s.TotalNOK > 0 {
		msg += "\n⚠️ *Points d'attention:*\n"
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

func formatGenericThankYouMessage(cfg *config.Config, techName string) string {
	msg := strings.ReplaceAll(cfg.MsgEODThanks, "{prenom}", techName)
	return msg
}

// ─── Helpers ───

func loadLatestExcel(cfg *config.Config) *models.DailyStats {
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
		log.Printf("Error parsing %s: %v", filepath.Base(latest), err)
		return nil
	}
	return stats
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ↵ ")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func indent(s string, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}
