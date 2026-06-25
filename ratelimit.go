// Package ratelimit provides request rate limiting / throttling for togo apps.
//
// It uses a token-bucket limiter: each key (an IP, user, route, or anything you
// choose) gets a bucket of Limit tokens that refills continuously over Window.
// A request consumes one token; when the bucket is empty the request is denied
// with a Retry-After hint. Use it as HTTP middleware or call Allow directly.
//
//	s, _ := ratelimit.FromKernel(k)
//	api := s.Middleware(ratelimit.Rate("api", 60, time.Minute), nil) // 60/min per IP
//	r.With(api).Get("/things", handler)
package ratelimit

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/togo-framework/togo"
)

// Policy is a rate limit: at most Limit requests per Window (also the burst).
type Policy struct {
	Name   string        `json:"name"`
	Limit  int           `json:"limit"`
	Window time.Duration `json:"window"`
}

// Rate builds a named policy: limit requests per window.
func Rate(name string, limit int, window time.Duration) Policy {
	return Policy{Name: name, Limit: limit, Window: window}
}

// Store holds per-key limiter state. The default is in-memory; implement this
// to back the limiter with Redis (via the cache plugin) for multi-instance use.
type Store interface {
	// Take attempts to consume one token for key under policy p at time now.
	// It returns whether the request is allowed, the tokens remaining, and the
	// time at which a token will next be available (the reset).
	Take(key string, p Policy, now time.Time) (allowed bool, remaining int, reset time.Time)
}

type bucket struct {
	tokens float64
	last   time.Time
}

type memStore struct {
	mu sync.Mutex
	b  map[string]*bucket
}

func newMemStore() *memStore { return &memStore{b: map[string]*bucket{}} }

func (m *memStore) Take(key string, p Policy, now time.Time) (bool, int, time.Time) {
	if p.Limit <= 0 || p.Window <= 0 {
		return true, p.Limit, now // unlimited / misconfigured → allow
	}
	rate := float64(p.Limit) / p.Window.Seconds() // tokens per second

	m.mu.Lock()
	defer m.mu.Unlock()
	bk := m.b[key]
	if bk == nil {
		bk = &bucket{tokens: float64(p.Limit), last: now}
		m.b[key] = bk
	}
	// Continuous refill since the last access, capped at the burst (Limit).
	if elapsed := now.Sub(bk.last).Seconds(); elapsed > 0 {
		bk.tokens += elapsed * rate
		if bk.tokens > float64(p.Limit) {
			bk.tokens = float64(p.Limit)
		}
		bk.last = now
	}
	if bk.tokens >= 1 {
		bk.tokens--
		// reset = when the bucket would be full again.
		full := (float64(p.Limit) - bk.tokens) / rate
		return true, int(bk.tokens), now.Add(time.Duration(full * float64(time.Second)))
	}
	// Denied: time until one token is available.
	need := (1 - bk.tokens) / rate
	return false, 0, now.Add(time.Duration(need * float64(time.Second)))
}

// Service is the rate-limit runtime stored on the kernel (k.Get("ratelimit")).
type Service struct {
	store    Store
	mu       sync.RWMutex
	policies map[string]Policy
}

func init() {
	togo.RegisterProviderFunc("ratelimit", togo.PriorityService, func(k *togo.Kernel) error {
		s := New()
		k.Set("ratelimit", s)
		return nil
	})
}

// New builds a Service with the in-memory store.
func New() *Service {
	return &Service{store: newMemStore(), policies: map[string]Policy{}}
}

// WithStore swaps the backing store (e.g. a Redis-backed one).
func (s *Service) WithStore(store Store) *Service { s.store = store; return s }

// FromKernel returns the rate-limit Service registered on the kernel.
func FromKernel(k *togo.Kernel) (*Service, bool) {
	v, ok := k.Get("ratelimit")
	if !ok {
		return nil, false
	}
	s, ok := v.(*Service)
	return s, ok
}

// Define registers a named policy so it can be referenced by name.
func (s *Service) Define(p Policy) {
	s.mu.Lock()
	s.policies[p.Name] = p
	s.mu.Unlock()
}

// Policy returns a previously defined policy by name.
func (s *Service) Policy(name string) (Policy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.policies[name]
	return p, ok
}

// Allow reports whether a request under key is permitted by policy p. When
// denied it returns the duration to wait before retrying.
func (s *Service) Allow(_ context.Context, key string, p Policy) (allowed bool, retryAfter time.Duration) {
	ok, _, reset := s.store.Take(p.Name+":"+key, p, time.Now())
	if ok {
		return true, 0
	}
	return false, time.Until(reset)
}

// KeyFunc extracts the rate-limit key from a request (default: client IP).
type KeyFunc func(r *http.Request) string

// ClientIP returns the best-effort client IP (honoring X-Forwarded-For).
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Middleware enforces policy p, keyed by keyFn (defaults to ClientIP). It sets
// the standard X-RateLimit-* headers and responds 429 with Retry-After when the
// limit is exceeded.
func (s *Service) Middleware(p Policy, keyFn KeyFunc) func(http.Handler) http.Handler {
	if keyFn == nil {
		keyFn = ClientIP
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, remaining, reset := s.store.Take(p.Name+":"+keyFn(r), p, time.Now())
			h := w.Header()
			h.Set("X-RateLimit-Limit", strconv.Itoa(p.Limit))
			if remaining < 0 {
				remaining = 0
			}
			h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
			if !allowed {
				retry := int(time.Until(reset).Seconds()) + 1
				h.Set("Retry-After", strconv.Itoa(retry))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
