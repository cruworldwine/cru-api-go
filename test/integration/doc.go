// Package integration holds black-box tests of the SDK, which reach it only through its public
// API, as a program importing github.com/cruworldwine/cru-api-go/cru does.
//
//   - TestReplayCaptures replays real API answers through the method that asks for each one. It
//     runs when CRU_CAPTURES names a capture run (a directory with captures/*.json), and is
//     skipped otherwise. Captures hold customer data: they stay out of the repository.
//   - TestLive calls the real API, read-only: Ping, Warehouses.List and Market.List. It runs only
//     when CRU_LIVE=1, with credentials in the environment (CRU_CLIENT_KEY and CRU_CLIENT_SECRET,
//     or CRU_ACCESS_TOKEN; CRU_MARKET or CRU_BASE_URL to pick the host).
//
// Run them with make integration.
package integration
