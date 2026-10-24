# Compatibility

## Built for an API that changes

The Cru Trading API adds fields and values without notice. The models are built so that a
change on the API's side never breaks a program built with an older SDK.

- **Unknown fields never fail decoding.** Every model keeps the JSON it was decoded from:
  `m.Raw()`, or `cru.Field(m, "new_field", &v)` for one field.

  ```go
  var colour string
  if found, err := cru.Field(bid, "colour", &colour); found && err == nil {
  	// a field this SDK does not know yet
  }
  ```

- **Fields with known values are plain strings.** The known values are constants
  (`cru.CurrencyGBP`, `cru.BidStatusLive`, `cru.ActionBidPlace`, ...), and any other value
  decodes as it is. Compare with the constants; do not assume the list is complete.
- **Nullable or optional fields are pointers**, nil for null or absent; `cru.Value(p)` reads one,
  or gives the zero value. Required, non-nullable fields are plain values. Lists are slices and
  free-form objects maps, nil when null or absent.
- **Numbers are tolerated.** `integer` is `int64` and `number` is `float64`. `1000.0` and
  `"1000"` decode into an `int64`, and `"12.5"` into a `float64`.
- **A value that does not fit is left unset**, never failing the answer; it is still in `Raw`.
  The rest of the model decodes.
- **Dates and times are strings**, as the API sends them (RFC 3339).

For a parameter or field the API has and this SDK does not know yet, `WithExtraQuery` and
`WithExtraBodyField` send it, and `client.Do` calls an endpoint the SDK does not have (see
[configuration](configuration.md)).

## Versioning

The module follows [semantic versioning](https://semver.org). While its major version is 0, a
minor version may change the API incompatibly; [CHANGELOG.md](../CHANGELOG.md) says what changed
in each version and what to change in your code. A patch version never breaks. Every pull
request's exported API is compared with the base branch, and an incompatible change needs the
`breaking-change` label and a CHANGELOG entry.

The latest minor version receives fixes, security fixes included ([SECURITY.md](../SECURITY.md)).

## Go versions

The SDK supports the two latest Go releases, and Go 1.23 as its floor. CI tests all of them.
It uses the standard library only.

## Import path

The package users import is `github.com/cruworldwine/cru-api-go/cru`. In v0.1.0 it was the
module root, `github.com/cruworldwine/cru-api-go`; the identifiers did not change, so moving
needs the import path changed and nothing else:

```go
import "github.com/cruworldwine/cru-api-go/cru"
```
