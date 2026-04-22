package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// ZitadelSessionResponse represents the Zitadel v2 Session API response.
type ZitadelSessionResponse struct {
	Details struct {
		Sequence      string `json:"sequence"`
		ChangeDate    string `json:"changeDate"`
		ResourceOwner string `json:"resourceOwner"`
	} `json:"details"`
	SessionID    string `json:"sessionId"`
	SessionToken string `json:"sessionToken"`
}

// ZitadelErrorResponse represents an error from the Zitadel API.
type ZitadelErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// HeadlessOIDCVerify authenticates a user against Zitadel using the Session API (v2).
// This is the official Zitadel approach for custom login UIs:
// https://zitadel.com/docs/guides/integrate/login-ui/username-password
//
// It uses a service account PAT (Personal Access Token) with IAM_OWNER role
// to create a session with user+password checks in a single API call.
// No browser redirect, no HTML scraping — pure API.
func HeadlessOIDCVerify(email, password, issuerURL, serviceAccountPAT string) error {
	// Build the Session API request per Zitadel docs
	reqBody := map[string]interface{}{
		"checks": map[string]interface{}{
			"user": map[string]interface{}{
				"loginName": email,
			},
			"password": map[string]interface{}{
				"password": password,
			},
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	sessionURL := fmt.Sprintf("%s/v2/sessions", issuerURL)
	req, err := http.NewRequest("POST", sessionURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+serviceAccountPAT)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach Zitadel: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		var errResp ZitadelErrorResponse
		json.Unmarshal(respBody, &errResp)
		return fmt.Errorf("authentication failed: %s", errResp.Message)
	}

	var sessionResp ZitadelSessionResponse
	if err := json.Unmarshal(respBody, &sessionResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if sessionResp.SessionID == "" {
		return fmt.Errorf("authentication failed: no session created")
	}

	// Session created successfully — user's password is valid
	return nil
}

