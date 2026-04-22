package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
)

// contextKey is an unexported type for context keys to prevent collisions.
type contextKey string

const userContextKey contextKey = "auth_user"

// ContextUser retrieves the authenticated user from the request context.
// Returns nil if no user is set (should not happen behind Middleware).
func ContextUser(r *http.Request) *User {
	if u, ok := r.Context().Value(userContextKey).(*User); ok {
		return u
	}
	return nil
}

const sessionCookieName = "ft_session"
const StateCookieName = "ft_oauth_state"

// GenerateSecureState creates a random secure string for OAuth State
func GenerateSecureState() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// SetStateCookie safely issues the OAuth 2.0 anti-CSRF challenge token
func SetStateCookie(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     StateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// Middleware returns an HTTP middleware that requires authentication.
// Public paths (login, forgot-password, static assets) are excluded.
func Middleware(store *Store, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check session cookie
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || cookie.Value == "" {
			handleUnauthorized(w, r)
			return
		}

		user, err := store.ValidateSession(cookie.Value)
		if err != nil {
			// Invalid/expired session — clear cookie
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteLaxMode,
			})
			handleUnauthorized(w, r)
			return
		}

		// Store authenticated user in request context (safe, not spoofable)
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}


func handleUnauthorized(w http.ResponseWriter, r *http.Request) {
	// API requests get JSON error
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"unauthorized","redirect":"/login"}`))
		return
	}
	// Page requests get redirected to login
	http.Redirect(w, r, "/login", http.StatusFound)
}

// SetSessionCookie sets the session cookie on the response.
func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   7 * 24 * 3600, // 7 days
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie clears the session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}
