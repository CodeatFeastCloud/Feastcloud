// SPDX-License-Identifier: AGPL-3.0-only

package idempotency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore serializes equal scopes with a transaction advisory lock and
// durably stores the exact operation value and headers for restart-safe replay.
type PostgresStore struct {
	pool *pgxpool.Pool
	ttl  time.Duration
	now  func() time.Time
}

func NewPostgresStore(ctx context.Context, databaseURL string, ttl time.Duration) (*PostgresStore, error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres idempotency: parse configuration: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("postgres idempotency: create pool: %w", err)
	}
	return &PostgresStore{pool: pool, ttl: ttl, now: time.Now}, nil
}

func (store *PostgresStore) Close() { store.pool.Close() }

func (store *PostgresStore) Do(ctx context.Context, scope Scope, fingerprint string, operation func() Result) (Result, bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Result{}, false, fmt.Errorf("postgres idempotency: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id',$1,true)`, scope.TenantID); err != nil {
		return Result{}, false, fmt.Errorf("postgres idempotency: establish tenant: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, scope.identity()); err != nil {
		return Result{}, false, fmt.Errorf("postgres idempotency: lock scope: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM idempotency_records WHERE tenant_id=$1 AND actor_id=$2 AND route=$3 AND idempotency_key=$4 AND expires_at <= $5`, scope.TenantID, scope.ActorID, scope.Route, scope.Key, store.now().UTC()); err != nil {
		return Result{}, false, fmt.Errorf("postgres idempotency: expire record: %w", err)
	}
	var storedFingerprint, body []byte
	var status int
	var headersRaw []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,response_status,response_headers,response_body FROM idempotency_records WHERE tenant_id=$1 AND actor_id=$2 AND route=$3 AND idempotency_key=$4 AND state='completed'`, scope.TenantID, scope.ActorID, scope.Route, scope.Key).Scan(&storedFingerprint, &status, &headersRaw, &body)
	if err == nil {
		if !bytes.Equal(storedFingerprint, []byte(fingerprint)) {
			return Result{}, false, ErrFingerprintConflict
		}
		headers := map[string]string{}
		if len(headersRaw) > 0 {
			if err := json.Unmarshal(headersRaw, &headers); err != nil {
				return Result{}, false, fmt.Errorf("postgres idempotency: decode headers: %w", err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return Result{}, false, fmt.Errorf("postgres idempotency: commit replay: %w", err)
		}
		return Result{Status: status, Value: append([]byte(nil), body...), Headers: headers}, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, false, fmt.Errorf("postgres idempotency: inspect record: %w", err)
	}

	result := operation()
	headersRaw, err = json.Marshal(result.Headers)
	if err != nil {
		return Result{}, false, fmt.Errorf("postgres idempotency: encode headers: %w", err)
	}
	now := store.now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO idempotency_records (tenant_id,actor_id,route,idempotency_key,request_hash,state,response_status,response_headers,response_body,created_at,completed_at,expires_at) VALUES ($1,$2,$3,$4,$5,'completed',$6,$7::jsonb,$8,$9,$9,$10)`, scope.TenantID, scope.ActorID, scope.Route, scope.Key, []byte(fingerprint), result.Status, headersRaw, []byte(result.Value), now, now.Add(store.ttl))
	if err != nil {
		return Result{}, false, fmt.Errorf("postgres idempotency: persist response: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, false, fmt.Errorf("postgres idempotency: commit response: %w", err)
	}
	return cloneResult(result), false, nil
}

var _ Store = (*PostgresStore)(nil)
