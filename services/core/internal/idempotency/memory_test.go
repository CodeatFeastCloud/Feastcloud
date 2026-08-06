// SPDX-License-Identifier: AGPL-3.0-only

package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoExecutesConcurrentDuplicatesOnce(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(time.Hour)
	var calls atomic.Int32
	operation := func() Result {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		return Result{Status: 201, Value: json.RawMessage(`"created"`)}
	}

	const workers = 8
	var wg sync.WaitGroup
	errorsFound := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, _, err := store.Do(context.Background(), Scope{TenantID: "tenant", ActorID: "actor", Route: "POST /route", Key: "key"}, "same", operation)
			if err != nil {
				errorsFound <- err
				return
			}
			if result.Status != 201 || string(result.Value) != `"created"` {
				errorsFound <- errors.New("unexpected replay result")
			}
		}()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("operation executed %d times; want 1", got)
	}
}

func TestDoRejectsFingerprintConflict(t *testing.T) {
	t.Parallel()

	store := NewMemoryStore(time.Hour)
	scope := Scope{TenantID: "tenant", ActorID: "actor", Route: "POST /route", Key: "key"}
	_, _, err := store.Do(context.Background(), scope, "first", func() Result {
		return Result{Status: 201}
	})
	if err != nil {
		t.Fatalf("first execution failed: %v", err)
	}

	_, _, err = store.Do(context.Background(), scope, "different", func() Result {
		t.Fatal("conflicting operation must not execute")
		return Result{}
	})
	if !errors.Is(err, ErrFingerprintConflict) {
		t.Fatalf("got %v; want ErrFingerprintConflict", err)
	}
}
