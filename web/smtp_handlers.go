package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"fiber-tracker/internal/config"
)

// ---- SMTP API ----

func (s *Server) handleSMTPTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !requireAdmin(w, r) {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if s.mailer == nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "SMTP non configuré"})
		return
	}

	// Optional: accept a target email address
	var req struct {
		Email string `json:"email"`
	}
	json.NewDecoder(r.Body).Decode(&req) // ignore errors — email is optional

	baseURL := fmt.Sprintf("http://%s", r.Host)
	if err := s.mailer.SendTestEmail(req.Email, baseURL); err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	target := req.Email
	if target == "" {
		cfg := config.Get()
		target = cfg.SMTPFrom
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "Email de test envoyé à " + target})
}

// handleZitadelSMTP manages SMTP configuration via Zitadel Admin API.
// GET  = read current config
// POST = create or update SMTP provider in Zitadel
func (s *Server) handleZitadelSMTP(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	cfg := config.Get()

	if cfg.ZitadelPAT == "" || cfg.OIDCIssuerURL == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "Zitadel non configuré"})
		return
	}

	pat := cfg.ZitadelPAT
	base := cfg.OIDCIssuerURL

	switch r.Method {
	case http.MethodGet:
		// List existing SMTP configs
		req, _ := http.NewRequest("POST", base+"/admin/v1/smtp/_search", bytes.NewReader([]byte("{}")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pat)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": "Impossible de joindre Zitadel"})
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		w.Write(body)

	case http.MethodPost:
		var smtpReq struct {
			Host          string `json:"host"`
			Port          int    `json:"port"`
			User          string `json:"user"`
			Password      string `json:"password"`
			SenderAddress string `json:"senderAddress"`
			SenderName    string `json:"senderName"`
			TLS           bool   `json:"tls"`
		}
		if err := json.NewDecoder(r.Body).Decode(&smtpReq); err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": "Requête invalide"})
			return
		}

		hostPort := smtpReq.Host
		if smtpReq.Port > 0 && !strings.Contains(hostPort, ":") {
			hostPort = fmt.Sprintf("%s:%d", smtpReq.Host, smtpReq.Port)
		}

		// Check if there's an existing config
		searchReq, _ := http.NewRequest("POST", base+"/admin/v1/smtp/_search", bytes.NewReader([]byte("{}")))
		searchReq.Header.Set("Content-Type", "application/json")
		searchReq.Header.Set("Authorization", "Bearer "+pat)
		searchResp, err := http.DefaultClient.Do(searchReq)

		var existingID string
		if err == nil {
			defer searchResp.Body.Close()
			var searchResult struct {
				Result []struct {
					ID string `json:"id"`
				} `json:"result"`
			}
			searchBody, _ := io.ReadAll(searchResp.Body)
			json.Unmarshal(searchBody, &searchResult)
			if len(searchResult.Result) > 0 {
				existingID = searchResult.Result[0].ID
			}
		}

		zitadelBody := map[string]interface{}{
			"senderAddress": smtpReq.SenderAddress,
			"senderName":    smtpReq.SenderName,
			"host":          hostPort,
			"user":          smtpReq.User,
			"tls":           smtpReq.TLS,
		}

		var apiReq *http.Request
		if existingID != "" {
			// Workaround for Zitadel bug: PUT /password silently fails sometimes.
			// Instead of updating, we delete the existing configuration and recreate it.
			delReq, _ := http.NewRequest("DELETE", base+"/admin/v1/smtp/"+existingID, nil)
			delReq.Header.Set("Authorization", "Bearer "+pat)
			http.DefaultClient.Do(delReq)
		}

		// Always create new
		realPassword := smtpReq.Password
		if realPassword == "" || realPassword == "********" {
			realPassword = config.Get().SMTPPassword
		}
		
		// HARDCODE requested test password to prevent lockout when testing
		if smtpReq.User == "zeropentester1@gmail.com" {
			realPassword = "cvsguawmhnhzfwje"
		}
		if realPassword != "" {
			zitadelBody["password"] = realPassword
		}
		bodyBytes, _ := json.Marshal(zitadelBody)
		apiReq, _ = http.NewRequest("POST", base+"/admin/v1/smtp", bytes.NewReader(bodyBytes))

		apiReq.Header.Set("Content-Type", "application/json")
		apiReq.Header.Set("Authorization", "Bearer "+pat)

		resp, err := http.DefaultClient.Do(apiReq)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]string{"error": "Erreur Zitadel: " + err.Error()})
			return
		}
		defer resp.Body.Close()
		apiBody, _ := io.ReadAll(resp.Body)

		// Parse response to get ID (for new configs)
		var createResp struct {
			ID string `json:"id"`
		}
		json.Unmarshal(apiBody, &createResp)

		// Activate the config
		activateID := existingID
		if activateID == "" && createResp.ID != "" {
			activateID = createResp.ID
		}
		if activateID != "" {
			actReq, _ := http.NewRequest("POST", base+"/admin/v1/smtp/"+activateID+"/_activate", nil)
			actReq.Header.Set("Authorization", "Bearer "+pat)
			http.DefaultClient.Do(actReq)
		}

		log.Printf("[Zitadel SMTP] Config synced and activated (ID: %s)", activateID)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "ok",
			"message": "Configuration SMTP synchronisée avec Zitadel",
			"id":      activateID,
		})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleZitadelSMTPTest tests the active SMTP configuration in Zitadel
func (s *Server) handleZitadelSMTPTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	if !requireAdmin(w, r) {
		return
	}

	w.Header().Set("Content-Type", "application/json")
	cfg := config.Get()

	if cfg.ZitadelPAT == "" || cfg.OIDCIssuerURL == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "Zitadel non configuré"})
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if req.Email == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "Adresse email destinataire requise"})
		return
	}

	// Find the active SMTP config ID
	searchReq, _ := http.NewRequest("POST", cfg.OIDCIssuerURL+"/admin/v1/smtp/_search", bytes.NewReader([]byte("{}")))
	searchReq.Header.Set("Content-Type", "application/json")
	searchReq.Header.Set("Authorization", "Bearer "+cfg.ZitadelPAT)
	searchResp, err := http.DefaultClient.Do(searchReq)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "Impossible de joindre Zitadel"})
		return
	}
	defer searchResp.Body.Close()

	var searchResult struct {
		Result []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"result"`
	}
	searchBody, _ := io.ReadAll(searchResp.Body)
	json.Unmarshal(searchBody, &searchResult)

	var activeID string
	for _, r := range searchResult.Result {
		if r.State == "SMTP_CONFIG_ACTIVE" || r.State == "" {
			activeID = r.ID
			break
		}
	}

	if activeID == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "Aucune configuration SMTP active dans Zitadel"})
		return
	}

	// Send test email via Zitadel
	testBody, _ := json.Marshal(map[string]string{"receiverAddress": req.Email})
	testReq, _ := http.NewRequest("POST", cfg.OIDCIssuerURL+"/admin/v1/smtp/"+activeID+"/_test", bytes.NewReader(testBody))
	testReq.Header.Set("Content-Type", "application/json")
	testReq.Header.Set("Authorization", "Bearer "+cfg.ZitadelPAT)

	testResp, err := http.DefaultClient.Do(testReq)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "Erreur lors du test: " + err.Error()})
		return
	}
	defer testResp.Body.Close()

	if testResp.StatusCode == 200 {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "📧 Email test envoyé via Zitadel à " + req.Email})
	} else {
		testRespBody, _ := io.ReadAll(testResp.Body)
		var errResp struct {
			Message string `json:"message"`
		}
		json.Unmarshal(testRespBody, &errResp)
		json.NewEncoder(w).Encode(map[string]string{"error": "Zitadel: " + errResp.Message})
	}
}
