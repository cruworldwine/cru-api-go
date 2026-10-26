# Authentication

The Cru Trading API answers on one host per market, `https://{market}.cruworldwine.com/api/v1`,
where market is `uk` (the default), `us`, `hk`, `sg`, `eu`, `ae` or `cn`. A credential works on its
own market only. The sandbox, `https://{market}.crusandbox.com/api/v1`, takes credentials issued
for it (`cru_test_...`); pick it with `WithEnvironment(cru.EnvironmentSandbox)` (see
[configuration](configuration.md#markets-and-the-sandbox)). There are two kinds of credential,
and they behave differently on writes.

```go
import "github.com/cruworldwine/cru-api-go/cru"
```

## Partner credential

Cru issues a partner credential: a key (`cru_live_...`) and a secret, which is shown once. Keep
both out of source control. The client sends them as the `CLIENT-KEY` and `CLIENT-SECRET`
headers.

```go
client, err := cru.NewClient(cru.WithClientKey(key, secret), cru.WithMarket("uk"))
```

A partner credential's writes run at once: `Write.Result` holds what each one did. Every partner
write carries a `reference`, its idempotency key; the SDK generates one when you leave it nil and
sends the same one on every retry, so a retry can never act twice (see
[writes and confirmations](writes-and-confirmations.md)).

## Connected app (OAuth 2.0)

An app that acts for a Cru customer gets their tokens with the OAuth 2.0 authorization code flow
with PKCE (S256), at `/api/oauth/authorize` and `/api/oauth/token` on the market's host. The SDK
sends the access token as `Authorization: Bearer`.

```go
client, err := cru.NewClient(
	cru.WithAccessToken(accessToken),
	cru.WithOAuthRefresh(refreshToken, clientID, clientSecret, func(t cru.Token) {
		save(t.AccessToken, t.RefreshToken) // refresh tokens rotate: store the new pair
	}),
)
```

With `WithOAuthRefresh`, a call answered 401 refreshes the access token once, at the market's
`/api/oauth/token`, and is retried with the new one. Concurrent calls rejected with the same
token share one refresh. A refresh token works once, so the callback must store every new pair;
it is called in order, while the client holds its token lock, and must not call the client.
`clientSecret` may be empty for a public client. A refresh the token endpoint refuses is an
`*cru.APIError` with status 401, `ErrAuthentication`, the OAuth error as `Code` (such as
`invalid_grant`) and the endpoint's status in `Details["token_status"]`.

A connected app's writes are staged: they answer 202 with a confirmation code, and nothing
happens until the customer agrees and you call `Confirmations.Confirm`.

## From the environment

Options left unset fall back to the environment, so `cru.NewClient()` alone works when these are
set:

| Variable | Used when |
|---|---|
| `CRU_CLIENT_KEY`, `CRU_CLIENT_SECRET` | no access token was given |
| `CRU_ACCESS_TOKEN` | no key pair was given |
| `CRU_MARKET` | no market was given |
| `CRU_ENVIRONMENT` | no environment was given |
| `CRU_BASE_URL` | no base URL was given |

A key pair wins over an access token. `NewClient` fails when it ends up with neither, with an
environment other than `live` or `sandbox`, or with a market that is not a market code: a market
can only ever pick a host under cruworldwine.com or crusandbox.com.

## Check the connection

```go
p, err := client.Ping(ctx)
if err != nil {
	log.Fatal(err)
}
fmt.Println(p.Client.Market, p.Client.Currency, p.Client.Scopes, p.Client.ExpiresAt)
```

## Keeping credentials safe

- The SDK never logs a credential or token (see [configuration](configuration.md#logging)), and
  a `*cru.Client` prints without them.
- It never follows a redirect to another host, or from https to http: net/http would forward
  the `CLIENT-KEY` and `CLIENT-SECRET` headers there.
- `WithRequestHeader` cannot set the credential headers. For another credential, create another
  client; clients are cheap, and can share one `*http.Client`.
