package whatsapp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/skip2/go-qrcode"
	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// Client wraps whatsmeow for WhatsApp Web multi-device linking.
type Client struct {
	mu          sync.RWMutex
	client      *whatsmeow.Client
	container   *sqlstore.Container
	status      string
	qrCode      string    // latest QR code string (updated by background goroutine)
	qrUpdatedAt time.Time // when the latest QR was received
	qrActive    bool      // whether a QR linking session is in progress
	connected   bool
	phoneNumber string
	logs        []MessageLog
	dbPath      string
}

// MessageLog records a sent message attempt.
type MessageLog struct {
	To      string `json:"to"`
	Message string `json:"message"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// New creates a new WhatsApp client backed by SQLite.
func New(dbPath string) (*Client, error) {
	if dbPath == "" {
		dbPath = "whatsapp.db"
	}

	c := &Client{
		dbPath: dbPath,
		status: "initializing",
		logs:   make([]MessageLog, 0),
	}

	if err := c.init(); err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Client) init() error {
	container, err := sqlstore.New(
		context.Background(),
		"sqlite3",
		"file:"+c.dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on",
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create DB: %w", err)
	}
	c.container = container

	deviceStore, err := container.GetFirstDevice(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get device: %w", err)
	}

	c.client = whatsmeow.NewClient(deviceStore, nil)
	c.client.AddEventHandler(c.handleEvent)

	// If already logged in from a previous session, auto-connect
	if c.client.Store.ID != nil {
		c.status = "connecting"
		if err := c.client.Connect(); err != nil {
			log.Printf("[whatsapp] Failed to reconnect: %v", err)
			c.status = "disconnected"
		} else {
			c.status = "connected"
			c.connected = true
			c.phoneNumber = c.client.Store.ID.User
			log.Printf("[whatsapp] Reconnected as %s", c.phoneNumber)
		}
	} else {
		c.status = "not_linked"
		log.Println("[whatsapp] Not linked — scan QR from Settings page")
	}

	return nil
}

func (c *Client) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		c.mu.Lock()
		c.connected = true
		c.status = "connected"
		if c.client.Store.ID != nil {
			c.phoneNumber = c.client.Store.ID.User
		}
		c.qrActive = false
		c.qrCode = ""
		c.mu.Unlock()
		log.Printf("[whatsapp] ✅ Connected as %s", c.phoneNumber)

	case *events.Disconnected:
		c.mu.Lock()
		c.connected = false
		c.status = "disconnected"
		c.mu.Unlock()
		log.Println("[whatsapp] Disconnected")

	case *events.LoggedOut:
		c.mu.Lock()
		c.connected = false
		c.status = "logged_out"
		c.phoneNumber = ""
		c.qrActive = false
		c.qrCode = ""
		c.mu.Unlock()
		log.Println("[whatsapp] Logged out")

	case *events.StreamReplaced:
		c.mu.Lock()
		c.connected = false
		c.status = "stream_replaced"
		c.mu.Unlock()
		log.Println("[whatsapp] Stream replaced")

	case *events.PairSuccess:
		c.mu.Lock()
		c.connected = true
		c.status = "connected"
		c.phoneNumber = v.ID.User
		c.qrActive = false
		c.qrCode = ""
		c.mu.Unlock()
		log.Printf("[whatsapp] ✅ Pair success! Linked as %s", v.ID.User)

	case *events.Message:
		_ = v
	}
}

// IsConnected returns whether the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Status returns the current status string.
func (c *Client) Status() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// PhoneNumber returns the linked phone number.
func (c *Client) PhoneNumber() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.phoneNumber
}

// StartQRLogin starts the QR linking flow in the background.
// It gets the QR channel, connects, and continuously reads QR codes
// so they're always fresh when the frontend requests them.
func (c *Client) StartQRLogin() error {
	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return fmt.Errorf("already connected as %s", c.phoneNumber)
	}
	if c.qrActive {
		c.mu.Unlock()
		return nil // already running
	}
	c.qrActive = true
	c.qrCode = ""
	c.status = "waiting_qr"
	c.mu.Unlock()

	// IMPORTANT: GetQRChannel MUST be called BEFORE Connect()
	qrChan, err := c.client.GetQRChannel(context.Background())
	if err != nil {
		// If there's an error, the client might already be connected
		if c.client.Store.ID != nil {
			c.mu.Lock()
			c.qrActive = false
			c.mu.Unlock()
			return fmt.Errorf("already has session, try connecting directly")
		}
		c.mu.Lock()
		c.qrActive = false
		c.status = "error"
		c.mu.Unlock()
		return fmt.Errorf("failed to get QR channel: %w", err)
	}

	// Connect in background (this triggers QR code generation)
	go func() {
		err := c.client.Connect()
		if err != nil {
			log.Printf("[whatsapp] Connect error: %v", err)
			c.mu.Lock()
			c.qrActive = false
			c.status = "error"
			c.mu.Unlock()
		}
	}()

	// Background goroutine: read ALL QR events from the channel
	// This is critical — whatsmeow sends new QR codes every ~20s
	// and they MUST be consumed or the channel blocks
	go func() {
		log.Println("[whatsapp] QR login session started — waiting for scan...")
		for evt := range qrChan {
			switch evt.Event {
			case "code":
				c.mu.Lock()
				c.qrCode = evt.Code
				c.qrUpdatedAt = time.Now()
				c.status = "scanning"
				c.mu.Unlock()
				log.Printf("[whatsapp] 📱 New QR code ready (scan it now!)")

			case "success":
				c.mu.Lock()
				c.qrActive = false
				c.qrCode = ""
				c.status = "connected"
				c.mu.Unlock()
				log.Println("[whatsapp] ✅ QR scan successful — paired!")
				return

			case "timeout":
				c.mu.Lock()
				c.qrActive = false
				c.qrCode = ""
				c.status = "qr_timeout"
				c.mu.Unlock()
				log.Println("[whatsapp] ⏰ QR session timed out — click 'Lier WhatsApp' again")
				return

			default:
				log.Printf("[whatsapp] QR event: %s", evt.Event)
			}
		}

		// Channel closed
		c.mu.Lock()
		c.qrActive = false
		c.mu.Unlock()
		log.Println("[whatsapp] QR channel closed")
	}()

	return nil
}

// GetQRCode returns the latest QR code as PNG bytes.
// Call StartQRLogin() first to begin the flow.
func (c *Client) GetQRCode() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.connected {
		return nil, fmt.Errorf("already connected as %s", c.phoneNumber)
	}

	if c.qrCode == "" {
		if c.qrActive {
			return nil, fmt.Errorf("QR code generating... try again in 2 seconds")
		}
		return nil, fmt.Errorf("no active QR session — click 'Lier WhatsApp'")
	}

	// Check if QR is stale (older than 25s — whatsmeow refreshes every ~20s)
	if time.Since(c.qrUpdatedAt) > 25*time.Second {
		return nil, fmt.Errorf("QR code expired — click 'Lier WhatsApp' to refresh")
	}

	png, err := qrcode.Encode(c.qrCode, qrcode.Medium, 256)
	if err != nil {
		return nil, fmt.Errorf("QR encode error: %w", err)
	}

	return png, nil
}

// SendMessage sends a text message to a phone number.
func (c *Client) SendMessage(phoneNumber, message string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := MessageLog{
		To:      phoneNumber,
		Message: message,
	}

	if !c.connected || c.client == nil {
		entry.Success = false
		entry.Error = "WhatsApp not connected"
		c.logs = append(c.logs, entry)
		log.Printf("[whatsapp] (offline) Would send to %s: %.50s...", phoneNumber, message)
		return fmt.Errorf("WhatsApp not connected")
	}

	jid := types.NewJID(phoneNumber, types.DefaultUserServer)

	resp, err := c.client.SendMessage(context.Background(), jid, &waE2E.Message{
		Conversation: proto.String(message),
	})
	if err != nil {
		entry.Success = false
		entry.Error = err.Error()
		c.logs = append(c.logs, entry)
		log.Printf("[whatsapp] Send error to %s: %v", phoneNumber, err)
		return err
	}

	entry.Success = true
	c.logs = append(c.logs, entry)
	log.Printf("[whatsapp] Sent to %s (id=%s): %.50s...", phoneNumber, resp.ID, message)
	return nil
}

// Logout disconnects and clears the session.
func (c *Client) Logout() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.Logout(context.Background())
		c.client.Disconnect()
	}
	c.connected = false
	c.status = "logged_out"
	c.phoneNumber = ""
	c.qrCode = ""
	c.qrActive = false
	log.Println("[whatsapp] Logged out and session cleared")
}

// IsReady checks if the client can send messages.
func (c *Client) IsReady() bool {
	return c.IsConnected()
}

// GetLogs returns recent message logs.
func (c *Client) GetLogs() []MessageLog {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]MessageLog, len(c.logs))
	copy(result, c.logs)
	return result
}

// Disconnect cleanly shuts down the client.
func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		c.client.Disconnect()
	}
}
