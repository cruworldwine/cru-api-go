package photos

import (
	"context"
	"errors"
	"testing"

	"github.com/cruworldwine/cru-api-go/cru/core"
	"github.com/cruworldwine/cru-api-go/internal/testkit"
)

func TestEmptyID(t *testing.T) {
	s := testkit.NewServer(t, testkit.Always(200, ""))
	if _, err := newService(testkit.Partner(t, s)).Get(context.Background(), ""); !errors.Is(err, core.ErrMissingParameter) {
		t.Errorf("an empty id: %v", err)
	}
	if len(s.Requests()) != 0 {
		t.Error("a request was sent with an empty id")
	}
}
