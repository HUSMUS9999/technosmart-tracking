package web

import (
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

	"fiber-tracker/internal/config"
	"fiber-tracker/internal/excel"
	"fiber-tracker/internal/models"
	"fiber-tracker/internal/whatsapp"
)

//go:embed static/*
var staticFS embed.FS

// Server is the HTTP server for the web dashboard.
type Server struct {
	port         int
	latestStats  *models.DailyStats
	activeFile   string // currently active Excel file name
	notifications []models.NotificationLog
	mu           sync.RWMutex
	notifID      int
	onNewFile    func(path string) // callback when file uploaded
	waClient     *whatsapp.Client  // WhatsApp client reference
}

// New creates a new web server.
func New(port int) *Server {
	return &Server{
		port:          port,
		notifications: make([]models.NotificationLog, 0),
	}
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

// Start begins serving the web dashboard.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/notifications", s.handleNotifications)
	mux.HandleFunc("/api/upload", s.handleUpload)
	mux.HandleFunc("/api/parse", s.handleParse)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/time", s.handleTime)
	mux.HandleFunc("/api/files", s.handleFiles)
	mux.HandleFunc("/api/files/select", s.handleFileSelect)
	mux.HandleFunc("/api/whatsapp/status", s.handleWAStatus)
	mux.HandleFunc("/api/whatsapp/qr", s.handleWAQR)
	mux.HandleFunc("/api/whatsapp/logout", s.handleWALogout)
	mux.HandleFunc("/api/whatsapp/send", s.handleWASend)

	// Static files (embedded)
	mux.HandleFunc("/", s.handleStatic)

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("[web] Dashboard available at http://localhost%s", addr)

	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Fatalf("[web] Server error: %v", err)
		}
	}()

	return nil
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
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

	// Set content type based on extension
	switch filepath.Ext(path) {
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".woff2":
		w.Header().Set("Content-Type", "font/woff2")
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

	switch r.Method {
	case http.MethodGet:
		cfg := config.Get()
		json.NewEncoder(w).Encode(cfg)

	case http.MethodPut:
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if err := config.Save(&cfg); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "saved"})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
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

	r.ParseMultipartForm(32 << 20) // 32MB
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `{"error":"no file provided"}`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	cfg := config.Get()
	destPath := filepath.Join(cfg.WatchFolder, header.Filename)

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

	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		http.Error(w, `{"error":"provide 'path' in JSON body"}`, http.StatusBadRequest)
		return
	}

	stats, err := excel.Parse(req.Path)
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
	cfg := config.Get()
	matches, _ := filepath.Glob(filepath.Join(cfg.WatchFolder, "*.xlsx"))

	s.mu.RLock()
	activeFile := s.activeFile
	s.mu.RUnlock()

	files := make([]fileInfo, 0, len(matches))
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		name := filepath.Base(m)
		// Skip temp files
		if strings.HasPrefix(name, "~$") || strings.HasPrefix(name, ".") {
			continue
		}
		files = append(files, fileInfo{
			Name:     name,
			Size:     info.Size(),
			Modified: info.ModTime().Format("02/01/2006 15:04"),
			Active:   name == activeFile,
		})
	}

	// Sort by modification time (newest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Modified > files[j].Modified
	})

	w.Header().Set("Content-Type", "application/json")
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
	fullPath := filepath.Join(cfg.WatchFolder, req.Name)

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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"file":   req.Name,
		"stats":  stats,
	})
}

// ---- WhatsApp API ----

func (s *Server) handleWAStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	wa := s.waClient
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")

	if wa == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
			"status":    "not_initialized",
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"connected": wa.IsConnected(),
		"status":    wa.Status(),
		"phone":     wa.PhoneNumber(),
	})
}

func (s *Server) handleWAQR(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	wa := s.waClient
	s.mu.RUnlock()

	if wa == nil {
		http.Error(w, `{"error":"WhatsApp not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	// Start the QR login session (no-op if already active)
	if err := wa.StartQRLogin(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  err.Error(),
			"status": wa.Status(),
		})
		return
	}

	// Give the background goroutine a moment to receive the first QR code
	for i := 0; i < 10; i++ {
		qrPNG, err := wa.GetQRCode()
		if err == nil {
			w.Header().Set("Content-Type", "image/png")
			w.Write(qrPNG)
			return
		}
		// If connected meanwhile, report success
		if wa.IsConnected() {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "connected",
				"phone":  wa.PhoneNumber(),
			})
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Timed out waiting for QR
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":  "QR code not available yet — try again",
		"status": wa.Status(),
	})
}

func (s *Server) handleWALogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	wa := s.waClient
	s.mu.RUnlock()

	if wa == nil {
		http.Error(w, `{"error":"WhatsApp not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	wa.Logout()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "logged_out"})
}

func (s *Server) handleWASend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		To      string `json:"to"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	wa := s.waClient
	s.mu.RUnlock()

	if wa == nil {
		http.Error(w, `{"error":"WhatsApp not initialized"}`, http.StatusServiceUnavailable)
		return
	}

	if err := wa.SendMessage(req.To, req.Message); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "sent"})
}
