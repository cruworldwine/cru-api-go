package cru

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

// Every fixture in these tests is invented: fake LWINs, names, codes and ids.

func TestMain(m *testing.M) {
	for _, name := range []string{"CRU_CLIENT_KEY", "CRU_CLIENT_SECRET", "CRU_ACCESS_TOKEN", "CRU_MARKET", "CRU_ENVIRONMENT", "CRU_BASE_URL"} {
		if err := os.Unsetenv(name); err != nil {
			panic(err)
		}
	}
	os.Exit(m.Run())
}

type sleepLog struct {
	mu sync.Mutex
	d  []time.Duration
}

func (l *sleepLog) all() []time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]time.Duration(nil), l.d...)
}

// newClient returns a client for s made with opts, whose retries are logged rather than waited.
func newClient(t *testing.T, s *testkit.Server, opts ...Option) (*Client, *sleepLog) {
	t.Helper()
	c, err := NewClient(append([]Option{WithBaseURL(s.BaseURL())}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	log := &sleepLog{}
	c.tr.Sleep = func(ctx context.Context, d time.Duration) error {
		log.mu.Lock()
		log.d = append(log.d, d)
		log.mu.Unlock()
		return ctx.Err()
	}
	return c, log
}

func partner(t *testing.T, s *testkit.Server, opts ...Option) (*Client, *sleepLog) {
	return newClient(t, s, append([]Option{WithClientKey("ck_test", "cs_test")}, opts...)...)
}

func app(t *testing.T, s *testkit.Server, opts ...Option) (*Client, *sleepLog) {
	return newClient(t, s, append([]Option{WithAccessToken("at_test")}, opts...)...)
}
