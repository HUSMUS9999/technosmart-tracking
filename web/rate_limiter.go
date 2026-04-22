package web

import (
	"net"
	"net/http"
	"sync"
	"time"
)

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// rateLimiter implements a per-IP token bucket rate limiter.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     time.Duration // time between allowed requests
	burst    int           // max burst size
}

type visitor struct {
	tokens    int
	lastSeen  time.Time
}

// newRateLimiter creates a rate limiter.
// rate = max requests per window, window = time window duration
func newRateLimiter(maxRequests int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     window / time.Duration(maxRequests),
		burst:    maxRequests,
	}

	// Cleanup stale entries every 10 minutes
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			rl.mu.Lock()
			for ip, v := range rl.visitors {
				if time.Since(v.lastSeen) > window*2 {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()

	return rl
}

// Allow checks if a request from the given IP is allowed.
func (rl *rateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &visitor{
			tokens:   rl.burst - 1,
			lastSeen: time.Now(),
		}
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := time.Since(v.lastSeen)
	refill := int(elapsed / rl.rate)
	if refill > 0 {
		v.tokens += refill
		if v.tokens > rl.burst {
			v.tokens = rl.burst
		}
		v.lastSeen = time.Now()
	}

	if v.tokens > 0 {
		v.tokens--
		return true
	}

	return false
}

// rateLimitMiddleware wraps a handler with rate limiting.
func rateLimitMiddleware(rl *rateLimiter, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		// Strip port from IP:port
		if host, _, err := splitHostPort(ip); err == nil {
			ip = host
		}
		// Trust X-Forwarded-For only for requests coming from a local proxy
		if isPrivateIP(ip) || ip == "localhost" {
			if forwarded := r.Header.Get("X-Real-IP"); forwarded != "" {
				ip = forwarded
			} else if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				// Take the first IP (client)
				if idx := indexOf(forwarded, ','); idx > 0 {
					ip = forwarded[:idx]
				} else {
					ip = forwarded
				}
			}
		}

		if !rl.Allow(ip) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "60")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"too many requests — please try again later"}`))
			return
		}

		next(w, r)
	}
}

// splitHostPort is a helper to avoid importing net just for this.
func splitHostPort(hostport string) (string, string, error) {
	// Find last colon
	for i := len(hostport) - 1; i >= 0; i-- {
		if hostport[i] == ':' {
			return hostport[:i], hostport[i+1:], nil
		}
	}
	return hostport, "", nil
}

// indexOf returns the index of the first occurrence of c in s, or -1.
func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
