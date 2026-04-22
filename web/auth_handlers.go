package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"fiber-tracker/internal/auth"
	"fiber-tracker/internal/config"
)

// ---- Auth API ----

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	c := config.Get()
	if c.ZitadelPAT == "" || c.OIDCIssuerURL == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "Authentication service not configured (Zitadel required)"})
		return
	}

	err := auth.HeadlessOIDCVerify(req.Email, req.Password, c.OIDCIssuerURL, c.ZitadelPAT)
	if err != nil {
		// Fallback to local DB auth (for admins injected directly into Postgres)
		if s.authStore != nil {
			if token, authErr := s.authStore.Authenticate(req.Email, req.Password); authErr == nil {
				log.Printf("[LocalDB] Auth success for injected user: %s", req.Email)
				auth.SetSessionCookie(w, token)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
				return
			}
		}

		log.Printf("[Zitadel/LocalDB] Auth failed for %s", req.Email)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"error": "Email ou mot de passe incorrect"})
		return
	}

	// Password verified by Zitadel — create local session
	log.Printf("[Zitadel] Auth success for %s", req.Email)
	if s.authStore != nil {
		localUser, err := s.authStore.GetUserByEmail(req.Email)
		if err != nil {
			// Auto-create local user for Zitadel-verified accounts
			newUser, cerr := s.authStore.CreateUser(req.Email, req.Email, "zitadel-managed", "admin")
			if cerr == nil {
				localUser = newUser
			}
		}

		if localUser != nil {
			sessionToken, err := s.authStore.CreateSessionForOIDC(localUser.ID)
			if err == nil {
				auth.SetSessionCookie(w, sessionToken)
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
				return
			}
		}
	}

	// Zitadel verified but no local session store — still OK
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// OIDC Login Handler: Redirects the user to Zitadel Authorization URL
func (s *Server) handleOIDCLogin(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	conf := s.oauth2Config
	s.mu.RUnlock()

	if conf.ClientID == "" {
		http.Error(w, "OIDC is not configured on this server.", http.StatusInternalServerError)
		return
	}

	// Generate a random state. In a purely robust implementation, you would store this in a cookie.
	state := auth.GenerateSecureState()
	auth.SetStateCookie(w, state)

	// Redirect to the provider's consent page
	http.Redirect(w, r, conf.AuthCodeURL(state), http.StatusFound)
}

// OIDC Callback Handler: Exchanges the authorization code for a JWT
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	conf := s.oauth2Config
	verifier := s.oidcVerifier
	s.mu.RUnlock()

	if conf.ClientID == "" {
		http.Error(w, "OIDC is not configured.", http.StatusInternalServerError)
		return
	}

	expectedState, err := r.Cookie(auth.StateCookieName)
	if err != nil || r.URL.Query().Get("state") != expectedState.Value {
		http.Error(w, "Invalid OAuth state", http.StatusBadRequest)
		return
	}

	oauth2Token, err := conf.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "Failed to exchange authorization code: "+err.Error(), http.StatusInternalServerError)
		return
	}

	rawIDToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "No ID token found in oauth response", http.StatusInternalServerError)
		return
	}

	idToken, err := verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		http.Error(w, "Failed to verify ID token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "Failed to parse claims: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Fetch or Create the user in our local database so we bind the OIDC user to our system!
	if s.authStore != nil {
		email := claims.Email
		if email == "" {
			email = claims.PreferredUsername // Fallback to preferred username if email isn't provided
		}

		user, err := s.authStore.GetUserByEmail(email)
		if err != nil {
			// Auto register OIDC user!
			user, err = s.authStore.CreateUser(email, claims.Name, "unusable_oidc_pass", "admin")
			if err != nil {
				log.Printf("Failed auto-registering OIDC user: %v", err)
			}
		}

		if user != nil {
			// Generate session natively
			token, err := s.authStore.CreateSessionForOIDC(user.ID)
			if err == nil {
				auth.SetSessionCookie(w, token)
			}
		}
	}

	// Redirect securely back to dashboard
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if s.authStore != nil {
		cookie, err := r.Cookie("ft_session")
		if err == nil {
			s.authStore.DestroySession(cookie.Value)
		}
	}
	auth.ClearSessionCookie(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if s.authStore == nil {
		json.NewEncoder(w).Encode(map[string]string{"email": "admin", "role": "admin"})
		return
	}

	cookie, err := r.Cookie("ft_session")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "not authenticated"})
		return
	}

	user, err := s.authStore.ValidateSession(cookie.Value)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "session expired"})
		return
	}

	json.NewEncoder(w).Encode(user)
}

func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		http.Error(w, `{"error":"provide email"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	cfg := config.Get()
	if cfg.ZitadelPAT == "" || cfg.OIDCIssuerURL == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "Zitadel non configuré"})
		return
	}

	// 1. Search for user by email in Zitadel
	searchQuery := map[string]interface{}{
		"queries": []map[string]interface{}{
			{
				"emailQuery": map[string]interface{}{
					"emailAddress": req.Email,
					"method":       "TEXT_QUERY_METHOD_EQUALS",
				},
			},
		},
	}
	bodyBytes, _ := json.Marshal(searchQuery)
	searchReq, _ := http.NewRequest("POST", cfg.OIDCIssuerURL+"/v2/users", bytes.NewReader(bodyBytes))
	searchReq.Header.Set("Content-Type", "application/json")
	searchReq.Header.Set("Authorization", "Bearer "+cfg.ZitadelPAT)

	resp, err := http.DefaultClient.Do(searchReq)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "Impossible de joindre Zitadel"})
		return
	}
	defer resp.Body.Close()

	var searchResult struct {
		Result []struct {
			UserId string `json:"userId"`
		} `json:"result"`
	}
	searchBody, _ := io.ReadAll(resp.Body)
	json.Unmarshal(searchBody, &searchResult)

	if len(searchResult.Result) == 0 {
		// Don't reveal if email exists or not for security
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		return
	}

	userId := searchResult.Result[0].UserId

	// 2. Trigger password reset in Zitadel and get the code directly
	resetTrigger := map[string]interface{}{
		"returnCode": map[string]interface{}{},
	}
	resetBodyBytes, _ := json.Marshal(resetTrigger)
	resetReq, _ := http.NewRequest("POST", cfg.OIDCIssuerURL+"/v2/users/"+userId+"/password_reset", bytes.NewReader(resetBodyBytes))
	resetReq.Header.Set("Content-Type", "application/json")
	resetReq.Header.Set("Authorization", "Bearer "+cfg.ZitadelPAT)

	resetResp, err := http.DefaultClient.Do(resetReq)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "Impossible de joindre Zitadel"})
		return
	}
	defer resetResp.Body.Close()

	if resetResp.StatusCode != 200 {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) // silently fail for security
		return
	}

	var resetResult struct {
		VerificationCode string `json:"verificationCode"`
	}
	resetRespBody, _ := io.ReadAll(resetResp.Body)
	json.Unmarshal(resetRespBody, &resetResult)

	if resetResult.VerificationCode != "" && s.mailer != nil {
		baseURL := fmt.Sprintf("http://localhost:%d", cfg.WebPort)
		if strings.HasPrefix(cfg.OIDCRedirectURI, "http") {
			parts := strings.Split(cfg.OIDCRedirectURI, "/")
			if len(parts) >= 3 {
				baseURL = parts[0] + "//" + parts[2]
			}
		}
		resetLink := fmt.Sprintf("%s/login?token=%s&user_id=%s", baseURL, resetResult.VerificationCode, userId)
		
		// Send the beautifully branded email locally!
		go s.mailer.SendResetEmail(req.Email, baseURL, resetLink)
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Token    string `json:"token"`
		UserId   string `json:"userId"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" || req.UserId == "" || req.Password == "" {
		http.Error(w, `{"error":"provide token, userId and password"}`, http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	cfg := config.Get()
	if cfg.ZitadelPAT == "" || cfg.OIDCIssuerURL == "" {
		json.NewEncoder(w).Encode(map[string]string{"error": "Zitadel non configuré"})
		return
	}

	// Change password in Zitadel
	resetData := map[string]interface{}{
		"verificationCode": req.Token,
		"newPassword": map[string]interface{}{
			"password": req.Password,
		},
	}
	bodyBytes, _ := json.Marshal(resetData)
	resetReq, _ := http.NewRequest("POST", cfg.OIDCIssuerURL+"/v2/users/"+req.UserId+"/password", bytes.NewReader(bodyBytes))
	resetReq.Header.Set("Content-Type", "application/json")
	resetReq.Header.Set("Authorization", "Bearer "+cfg.ZitadelPAT)

	resp, err := http.DefaultClient.Do(resetReq)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"error": "Impossible de joindre Zitadel"})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp struct {
			Message string `json:"message"`
		}
		respBody, _ := io.ReadAll(resp.Body)
		json.Unmarshal(respBody, &errResp)
		// Return specific error
		errMsg := errResp.Message
		if errMsg == "" {
			errMsg = "Code invalide ou expiré"
		}
		json.NewEncoder(w).Encode(map[string]string{"error": "Zitadel: " + errMsg})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
