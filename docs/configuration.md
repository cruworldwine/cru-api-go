# Configuration

A `*cru.Client` is configured once, with options to `cru.NewClient`, and each call can change its
own behaviour with request options. A client is safe for concurrent use: create one per
credential and share it.

## Client options

| Option | Default | What it does |
|---|---|---|
| `WithClientKey(key, secret)` | `CRU_CLIENT_KEY`, `CRU_CLIENT_SECRET` | a partner credential |
| `WithAccessToken(token)` | `CRU_ACCESS_TOKEN` | a connected app's access token |
| `WithOAuthRefresh(refresh, id, secret, onRefresh)` | none | refresh an expired access token |
| `WithMarket(market)` | `CRU_MARKET`, else `uk` | the market's host |
| `WithEnvironment(env)` | `CRU_ENVIRONMENT`, else `live` | the live API or the sandbox |
| `WithBaseURL(url)` | `CRU_BASE_URL` | another base URL, ending in `/api/v1`; wins over the market and environment |
| `WithHTTPClient(hc)` | a client on `http.DefaultTransport` | the HTTP client requests go through |
| `WithTimeout(d)` | 30s | limit on each attempt; 0 is none |
| `WithMaxRetries(n)` | 2 | retries of a read or a partner write; 0 is none |
| `WithLogger(l)` | none | a `*slog.Logger` |
| `WithAppInfo(name, version, url)` | none | your application, in the User-Agent |

[Authentication](authentication.md) covers the credentials.

## Markets and the sandbox

The client calls one market's host in one environment:

| Environment | Base URL | For |
|---|---|---|
| `cru.EnvironmentLive`, the default | `https://{market}.cruworldwine.com/api/v1` | the real API |
| `cru.EnvironmentSandbox` | `https://{market}.crusandbox.com/api/v1` | for building and testing; no real money or wine changes hands |

The market is `uk` (the default), `us`, `hk`, `sg`, `eu`, `ae` or `cn`, the same in both. The
sandbox takes credentials issued for it, and a credential works on its own market only.
`WithEnvironment` and `CRU_ENVIRONMENT` take `live` or `sandbox` in any case; `NewClient` refuses
anything else, even with a base URL. A base URL wins over both the market and the environment.

```go
client, err := cru.NewClient(cru.WithClientKey(key, secret),
	cru.WithEnvironment(cru.EnvironmentSandbox), cru.WithMarket("sg"))
// client.BaseURL() is https://sg.crusandbox.com/api/v1
```

The SDK carries the table of markets, generated from the spec, so a program knows a market's
currency without a call:

```go
for _, m := range cru.Markets() { // uk first, in the spec's order
	fmt.Println(m.Code, m.Name, m.Currency, m.Symbol, m.URL(cru.EnvironmentSandbox))
}
hk, ok := cru.LookupMarket("HK") // in any case; ok is false for a code the table lacks
m, ok := client.MarketInfo()     // the client's market
```

`client.MarketInfo()` is the market the client was made for. With a base URL, it is the market
whose host that is, in either environment (`hk` for `https://hk.crusandbox.com/api/v1`); for
another host, the market `WithMarket` or `CRU_MARKET` gave; and none when neither names one. A
market code that is valid but not in the table still picks its host, so a new market needs no
new SDK release, but `MarketInfo` reports none for it.

## Request options

Every method takes trailing `cru.RequestOption`s, which change that call only:

```go
bid, err := client.Bids.Get(ctx, code,
	cru.WithRequestTimeout(5*time.Second),             // each attempt, in place of WithTimeout
	cru.WithRequestMaxRetries(0),                      // in place of WithMaxRetries
	cru.WithRequestHeader("traceparent", traceparent), // any header but the credentials
)
```

| Option | What it does |
|---|---|
| `WithRequestHeader(key, value)` | sends a header, replacing the SDK's; the credential headers, and headers net/http would refuse, fail the call |
| `WithRequestTimeout(d)` | limits each attempt of this call |
| `WithRequestMaxRetries(n)` | retries for this call; a connected app's writes are never retried |
| `WithExtraQuery(key, value)` | sends a query parameter this SDK version does not know, or replaces one |
| `WithExtraBodyField(key, value)` | sets a top-level body field; on a read, which sends no body, the call fails |
| `WithResponseInto(*ResponseMeta)` | reports the answer's status, headers, request id and attempts |

A call that a request option refuses fails before anything is sent.

### Request ids and `ResponseMeta`

Every answer carries a request id, which Cru support needs to find the call. An error has it in
`APIError.RequestID` and a write in `Write.RequestID`. For any call, `WithResponseInto` fills a
`cru.ResponseMeta` from the call's last answer, success or error:

```go
var meta cru.ResponseMeta
page, err := client.Portfolio.List(ctx, nil, cru.WithResponseInto(&meta))
log.Println(meta.StatusCode, meta.RequestID, meta.Attempts)
```

For a list walked with `Page.All`, it describes the last page fetched. Give each call its own.

## Timeouts

`WithTimeout` and `WithRequestTimeout` limit each attempt. To bound a whole call, retries and
their waits included, give it a context with a deadline. A cancelled context or a passed
deadline comes back as the context's error, and is never retried.

## Retries

Reads, and a partner credential's writes, are retried on network errors, attempt timeouts, 429,
500, 502, 503, 504 and 409 `REQUEST_IN_PROGRESS`. The wait is 0.5s, doubling to at most 8s, with
±25% jitter; or as long as the API says, in `errors[0].details.retry_after` or the `Retry-After`
header (seconds or an HTTP date), up to 30s. A connected app's writes are never retried: they
have no reference that would make a second attempt safe.

## Logging

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
client, err := cru.NewClient(cru.WithLogger(logger))
```

| Level | Message | Attributes |
|---|---|---|
| Debug | `cru: request`, each attempt | method, path, attempt, status, duration, request_id; error and timeout when no answer came |
| Info | `cru: retrying request` | method, path, the next attempt, wait, and the status and code or the error |
| Info | `cru: access token refreshed` or `refresh failed` | duration, the token endpoint's status and error code |

The client key and secret, tokens, query strings and request and response bodies are never
logged; a test proves it with a secret in each of those places. Without `WithLogger`, nothing is
logged.

## User-Agent

Every request says `cruworldwine-go/<version> go/<Go version> (<os>; <arch>)`. Name your
application after it, so Cru can tell its traffic apart:

```go
client, err := cru.NewClient(cru.WithAppInfo("cellar-sync", "2.1.0", "https://example.com/cellar-sync"))
// User-Agent: cruworldwine-go/0.2.0 go/1.24.2 (linux; amd64) cellar-sync/2.1.0 (https://example.com/cellar-sync)
```

## Calling an endpoint this SDK does not know

`client.Do(ctx, method, path, query, body, opts...)` calls any endpoint and returns the envelope
(`StatusCode`, `Header`, `Status`, `Message`, `Meta`, `Data`, `Body`), with the same errors,
retries, references and request options as every other call.
