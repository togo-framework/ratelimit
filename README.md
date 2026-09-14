<div align="center">
  <picture><source media="(prefers-color-scheme: dark)" srcset=".github/assets/togo-mark-dark.svg" /><img src=".github/assets/togo-mark.svg" alt="ToGO" height="64" /></picture>
  <h1>togo-framework/ratelimit</h1>
  <p>
    <a href="https://to-go.dev/marketplace"><img src="https://img.shields.io/badge/marketplace-to--go.dev-1F8A99" alt="marketplace" /></a>
    <a href="https://pkg.go.dev/github.com/togo-framework/ratelimit"><img src="https://pkg.go.dev/badge/github.com/togo-framework/ratelimit.svg" alt="pkg.go.dev" /></a>
    <img src="https://img.shields.io/badge/license-MIT-blue" alt="MIT" />
  </p>
  <p><strong>Request rate limiting & throttling for <a href="https://to-go.dev">togo</a> — token-bucket, per key, with standard headers.</strong></p>
</div>

## Install

```bash
togo install togo-framework/ratelimit
```

A token-bucket limiter: each **key** (IP, user, route, or anything you choose) gets a bucket of `Limit` tokens that refills continuously over `Window`. A request consumes one token; when empty the request is denied with a `Retry-After` hint and a `429`. Use it as HTTP middleware or call `Allow` directly.

## Usage

### As middleware

```go
import (
	"time"
	"github.com/togo-framework/ratelimit"
)

s, _ := ratelimit.FromKernel(k)

// 60 requests/minute per client IP
api := s.Middleware(ratelimit.Rate("api", 60, time.Minute), nil)
r.With(api).Get("/things", listThings)

// 5 login attempts / 15 min, keyed by submitted email
login := s.Middleware(ratelimit.Rate("login", 5, 15*time.Minute), func(req *http.Request) string {
	return req.FormValue("email")
})
r.With(login).Post("/login", doLogin)
```

Responses carry the standard headers — `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` — and a denied request gets `429 Too Many Requests` + `Retry-After`.

### Direct check

```go
allowed, retryAfter := s.Allow(ctx, userID, ratelimit.Rate("export", 10, time.Hour))
if !allowed {
	return fmt.Errorf("rate limited, retry in %s", retryAfter)
}
```

### Key strategies

`ratelimit.ClientIP` (default, honors `X-Forwarded-For`), or any `KeyFunc(*http.Request) string` — key by user id, API key, route, tenant, etc.

## Configuration

No required env. The default store is **in-memory** (per instance). For multi-instance deployments implement the small `Store` interface (one method, `Take`) backed by Redis via `togo install togo-framework/cache-redis`, and attach it with `s.WithStore(myStore)`.

---

<div align="center">
  <h3>Premium sponsors</h3>
  <p>
    <a href="https://id8media.com"><strong>ID8 Media</strong></a> &nbsp;·&nbsp;
    <a href="https://one-studio.co"><strong>One Studio</strong></a>
  </p>
  <p><sub>Support togo — <a href="https://github.com/sponsors/fadymondy">become a sponsor</a>.</sub></p>
</div>
