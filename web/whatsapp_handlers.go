package web

import (
	"encoding/json"
	"net/http"
	"time"
)

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
