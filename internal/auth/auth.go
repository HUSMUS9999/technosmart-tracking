package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserExists         = errors.New("user already exists")
)

type User struct {
	ID        int       `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	db *sql.DB
}

func NewStore(connStr string) (*Store, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("open auth db: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		return nil, fmt.Errorf("migrate auth db: %w", err)
	}

	return store, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id SERIAL PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'viewer',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);
	`)
	return err
}

func (s *Store) HasUsers() bool {
	var count int
	s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	return count > 0
}

func (s *Store) CreateUser(email, name, password, role string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	var id int
	err = s.db.QueryRow(
		"INSERT INTO users (email, name, password_hash, role) VALUES ($1, $2, $3, $4) RETURNING id",
		email, name, string(hash), role,
	).Scan(&id)
	
	if err != nil {
		return nil, ErrUserExists
	}

	log.Printf("[auth] User created: %s (%s)", email, role)
	return &User{ID: id, Email: email, Name: name, Role: role}, nil
}

func (s *Store) Authenticate(email, password string) (string, error) {
	var id int
	var hash string
	err := s.db.QueryRow(
		"SELECT id, password_hash FROM users WHERE email = $1", email,
	).Scan(&id, &hash)
	if err != nil {
		return "", ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}

	token := generateToken(32)
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	_, err = s.db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)",
		token, id, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	s.db.Exec("DELETE FROM sessions WHERE expires_at < $1", time.Now())
	return token, nil
}

// CreateSessionForOIDC actively bypasses credential hash checks and creates a 7-day session token for external JWT integrations
func (s *Store) CreateSessionForOIDC(userID int) (string, error) {
	token := generateToken(32)
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	_, err := s.db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)",
		token, userID, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("create session for oidc: %w", err)
	}

	s.db.Exec("DELETE FROM sessions WHERE expires_at < $1", time.Now())
	return token, nil
}

func (s *Store) ValidateSession(token string) (*User, error) {
	var user User
	err := s.db.QueryRow(`
		SELECT u.id, u.email, u.name, u.role, u.created_at
		FROM sessions s JOIN users u ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > $2
	`, token, time.Now()).Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.CreatedAt)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	return &user, nil
}

func (s *Store) DestroySession(token string) {
	s.db.Exec("DELETE FROM sessions WHERE token = $1", token)
}

// NOTE: Password reset is handled by Zitadel — no local reset tokens needed.

// UpdatePassword re-hashes newPassword and saves it for the given email.
// Returns an error if the user does not exist.
func (s *Store) UpdatePassword(email, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	res, err := s.db.Exec(
		"UPDATE users SET password_hash = $1 WHERE email = $2",
		string(hash), email,
	)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("user not found: %s", email)
	}
	log.Printf("[auth] Password updated for %s", email)
	return nil
}

func (s *Store) GetUserByEmail(email string) (*User, error) {
	var user User
	err := s.db.QueryRow(
		"SELECT id, email, name, role, created_at FROM users WHERE email = $1", email,
	).Scan(&user.ID, &user.Email, &user.Name, &user.Role, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Store) Close() {
	s.db.Close()
}

func generateToken(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return hex.EncodeToString(b)
}
