package ratelimit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAllowUnderLimitThenBlock(t *testing.T) {
	s := New()
	p := Rate("t", 3, time.Minute)
	for i := 0; i < 3; i++ {
		if ok, _ := s.Allow(context.Background(), "k", p); !ok {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	ok, retry := s.Allow(context.Background(), "k", p)
	if ok {
		t.Fatal("4th request should be blocked")
	}
	if retry <= 0 {
		t.Fatalf("blocked request should report a positive retry-after, got %v", retry)
	}
}

func TestPerKeyIsolation(t *testing.T) {
	s := New()
	p := Rate("t", 1, time.Minute)
	if ok, _ := s.Allow(context.Background(), "a", p); !ok {
		t.Fatal("key a first request should pass")
	}
	if ok, _ := s.Allow(context.Background(), "a", p); ok {
		t.Fatal("key a second request should be blocked")
	}
	// A different key is independent.
	if ok, _ := s.Allow(context.Background(), "b", p); !ok {
		t.Fatal("key b should be unaffected by key a")
	}
}

func TestRefillOverTime(t *testing.T) {
	st := newMemStore()
	p := Rate("t", 2, time.Second) // 2 tokens/sec
	base := time.Unix(1_000_000, 0)
	// drain
	for i := 0; i < 2; i++ {
		if ok, _, _ := st.Take("k", p, base); !ok {
			t.Fatalf("drain %d should pass", i)
		}
	}
	if ok, _, _ := st.Take("k", p, base); ok {
		t.Fatal("should be empty after draining")
	}
	// 600ms later → ~1.2 tokens refilled → one request allowed again.
	if ok, _, _ := st.Take("k", p, base.Add(600*time.Millisecond)); !ok {
		t.Fatal("a token should have refilled after 600ms")
	}
}

func TestMiddlewareHeadersAnd429(t *testing.T) {
	s := New()
	p := Rate("api", 1, time.Minute)
	var served int
	h := s.Middleware(p, func(r *http.Request) string { return "fixed" })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { served++; w.WriteHeader(200) }),
	)

	// First request: 200 + headers present.
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, httptest.NewRequest("GET", "/", nil))
	if rec1.Code != 200 {
		t.Fatalf("first request code = %d", rec1.Code)
	}
	if rec1.Header().Get("X-RateLimit-Limit") != "1" {
		t.Errorf("missing X-RateLimit-Limit: %q", rec1.Header().Get("X-RateLimit-Limit"))
	}

	// Second request: 429 + Retry-After.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("GET", "/", nil))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request code = %d, want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Error("429 response missing Retry-After header")
	}
	if served != 1 {
		t.Fatalf("handler served %d times, want 1", served)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.7:54321"
	if got := ClientIP(r); got != "203.0.113.7" {
		t.Errorf("ClientIP(RemoteAddr) = %q", got)
	}
	r.Header.Set("X-Forwarded-For", "198.51.100.2, 10.0.0.1")
	if got := ClientIP(r); got != "198.51.100.2" {
		t.Errorf("ClientIP(XFF) = %q", got)
	}
}

func TestUnlimitedPolicy(t *testing.T) {
	s := New()
	p := Rate("none", 0, 0)
	for i := 0; i < 100; i++ {
		if ok, _ := s.Allow(context.Background(), "k", p); !ok {
			t.Fatal("zero-limit policy should allow everything")
		}
	}
}
