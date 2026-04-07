package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
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
	EODHour           int               `json:"EOD_HOUR"`
	Timezone          string            `json:"FRANCE_TZ"`
	NTPServer         string            `json:"NTP_SERVER"`
	WebPort           int               `json:"WEB_PORT"`
	WhatsAppEnabled   bool              `json:"WHATSAPP_ENABLED"`
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
	
	if portStr := os.Getenv("PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			cfg.WebPort = port
		}
	} else if cfg.WebPort == 0 {
		cfg.WebPort = 8080
	}
	if cfg.StatsIntervalH == 0 {
		cfg.StatsIntervalH = 2
	}
	if cfg.EODHour == 0 {
		cfg.EODHour = 17
	}

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
	return os.WriteFile(path, data, 0644)
}
