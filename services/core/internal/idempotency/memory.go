// SPDX-License-Identifier: AGPL-3.0-only

// Package idempotency provides replay protection for at-least-once clients.
package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"
)

var (
	// ErrFingerprintConflict means a key was reused with a different request.
	ErrFingerprintConflict = errors.New("idempotency key reused with a different request")
)

// Result is the transport-neutral result cached for a completed mutation.
type Result struct {
	Status  int
	Value   json.RawMessage
	Headers map[string]string
}

// Scope is the durable uniqueness boundary for one mutation key.
type Scope struct {
	TenantID string
	ActorID  string
	Route    string
	Key      string
}

func (scope Scope) identity() string {
	return scope.TenantID + "|" + scope.ActorID + "|" + scope.Route + "|" + scope.Key
}

// Store coordinates at-most-once execution and exact response replay.
type Store interface {
	Do(context.Context, Scope, string, func() Result) (Result, bool, error)
}

type entry struct {
	fingerprint string
	createdAt   time.Time
	done        chan struct{}
	result      Result
	aborted     bool
}

// MemoryStore coordinates concurrent duplicate requests and caches their
// business result. Entries expire lazily; durable deployments should replace it
// with a shared implementation that preserves the same semantics.
type MemoryStore struct {
	mu      sync.Mutex
	entries map[string]*entry
	ttl     time.Duration
	now     func() time.Time
}

// NewMemoryStore returns an idempotency store with the requested retention.
func NewMemoryStore(ttl time.Duration) *MemoryStore {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &MemoryStore{
		entries: make(map[string]*entry),
		ttl:     ttl,
		now:     time.Now,
	}
}

// Do executes operation once for a scope and fingerprint. Concurrent requests
// with the same pair wait for and replay the first result. Reusing the scope
// with a different fingerprint fails without executing operation.
func (s *MemoryStore) Do(
	ctx context.Context,
	scope Scope,
	fingerprint string,
	operation func() Result,
) (Result, bool, error) {
	identity := scope.identity()
	for {
		s.mu.Lock()
		s.removeExpiredLocked()
		if existing, ok := s.entries[identity]; ok {
			if existing.fingerprint != fingerprint {
				s.mu.Unlock()
				return Result{}, false, ErrFingerprintConflict
			}
			done := existing.done
			s.mu.Unlock()
			select {
			case <-done:
				s.mu.Lock()
				if existing.aborted {
					s.mu.Unlock()
					continue
				}
				result := cloneResult(existing.result)
				s.mu.Unlock()
				return result, true, nil
			case <-ctx.Done():
				return Result{}, false, ctx.Err()
			}
		}

		owned := &entry{
			fingerprint: fingerprint,
			createdAt:   s.now().UTC(),
			done:        make(chan struct{}),
		}
		s.entries[identity] = owned
		s.mu.Unlock()

		result := s.executeOwned(identity, owned, operation)
		return cloneResult(result), false, nil
	}
}

func (s *MemoryStore) executeOwned(scope string, owned *entry, operation func() Result) (result Result) {
	completed := false
	defer func() {
		if completed {
			return
		}
		s.mu.Lock()
		if current, exists := s.entries[scope]; exists && current == owned {
			owned.aborted = true
			delete(s.entries, scope)
			close(owned.done)
		}
		s.mu.Unlock()
	}()

	result = operation()
	s.mu.Lock()
	owned.result = cloneResult(result)
	close(owned.done)
	s.mu.Unlock()
	completed = true
	return result
}

func (s *MemoryStore) removeExpiredLocked() {
	cutoff := s.now().UTC().Add(-s.ttl)
	for key, value := range s.entries {
		select {
		case <-value.done:
			if value.createdAt.Before(cutoff) {
				delete(s.entries, key)
			}
		default:
		}
	}
}

func cloneResult(result Result) Result {
	if result.Headers == nil {
		return result
	}
	clone := make(map[string]string, len(result.Headers))
	for key, value := range result.Headers {
		clone[key] = value
	}
	result.Headers = clone
	return result
}
