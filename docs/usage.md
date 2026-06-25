# ratelimit — usage

## Define a policy
`ratelimit.Rate(name, limit, window)` — at most `limit` requests per `window`
(also the burst size). Resolve the service with `ratelimit.FromKernel(k)`.

## Middleware
```go
s, _ := ratelimit.FromKernel(k)
api := s.Middleware(ratelimit.Rate("api", 60, time.Minute), nil) // 60/min per IP
r.With(api).Get("/things", handler)
```
Custom key (user/email/route/tenant):
```go
s.Middleware(ratelimit.Rate("login", 5, 15*time.Minute), func(r *http.Request) string {
    return r.FormValue("email")
})
```
Responses set `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`;
denied requests return `429` + `Retry-After`.

## Direct check
```go
allowed, retryAfter := s.Allow(ctx, key, ratelimit.Rate("export", 10, time.Hour))
```

## Distributed backend
The default store is in-memory (per instance). Implement the `Store` interface
(`Take(key, policy, now) (allowed, remaining, reset)`) backed by Redis (via the
cache plugin) and attach it with `s.WithStore(store)` for multi-instance limits.
