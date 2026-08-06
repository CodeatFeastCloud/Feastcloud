// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/api"
	"github.com/feastcloud/feastcloud/services/core/internal/auth"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

var version = "dev"

func main() {
	address := flag.String("addr", ":8080", "HTTP listen address")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	var repository store.Repository = store.NewMemoryRepository()
	syncRepository := store.SyncRepository(store.NewUnavailableSyncRepository())
	requireSyncReady := false
	var postgresRepository *store.PostgresRepository
	var idempotencyStore idempotency.Store
	var postgresIdempotency *idempotency.PostgresStore
	if databaseURL := strings.TrimSpace(os.Getenv("FEASTCLOUD_DATABASE_URL")); databaseURL != "" {
		var err error
		postgresRepository, err = store.NewPostgresRepository(context.Background(), databaseURL)
		if err != nil {
			logger.Error("postgres repository configuration failed", "error", err)
			os.Exit(1)
		}
		defer postgresRepository.Close()
		repository = postgresRepository
		syncRepository = postgresRepository
		postgresIdempotency, err = idempotency.NewPostgresStore(context.Background(), databaseURL, 90*24*time.Hour)
		if err != nil {
			logger.Error("postgres idempotency configuration failed", "error", err)
			os.Exit(1)
		}
		defer postgresIdempotency.Close()
		idempotencyStore = postgresIdempotency
		requireSyncReady = true
	}
	authMode := strings.ToLower(strings.TrimSpace(os.Getenv("FEASTCLOUD_AUTH_MODE")))
	if authMode == "" {
		authMode = "demo"
	}
	var userAuthenticator auth.Authenticator = auth.DemoAuthenticator{}
	var deviceAuthenticator auth.Authenticator = auth.DemoAuthenticator{}
	if authMode == "oidc" {
		if postgresRepository == nil {
			logger.Error("oidc mode requires FEASTCLOUD_DATABASE_URL for the durable device registry")
			os.Exit(1)
		}
		var err error
		userAuthenticator, err = auth.NewOIDCAuthenticator(
			strings.TrimSpace(os.Getenv("FEASTCLOUD_OIDC_ISSUER")),
			strings.TrimSpace(os.Getenv("FEASTCLOUD_OIDC_AUDIENCE")),
			strings.TrimSpace(os.Getenv("FEASTCLOUD_OIDC_PUBLIC_KEY_FILE")),
		)
		if err != nil {
			logger.Error("oidc authentication configuration failed", "error", err)
			os.Exit(1)
		}
		deviceAuthenticator = auth.NewCertificateAuthenticator(postgresRepository)
	} else if authMode != "demo" {
		logger.Error("unsupported authentication mode", "mode", authMode)
		os.Exit(1)
	}
	var snapshotSigningKey ed25519.PrivateKey
	if keyFile := strings.TrimSpace(os.Getenv("FEASTCLOUD_SNAPSHOT_SIGNING_KEY_FILE")); keyFile != "" {
		raw, err := os.ReadFile(keyFile)
		if err != nil {
			logger.Error("snapshot signing key read failed", "error", err)
			os.Exit(1)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil {
			logger.Error("snapshot signing key must be base64", "error", err)
			os.Exit(1)
		}
		switch len(decoded) {
		case ed25519.SeedSize:
			snapshotSigningKey = ed25519.NewKeyFromSeed(decoded)
		case ed25519.PrivateKeySize:
			snapshotSigningKey = ed25519.PrivateKey(decoded)
		default:
			logger.Error("snapshot signing key has invalid length")
			os.Exit(1)
		}
	} else if authMode == "oidc" {
		logger.Error("oidc mode requires FEASTCLOUD_SNAPSHOT_SIGNING_KEY_FILE")
		os.Exit(1)
	}
	handler := api.NewServer(repository, logger, api.Config{
		Version: version, SyncRepository: syncRepository, RequireSyncReady: requireSyncReady, IdempotencyStore: idempotencyStore,
		UserAuthenticator: userAuthenticator, DeviceAuthenticator: deviceAuthenticator, AuthMode: authMode,
		AllowedOrigin:      strings.TrimSpace(os.Getenv("FEASTCLOUD_CORE_ALLOWED_ORIGIN")),
		SnapshotSigningKey: snapshotSigningKey,
	})
	server := &http.Server{
		Addr:              *address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	certificateFile := strings.TrimSpace(os.Getenv("FEASTCLOUD_CORE_TLS_CERT"))
	certificateKeyFile := strings.TrimSpace(os.Getenv("FEASTCLOUD_CORE_TLS_KEY"))
	if authMode == "oidc" {
		var err error
		server.TLSConfig, err = coreTLSConfig(strings.TrimSpace(os.Getenv("FEASTCLOUD_CORE_CLIENT_CA")))
		if err != nil || certificateFile == "" || certificateKeyFile == "" {
			logger.Error("oidc mode requires TLS certificate, key, and trusted device CA", "error", err)
			os.Exit(1)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("core service starting", "address", *address, "version", version)
		if certificateFile != "" || certificateKeyFile != "" {
			serverErrors <- server.ListenAndServeTLS(certificateFile, certificateKeyFile)
			return
		}
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("core service stopped unexpectedly", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		logger.Info("shutdown requested")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		if closeErr := server.Close(); closeErr != nil {
			logger.Error("forced shutdown failed", "error", closeErr)
		}
		os.Exit(1)
	}
	logger.Info("core service stopped")
}

func coreTLSConfig(clientCAFile string) (*tls.Config, error) {
	if clientCAFile == "" {
		return nil, errors.New("FEASTCLOUD_CORE_CLIENT_CA is required")
	}
	caBytes, err := os.ReadFile(clientCAFile)
	if err != nil {
		return nil, fmt.Errorf("read device CA: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caBytes) {
		return nil, errors.New("device CA contains no valid certificates")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.VerifyClientCertIfGiven,
		ClientCAs:  pool,
	}, nil
}
