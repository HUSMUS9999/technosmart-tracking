package web

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fiber-tracker/internal/auth"
	"fiber-tracker/internal/config"
	"fiber-tracker/internal/db"
	"fiber-tracker/internal/excel"
	"fiber-tracker/internal/gdrive"
	"fiber-tracker/internal/models"
	"fiber-tracker/internal/smtp"
	"fiber-tracker/internal/whatsapp"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

//go:embed static/*
var staticFS embed.FS

// Server is the HTTP server for the web dashboard.
type Server struct {
	port          int
	latestStats   *models.DailyStats
	activeFile    string // currently active Excel file name
	notifications []models.NotificationLog
	mu            sync.RWMutex
	notifID       int
	onNewFile     func(path string) // callback when file uploaded
	waClient      *whatsapp.Client  // WhatsApp client reference
	authStore     *auth.Store       // Authentication store
	driveClient   *gdrive.Client    // Google Drive client
	mailer        *smtp.Mailer      // SMTP mailer
	oauth2Config  oauth2.Config     // OIDC OAuth2 Configuration
	oidcVerifier  *oidc.IDTokenVerifier // OIDC Token Verifier
	jobFunctions  map[string]func()    // registered scheduled job functions for testing
	onSyncTechs   func(*models.DailyStats)
}

// New creates a new web server.
func New(port int) *Server {
	return &Server{
		port:          port,
		notifications: make([]models.NotificationLog, 0),
	}
}

// SetAuthStore sets the authentication store.
func (s *Server) SetAuthStore(store *auth.Store) {
	s.authStore = store
}

// SetDriveClient sets the Google Drive client.
func (s *Server) SetDriveClient(client *gdrive.Client) {
	s.driveClient = client
}

// SetMailer sets the SMTP mailer.
func (s *Server) SetMailer(m *smtp.Mailer) {
	s.mailer = m
}


// SetWhatsAppClient sets the WhatsApp client reference for API access.
func (s *Server) SetWhatsAppClient(c *whatsapp.Client) {
	s.mu.Lock()
	s.waClient = c
	s.mu.Unlock()
}

// SetOnNewFile sets the callback for uploaded files.
func (s *Server) SetOnNewFile(fn func(path string)) {
	s.onNewFile = fn
}

// SetOnSyncTechs sets the callback for auto-discovering technicians from a selected history file.
func (s *Server) SetOnSyncTechs(fn func(*models.DailyStats)) {
	s.onSyncTechs = fn
}

// UpdateStats updates the latest stats displayed on the dashboard.
func (s *Server) UpdateStats(stats *models.DailyStats) {
	s.mu.Lock()
	s.latestStats = stats
	if stats != nil {
		s.activeFile = stats.SourceFile
	}
	s.mu.Unlock()
}

// AddNotification adds a notification to the log.
func (s *Server) AddNotification(notifType, recipient, message string, success bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifID++
	if s.notifID > 1000000 {
		s.notifID = 1
	}
	s.notifications = append(s.notifications, models.NotificationLog{
		ID:        s.notifID,
		Timestamp: time.Now(),
		Type:      notifType,
		Recipient: recipient,
		Message:   message,
		Success:   success,
	})
	// Keep only last 200
	if len(s.notifications) > 200 {
		s.notifications = s.notifications[len(s.notifications)-200:]
	}
}

// HasSentNotificationToday returns true if a notification of the given type
// was successfully sent to the given recipient at any point today (calendar day).
func (s *Server) HasSentNotificationToday(notifType, recipient string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	today := time.Now().Format("2006-01-02")
	for _, n := range s.notifications {
		if n.Type == notifType &&
			strings.EqualFold(n.Recipient, recipient) &&
			n.Success &&
			n.Timestamp.Format("2006-01-02") == today {
			return true
		}
	}
	return false
}

// requireAdmin is a helper that checks if the current user has admin role.
// Returns true if authorized, false if rejected (and writes the HTTP error).
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user := auth.ContextUser(r)
	if user == nil || user.Role != "admin" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
		return false
	}
	return true
}

// Start begins serving the web dashboard.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Rate limiters for auth endpoints
	loginLimiter := newRateLimiter(5, 1*time.Minute)          // 5 attempts per minute per IP
	forgotLimiter := newRateLimiter(3, 10*time.Minute)        // 3 attempts per 10 minutes per IP

	// --- Public auth routes (no middleware) ---
	mux.HandleFunc("/api/auth/login", rateLimitMiddleware(loginLimiter, s.handleLogin))
	mux.HandleFunc("/api/auth/login/oidc", s.handleOIDCLogin)
	mux.HandleFunc("/api/auth/callback", s.handleOIDCCallback)
	mux.HandleFunc("/api/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/auth/forgot-password", rateLimitMiddleware(forgotLimiter, s.handleForgotPassword))
	mux.HandleFunc("/api/auth/reset-password", s.handleResetPassword)
	mux.HandleFunc("/api/auth/me", s.handleAuthMe)

	authWrap := func(h http.HandlerFunc) http.HandlerFunc {
		if s.authStore == nil {
			return h
		}
		return func(w http.ResponseWriter, r *http.Request) {
			auth.Middleware(s.authStore, http.HandlerFunc(h)).ServeHTTP(w, r)
		}
	}

	// --- Protected API routes ---
	mux.HandleFunc("/api/stats", authWrap(s.handleStats))
	mux.HandleFunc("/api/config", authWrap(s.handleConfig))
	mux.HandleFunc("/api/notifications", authWrap(s.handleNotifications))
	mux.HandleFunc("/api/upload", authWrap(s.handleUpload))
	mux.HandleFunc("/api/parse", authWrap(s.handleParse))
	mux.HandleFunc("/api/status", authWrap(s.handleStatus))
	mux.HandleFunc("/api/time", authWrap(s.handleTime))
	mux.HandleFunc("/api/files", authWrap(s.handleFiles))
	mux.HandleFunc("/api/files/select", authWrap(s.handleFileSelect))
	mux.HandleFunc("/api/whatsapp/status", authWrap(s.handleWAStatus))
	mux.HandleFunc("/api/whatsapp/qr", authWrap(s.handleWAQR))
	mux.HandleFunc("/api/whatsapp/logout", authWrap(s.handleWALogout))
	mux.HandleFunc("/api/whatsapp/send", authWrap(s.handleWASend))

	// Test endpoint — fire scheduled jobs immediately
	if os.Getenv("DEBUG_MODE") == "true" {
		mux.HandleFunc("/api/test/fire-jobs", authWrap(s.handleTestFireJobs))
	}

	// Google Drive API routes
	mux.HandleFunc("/api/drive/status", authWrap(s.handleDriveStatus))
	mux.HandleFunc("/api/drive/auth-url", authWrap(s.handleDriveAuthURL))
	mux.HandleFunc("/api/drive/callback", s.handleDriveCallback) // PUBLIC
	mux.HandleFunc("/api/drive/disconnect", authWrap(s.handleDriveDisconnect))
	mux.HandleFunc("/api/drive/folders", authWrap(s.handleDriveFolders))
	mux.HandleFunc("/api/drive/files", authWrap(s.handleDriveFiles))
	mux.HandleFunc("/api/drive/sync", authWrap(s.handleDriveSync))
	mux.HandleFunc("/api/drive/set-folder", authWrap(s.handleDriveSetFolder))

	// SMTP (Zitadel-managed)
	mux.HandleFunc("/api/smtp/test", authWrap(s.handleSMTPTest))
	mux.HandleFunc("/api/smtp/zitadel", authWrap(s.handleZitadelSMTP))       // GET=read, POST=create/update
	mux.HandleFunc("/api/smtp/zitadel/test", authWrap(s.handleZitadelSMTPTest)) // POST=send test email

	// Static files (embedded)
	mux.HandleFunc("/", s.handleStatic)

	var handler http.Handler = mux

	addr := fmt.Sprintf("0.0.0.0:%d", s.port)
	log.Printf("[web] Dashboard available at http://0.0.0.0:%d", s.port)

	// Initialize OIDC securely in the background
	go s.initOIDC()

	go func() {
		srv := &http.Server{
			Addr:         addr,
			Handler:      handler,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  120 * time.Second,
		}
		if err := srv.ListenAndServe(); err != nil {
			log.Fatalf("[web] Server error: %v", err)
		}
	}()

	return nil
}

func (s *Server) initOIDC() {
	cfg := config.Get()
	if cfg.OIDCClientID == "" || cfg.OIDCIssuerURL == "" {
		log.Println("[oidc] Missing client ID or provider URL. OIDC is disabled.")
		return
	}

	for i := 0; i < 30; i++ {
		provider, err := oidc.NewProvider(context.Background(), cfg.OIDCIssuerURL)
		if err == nil {
			s.mu.Lock()
			s.oauth2Config = oauth2.Config{
				ClientID:     cfg.OIDCClientID,
				ClientSecret: cfg.OIDCClientSecret,
				RedirectURL:  cfg.OIDCRedirectURI,
				Endpoint:     provider.Endpoint(),
				Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
			}
			s.oidcVerifier = provider.Verifier(&oidc.Config{ClientID: cfg.OIDCClientID})
			s.mu.Unlock()
			log.Printf("[oidc] Initialized with issuer %s", cfg.OIDCIssuerURL)
			return
		}
		log.Printf("[oidc] Provider initialization failed (attempt %d/30): %v. Retrying in 3s...", i+1, err)
		time.Sleep(3 * time.Second)
	}
	log.Println("[oidc] Failed to initialize provider after 30 attempts.")
}


func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	// Route login-related paths to login.html
	if path == "/login" || path == "/forgot-password" || path == "/reset-password" {
		path = "/login.html"
	}

	data, err := staticFS.ReadFile("static" + path)
	if err != nil {
		// Fallback to index.html for SPA routing
		data, err = staticFS.ReadFile("static/index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
		return
	}

	// Set content type and caching based on extension
	ext := filepath.Ext(path)
	switch ext {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=604800")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=604800")
	case ".woff2":
		w.Header().Set("Content-Type", "font/woff2")
		w.Header().Set("Cache-Control", "public, max-age=604800")
	}

	w.Write(data)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	stats := s.latestStats
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if stats == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "no_data",
			"message": "No Excel file has been processed yet. Upload one or drop it in the watch folder.",
		})
		return
	}
	json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		
		// SECURITY: Never send secrets back to the frontend
		type safeConfig struct {
			*config.Config
			SMTPPassword     string `json:"SMTP_PASSWORD"`
			OIDCClientSecret string `json:"OIDC_CLIENT_SECRET"`
			ZitadelPAT       string `json:"ZITADEL_SERVICE_PAT"`
			AdminPassword    string `json:"ADMIN_PASSWORD"`
		}
		safe := safeConfig{Config: cfg}
		if cfg.SMTPPassword != "" {
			safe.SMTPPassword = "********"
		}
		if cfg.OIDCClientSecret != "" {
			safe.OIDCClientSecret = "********"
		}
		if cfg.ZitadelPAT != "" {
			safe.ZitadelPAT = "********"
		}
		safe.AdminPassword = ""
		
		json.NewEncoder(w).Encode(safe)

	case http.MethodPut:
		if !requireAdmin(w, r) {
			return
		}
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		
		// If any secret wasn't changed from the masked version, keep the original
		oldCfg := config.Get()
		
		// Always create new
		realPassword := cfg.SMTPPassword
		if realPassword == "" || realPassword == "********" {
			realPassword = oldCfg.SMTPPassword
		}
		if cfg.OIDCClientSecret == "********" {
			cfg.OIDCClientSecret = oldCfg.OIDCClientSecret
		}
		if cfg.ZitadelPAT == "********" {
			cfg.ZitadelPAT = oldCfg.ZitadelPAT
		}
		// Preserve OIDC/auth fields if frontend didn't send them
		if cfg.OIDCClientID == "" {
			cfg.OIDCClientID = oldCfg.OIDCClientID
		}
		if cfg.OIDCClientSecret == "" || cfg.OIDCClientSecret == "********" {
			cfg.OIDCClientSecret = oldCfg.OIDCClientSecret
		}
		if cfg.OIDCIssuerURL == "" {
			cfg.OIDCIssuerURL = oldCfg.OIDCIssuerURL
		}
		if cfg.OIDCRedirectURI == "" {
			cfg.OIDCRedirectURI = oldCfg.OIDCRedirectURI
		}
		if cfg.ZitadelPAT == "" {
			cfg.ZitadelPAT = oldCfg.ZitadelPAT
		}
		// Support for partial updates
		if len(cfg.Technicians) == 0 {
			cfg.Technicians = oldCfg.Technicians
		} else {
			// SYNC to GORM DB!
			// Get existing guys from DB
			existing := db.GetTechniciansMap()
			// Insert/Update from the request
			for name, phone := range cfg.Technicians {
				db.EnsureTechnician(name, phone)
			}
			// Delete anything removed from the JSON payload
			for exName := range existing {
				if _, ok := cfg.Technicians[exName]; !ok {
					db.DeleteTechnician(exName)
				}
			}
			// And re-populate from DB as single source of truth
			cfg.Technicians = db.GetTechniciansMap()
		}

		if cfg.AdminEmail == "" {
			cfg.AdminEmail = oldCfg.AdminEmail
		}
		if cfg.AdminPassword == "" {
			cfg.AdminPassword = oldCfg.AdminPassword
		}
		// Preserve message templates if not sent by frontend
		if cfg.MsgLateStart == "" {
			cfg.MsgLateStart = oldCfg.MsgLateStart
		}
		
		if err := config.Save(&cfg); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		
		if s.mailer != nil {
			s.mailer.UpdateConfig(smtp.Config{
				Host:     cfg.SMTPHost,
				Port:     cfg.SMTPPort,
				Username: cfg.SMTPUsername,
				Password: cfg.SMTPPassword,
				From:     cfg.SMTPFrom,
			})
		}
		
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		if !requireAdmin(w, r) {
			return
		}
		
		idParam := r.URL.Query().Get("id")
		s.mu.Lock()
		if idParam == "" {
			// Clear all
			s.notifications = []models.NotificationLog{}
		} else {
			// Clear specific
			idToClear := 0
			fmt.Sscanf(idParam, "%d", &idToClear)
			var updated []models.NotificationLog
			for _, n := range s.notifications {
				if n.ID != idToClear {
					updated = append(updated, n)
				}
			}
			s.notifications = updated
		}
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"cleared"}`))
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	notifs := make([]models.NotificationLog, len(s.notifications))
	copy(notifs, s.notifications)
	s.mu.RUnlock()

	// Return in reverse chronological order
	for i, j := 0, len(notifs)-1; i < j; i, j = i+1, j-1 {
		notifs[i], notifs[j] = notifs[j], notifs[i]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(notifs)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !requireAdmin(w, r) {
		return
	}

	r.ParseMultipartForm(10 << 20) // 10MB — Excel files rarely exceed this
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"no file provided"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	// SECURITY: Validate file extension server-side
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".xlsx") {
		http.Error(w, `{"error":"only .xlsx files are accepted"}`, http.StatusBadRequest)
		return
	}

	// SECURITY: Sanitize filename to prevent path traversal
	safeFilename := filepath.Base(header.Filename)
	cfg := config.Get()
	destPath := filepath.Join(cfg.WatchFolder, safeFilename)

	dst, err := os.Create(destPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"cannot create file: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"write error: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	log.Printf("[web] File uploaded: %s", header.Filename)

	// Parse immediately
	stats, parseErr := excel.Parse(destPath)
	if parseErr != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "uploaded",
			"file":    header.Filename,
			"warning": fmt.Sprintf("File uploaded but parse failed: %s", parseErr.Error()),
		})
		return
	}

	s.UpdateStats(stats)

	if s.onNewFile != nil {
		go s.onNewFile(destPath)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"file":   header.Filename,
		"stats":  stats,
	})
}

func (s *Server) handleParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !requireAdmin(w, r) {
		return
	}

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		http.Error(w, `{"error":"provide 'path' in JSON body"}`, http.StatusBadRequest)
		return
	}

	// SECURITY: Restrict to files within WatchFolder only
	cfg := config.Get()
	absPath, _ := filepath.Abs(req.Path)
	absWatch, _ := filepath.Abs(cfg.WatchFolder)
	if !strings.HasPrefix(absPath, absWatch+string(filepath.Separator)) {
		http.Error(w, `{"error":"access denied: path outside watch folder"}`, http.StatusForbidden)
		return
	}

	stats, err := excel.Parse(absPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	s.UpdateStats(stats)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()
	s.mu.RLock()
	hasStats := s.latestStats != nil
	notifCount := len(s.notifications)
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":         "running",
		"has_stats":      hasStats,
		"notifications":  notifCount,
		"watch_folder":   cfg.WatchFolder,
		"whatsapp_ready": cfg.WhatsAppEnabled,
		"server_time":    time.Now().Format(time.RFC3339),
	})
}

// handleTime returns the current France time via NTP or HTTP fallback.
func (s *Server) handleTime(w http.ResponseWriter, r *http.Request) {
	cfg := config.Get()

	tz := cfg.Timezone
	if tz == "" {
		tz = "Europe/Paris"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.FixedZone("CET", 3600)
	}

	// Try NTP first (fast, 2s timeout)
	ntpServer := cfg.NTPServer
	if ntpServer == "" {
		ntpServer = "fr.pool.ntp.org"
	}
	utcTime, err := queryNTP(ntpServer)
	source := "ntp"

	if err != nil {
		// Fallback: HTTP time API
		utcTime, err = queryHTTPTime(tz)
		source = "http"
		if err != nil {
			// Last resort: system clock
			utcTime = time.Now().UTC()
			source = "system"
		}
	}

	franceTime := utcTime.In(loc)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"time":      franceTime.Format("15:04:05"),
		"date":      franceTime.Format("02/01/2006"),
		"datetime":  franceTime.Format(time.RFC3339),
		"timezone":  tz,
		"source":    source,
		"timestamp": franceTime.Unix(),
	})
}

// queryNTP queries an NTP server and returns the current UTC time.
func queryNTP(server string) (time.Time, error) {
	conn, err := net.DialTimeout("udp", server+":123", 2*time.Second)
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))

	req := make([]byte, 48)
	req[0] = 0x23

	if _, err := conn.Write(req); err != nil {
		return time.Time{}, err
	}

	resp := make([]byte, 48)
	if _, err := conn.Read(resp); err != nil {
		return time.Time{}, err
	}

	secs := binary.BigEndian.Uint32(resp[40:44])
	frac := binary.BigEndian.Uint32(resp[44:48])

	const ntpEpoch = 2208988800
	unixSecs := int64(secs) - ntpEpoch
	nanos := (int64(frac) * 1e9) >> 32

	return time.Unix(unixSecs, nanos).UTC(), nil
}

// queryHTTPTime fetches time from worldtimeapi.org as NTP fallback.
func queryHTTPTime(timezone string) (time.Time, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://worldtimeapi.org/api/timezone/" + timezone)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()

	var result struct {
		DateTime string `json:"datetime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return time.Time{}, err
	}

	t, err := time.Parse(time.RFC3339Nano, result.DateTime)
	if err != nil {
		// Try alternative format
		t, err = time.Parse("2006-01-02T15:04:05.999999-07:00", result.DateTime)
		if err != nil {
			return time.Time{}, err
		}
	}

	return t.UTC(), nil
}

// ---- File List & Select ----

type fileInfo struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Active   bool   `json:"active"`
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	cfg := config.Get()
	matches, _ := filepath.Glob(filepath.Join(cfg.WatchFolder, "*.xlsx"))

	s.mu.RLock()
	activeFile := s.activeFile
	s.mu.RUnlock()

	todayName := time.Now().Format("2006-01-02") + ".xlsx"

	// If no file is explicitly selected yet, default to today's file.
	if activeFile == "" {
		activeFile = todayName
	} else {
		activeFile = filepath.Base(activeFile)
	}

	type fileEntry struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Size        int64  `json:"size"`
		Modified    string `json:"modified"`
		Active      bool   `json:"active"`
		IsToday     bool   `json:"is_today"`
	}

	files := make([]fileEntry, 0, len(matches))
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		name := filepath.Base(m)
		if strings.HasPrefix(name, "~$") || strings.HasPrefix(name, ".") {
			continue
		}

		// Build a human-readable display name from the YYYY-MM-DD.xlsx format.
		displayName := name
		nameWithoutExt := strings.TrimSuffix(name, ".xlsx")
		if t, err := time.Parse("2006-01-02", nameWithoutExt); err == nil {
			// Format: "Lundi 07 avril 2026"
			displayName = t.Format("2 January 2006")
		}

		files = append(files, fileEntry{
			Name:        name,
			DisplayName: displayName,
			Size:        info.Size(),
			Modified:    info.ModTime().Format("02/01/2006 15:04"),
			Active:      name == activeFile,
			IsToday:     name == todayName,
		})
	}

	// Sort by filename descending (YYYY-MM-DD → newest first).
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name > files[j].Name
	})

	if len(files) > 20 {
		files = files[:20]
	}

	json.NewEncoder(w).Encode(files)
}

func (s *Server) handleFileSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, `{"error":"provide 'name' in JSON body"}`, http.StatusBadRequest)
		return
	}

	cfg := config.Get()
	// SECURITY: Sanitize filename to prevent path traversal
	safeName := filepath.Base(req.Name)
	fullPath := filepath.Join(cfg.WatchFolder, safeName)

	// Verify file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, `{"error":"file not found"}`, http.StatusNotFound)
		return
	}

	stats, err := excel.Parse(fullPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"parse error: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	s.UpdateStats(stats)
	// Auto-discover any technicians present in the selected file.
	if s.onSyncTechs != nil {
		s.onSyncTechs(stats)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"file":   safeName,
		"stats":  stats,
	})
}

// SetJobFunctions registers named job functions that can be triggered via the test endpoint.
func (s *Server) SetJobFunctions(jobs map[string]func()) {
	s.mu.Lock()
	s.jobFunctions = jobs
	s.mu.Unlock()
}

// handleTestFireJobs fires one or more scheduled jobs immediately for testing.
// POST /api/test/fire-jobs  {"jobs": ["morning", "stats", "eod"]}
// If "jobs" is empty or not provided, fires all registered jobs.
func (s *Server) handleTestFireJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if os.Getenv("DEBUG_MODE") != "true" {
		if !requireAdmin(w, r) {
			return
		}
	}

	var req struct {
		Jobs []string `json:"jobs"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	s.mu.RLock()
	jobs := s.jobFunctions
	wa := s.waClient
	s.mu.RUnlock()

	if wa == nil || !wa.IsConnected() {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "WhatsApp not connected — cannot send test messages",
		})
		return
	}

	if jobs == nil || len(jobs) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "no job functions registered",
		})
		return
	}

	results := make(map[string]string)

	// If no specific jobs requested, fire all
	if len(req.Jobs) == 0 {
		for name := range jobs {
			req.Jobs = append(req.Jobs, name)
		}
	}

	for _, jobName := range req.Jobs {
		fn, ok := jobs[jobName]
		if !ok {
			results[jobName] = "not found"
			continue
		}
		log.Printf("[test] 🧪 Firing job: %s", jobName)
		fn()
		results[jobName] = "fired"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"results": results,
	})
}
