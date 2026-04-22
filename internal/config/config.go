package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	
	"github.com/joho/godotenv"
)

// Config holds all application settings.
type Config struct {
	Technicians       map[string]string `json:"TECHNICIENS"`
	MyNumber          string            `json:"MY_NUMBER"`
	WatchFolder       string            `json:"WATCH_FOLDER"`
	ExcelPattern      string            `json:"EXCEL_PATTERN"`
	MorningHour       int               `json:"MORNING_HOUR"`
	MorningMinute     int               `json:"MORNING_MINUTE"`
	StatsIntervalH    int               `json:"STATS_INTERVAL_HOURS"`
	StatsIntervalM    int               `json:"STATS_INTERVAL_MINUTES"`
	EODHour           int               `json:"EOD_HOUR"`
	EODMinute         int               `json:"EOD_MINUTE"`
	Timezone          string            `json:"FRANCE_TZ"`
	NTPServer         string            `json:"NTP_SERVER"`
	WebPort           int               `json:"WEB_PORT"`
	WhatsAppEnabled   bool              `json:"WHATSAPP_ENABLED"`
	// Message templates
	MsgTest           string            `json:"MSG_TEST"`
	MsgMorning        string            `json:"MSG_MORNING"`
	MsgEODThanks      string            `json:"MSG_EOD_THANKS"`
	MsgLateStart      string            `json:"MSG_LATE_START"`
	// Authentication
	AdminEmail        string            `json:"ADMIN_EMAIL"`
	AdminPassword     string            `json:"ADMIN_PASSWORD"` // only used for initial setup, cleared after
	// OIDC Authentication (Zitadel) - Set securely via .env file now
	OIDCClientID      string            `json:"OIDC_CLIENT_ID"`
	OIDCClientSecret  string            `json:"-"`
	OIDCIssuerURL     string            `json:"OIDC_ISSUER_URL"`
	OIDCRedirectURI   string            `json:"OIDC_REDIRECT_URI"`
	ZitadelPAT        string            `json:"-"`
	// SMTP
	SMTPHost          string            `json:"SMTP_HOST"`
	SMTPPort          int               `json:"SMTP_PORT"`
	SMTPUsername      string            `json:"SMTP_USERNAME"`
	SMTPPassword      string            `json:"SMTP_PASSWORD"`
	SMTPFrom          string            `json:"SMTP_FROM"`
	// Google Drive (public link)
	GDriveFolderID    string            `json:"GDRIVE_FOLDER_ID"`
	GDriveFolderName  string            `json:"GDRIVE_FOLDER_NAME"`
	GDriveEnabled     bool              `json:"GDRIVE_ENABLED"`
	GDriveSyncMinutes int               `json:"GDRIVE_SYNC_MINUTES"`
}

var (
	current *Config
	mu      sync.RWMutex
	path    string
)

// Load reads config from disk.
func Load(filepath string) (*Config, error) {
	path = filepath
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Always load .env file for secrets (ignores error if .env doesn't exist)
	godotenv.Load(".env")

	// ---- Secrets from environment (always win over config.json) ----

	// OIDC
	if v := os.Getenv("OIDC_CLIENT_SECRET"); v != "" {
		cfg.OIDCClientSecret = v
	}
	if v := os.Getenv("ZITADEL_SERVICE_PAT"); v != "" {
		cfg.ZitadelPAT = v
	}
	if v := os.Getenv("OIDC_CLIENT_ID"); v != "" {
		cfg.OIDCClientID = v
	}
	if v := os.Getenv("OIDC_ISSUER_URL"); v != "" {
		cfg.OIDCIssuerURL = v
	}
	if v := os.Getenv("OIDC_REDIRECT_URI"); v != "" {
		cfg.OIDCRedirectURI = v
	}

	// SMTP
	if v := os.Getenv("SMTP_PASSWORD"); v != "" {
		cfg.SMTPPassword = v
	}
	if v := os.Getenv("SMTP_HOST"); v != "" {
		cfg.SMTPHost = v
	}
	if v := os.Getenv("SMTP_USERNAME"); v != "" {
		cfg.SMTPUsername = v
	}
	if v := os.Getenv("SMTP_FROM"); v != "" {
		cfg.SMTPFrom = v
	}
	if v := os.Getenv("SMTP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.SMTPPort = p
		}
	}

	// DB (used by main.go directly via os.Getenv, but keep in sync on cfg for completeness)
	if v := os.Getenv("DB_HOST"); v != "" && cfg.WatchFolder != "" { // only override if config already loaded
		_ = v // DB fields are read directly in main.go; no Config field for them
	}

	// Admin bootstrap credentials
	if v := os.Getenv("ADMIN_EMAIL"); v != "" && cfg.AdminEmail == "" {
		cfg.AdminEmail = v
	}
	if v := os.Getenv("ADMIN_PASSWORD"); v != "" && cfg.AdminPassword == "" {
		cfg.AdminPassword = v
	}

	// Defaults
	if cfg.ExcelPattern == "" {
		cfg.ExcelPattern = "*.xlsx"
	}
	if cfg.Timezone == "" {
		cfg.Timezone = "Europe/Paris"
	}
	if cfg.NTPServer == "" {
		cfg.NTPServer = "fr.pool.ntp.org"
	}
	if cfg.MsgTest == "" {
		cfg.MsgTest = "✅ Test Moca consult — la connexion WhatsApp fonctionne !"
	}
	if cfg.MsgMorning == "" {
		cfg.MsgMorning = "Bonjour à tous,\n\nMerci de respecter les horaires d'intervention. Tout retard doit être anticipé et signalé en amont, c'est essentiel pour le bon déroulement des opérations.\n\nJe compte sur chacun pour appliquer le process correctement du début jusqu'à la fin, sans négliger les étapes. L'objectif est simple : une intervention réussie dès le premier passage et un client satisfait.\n\nPrenez le temps de bien faire, vérifiez votre travail avant de quitter le site et assurez-vous que tout est conforme.\n\nOn doit tous maintenir un niveau d'exigence élevé sur le terrain. Votre sérieux et votre rigueur font toute la différence."
	}
	if cfg.MsgEODThanks == "" {
		cfg.MsgEODThanks = "👋 Bonsoir {prenom} !\n\nMerci pour ton travail aujourd'hui ! 🙏\nBonne soirée ! 🌙"
	}
	if cfg.MsgLateStart == "" {
		cfg.MsgLateStart = "Bonjour,\n\nJe constate que ton intervention (Jeton : {jeton}) n'a pas encore été démarrée. Merci de me faire un point immédiatement sur la situation.\n\nLes interventions doivent être lancées dans les délais prévus. Si tu rencontres une difficulté, elle doit être remontée sans attendre.\n\nMerci de démarrer rapidement et de respecter le process jusqu'à la clôture, en assurant une intervention propre et la satisfaction du client.\n\nJe compte sur ta réactivité."
	}
	
	if portStr := os.Getenv("PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.WebPort = port
		}
	} else if cfg.WebPort == 0 {
		cfg.WebPort = 9510
	}
	if cfg.StatsIntervalH == 0 && cfg.StatsIntervalM == 0 {
		cfg.StatsIntervalH = 2
	}
	if cfg.EODHour == 0 {
		cfg.EODHour = 17
	}
	if cfg.SMTPPort == 0 {
		cfg.SMTPPort = 587
	}
	if cfg.GDriveSyncMinutes == 0 {
		cfg.GDriveSyncMinutes = 5
	}
	// Admin email and password come from config or env — no hardcoded defaults
	// OIDC settings come from config or env — no hardcoded defaults

	mu.Lock()
	current = &cfg
	mu.Unlock()

	return &cfg, nil
}

// Get returns the current config (thread-safe).
func Get() *Config {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// Save writes the current config back to disk.
func Save(cfg *Config) error {
	mu.Lock()
	current = cfg
	mu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}
