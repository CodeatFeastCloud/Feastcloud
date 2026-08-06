// SPDX-License-Identifier: AGPL-3.0-only

package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidPairingCode = errors.New("pairing code is invalid or expired")
var ErrInvalidSession = errors.New("local session is invalid, expired, or revoked")

func (store *Store) CreatePairingCode(ctx context.Context, codeHash, role string, expiresAt time.Time) error {
	now := store.now().UTC()
	_, err := store.db.ExecContext(ctx, `INSERT INTO pairing_codes(code_hash,role,created_at,expires_at) VALUES(?,?,?,?)`, codeHash, role, now.Format(time.RFC3339Nano), expiresAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: create pairing code: %w", err)
	}
	return nil
}

func (store *Store) ExchangePairingCode(ctx context.Context, codeHash, tokenHash string, expiresAt time.Time) (string, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := store.now().UTC()
	var role, encodedExpiry string
	err = tx.QueryRowContext(ctx, `SELECT role,expires_at FROM pairing_codes WHERE code_hash=? AND consumed_at IS NULL`, codeHash).Scan(&role, &encodedExpiry)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidPairingCode
	}
	if err != nil {
		return "", fmt.Errorf("store: read pairing code: %w", err)
	}
	pairingExpiry, err := time.Parse(time.RFC3339Nano, encodedExpiry)
	if err != nil || !now.Before(pairingExpiry) {
		return "", ErrInvalidPairingCode
	}
	result, err := tx.ExecContext(ctx, `UPDATE pairing_codes SET consumed_at=? WHERE code_hash=? AND consumed_at IS NULL`, now.Format(time.RFC3339Nano), codeHash)
	if err != nil {
		return "", fmt.Errorf("store: consume pairing code: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return "", ErrInvalidPairingCode
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO local_sessions(token_hash,role,created_at,expires_at) VALUES(?,?,?,?)`, tokenHash, role, now.Format(time.RFC3339Nano), expiresAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return "", fmt.Errorf("store: create local session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("store: commit local session: %w", err)
	}
	return role, nil
}

func (store *Store) AuthenticateSession(ctx context.Context, tokenHash string) (string, error) {
	var role, encodedExpiry string
	err := store.db.QueryRowContext(ctx, `SELECT role,expires_at FROM local_sessions WHERE token_hash=? AND revoked_at IS NULL`, tokenHash).Scan(&role, &encodedExpiry)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidSession
	}
	if err != nil {
		return "", fmt.Errorf("store: authenticate local session: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, encodedExpiry)
	if err != nil || !store.now().UTC().Before(expiresAt) {
		return "", ErrInvalidSession
	}
	return role, nil
}

func (store *Store) RevokeSession(ctx context.Context, tokenHash string) error {
	result, err := store.db.ExecContext(ctx, `UPDATE local_sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, store.now().UTC().Format(time.RFC3339Nano), tokenHash)
	if err != nil {
		return fmt.Errorf("store: revoke local session: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalidSession
	}
	return nil
}
