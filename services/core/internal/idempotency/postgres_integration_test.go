// SPDX-License-Identifier: AGPL-3.0-only

package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestPostgresStoreReplaysResponseAfterRestart(t *testing.T) {
	databaseURL := os.Getenv("FEASTCLOUD_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set FEASTCLOUD_TEST_DATABASE_URL to run PostgreSQL integration coverage")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	scope := Scope{TenantID: "11111111-1111-4111-8111-111111111111", ActorID: "idempotency-integration", Route: "POST /api/v1/orders", Key: "restart-" + time.Now().UTC().Format("20060102150405.000000000")}
	store, err := NewPostgresStore(ctx, databaseURL, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	expected := Result{Status: 201, Value: json.RawMessage(`{"data":{"id":"durable"}}`), Headers: map[string]string{"Location": "/api/v1/orders/durable"}}
	result, replayed, err := store.Do(ctx, scope, "fingerprint-one", func() Result { calls.Add(1); return expected })
	if err != nil || replayed || result.Status != 201 {
		t.Fatalf("first result=%#v replay=%t err=%v", result, replayed, err)
	}
	store.Close()
	restarted, err := NewPostgresStore(ctx, databaseURL, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	result, replayed, err = restarted.Do(ctx, scope, "fingerprint-one", func() Result { calls.Add(1); return Result{} })
	if err != nil || !replayed || string(result.Value) != string(expected.Value) || result.Headers["Location"] != expected.Headers["Location"] {
		t.Fatalf("restart result=%#v replay=%t err=%v", result, replayed, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("operation calls=%d; want 1", calls.Load())
	}
	_, _, err = restarted.Do(ctx, scope, "fingerprint-two", func() Result { t.Fatal("conflicting request executed"); return Result{} })
	if !errors.Is(err, ErrFingerprintConflict) {
		t.Fatalf("conflict error=%v", err)
	}
}
