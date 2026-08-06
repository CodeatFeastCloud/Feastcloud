// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/feastcloud/feastcloud/services/edge/internal/api"
	"github.com/feastcloud/feastcloud/services/edge/internal/store"
	"github.com/feastcloud/feastcloud/services/edge/internal/syncer"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("edge stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	repository, err := store.Open(config.databasePath)
	if err != nil {
		return err
	}
	defer repository.Close()

	var adapter syncer.Adapter
	if config.syncEndpoint != "" {
		client, err := cloudHTTPClient(config)
		if err != nil {
			return err
		}
		adapter, err = syncer.NewHTTPAdapter(syncer.HTTPAdapterConfig{
			Endpoint: config.syncEndpoint, BearerToken: config.cloudToken,
			TenantID: config.tenantID, ActorID: "edge:" + config.edgeID, Client: client,
		})
		if err != nil {
			return err
		}
	}
	coordinator := syncer.NewCoordinator(repository, adapter, logger, syncer.Config{
		EdgeID: config.edgeID, TenantID: config.tenantID, OutletID: config.outletID, Interval: config.syncInterval,
		BatchSize: config.syncBatchSize,
	})

	rootContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go coordinator.Run(rootContext)

	handler := api.NewServer(repository, logger, api.Config{
		Version: version, EdgeID: config.edgeID, TenantID: config.tenantID,
		OutletID: config.outletID, BearerToken: config.localToken,
		AllowedOrigin: config.allowedOrigin,
		SyncEnabled:   coordinator.Enabled(), MaxBodyBytes: config.maxBodyBytes,
	})
	server := &http.Server{
		Addr: config.listenAddress, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() {
		logger.Info("edge listening", "address", config.listenAddress, "edge_id", config.edgeID, "outlet_id", config.outletID, "sync_enabled", coordinator.Enabled())
		if config.localTLSCert != "" {
			serveErrors <- server.ListenAndServeTLS(config.localTLSCert, config.localTLSKey)
			return
		}
		serveErrors <- server.ListenAndServe()
	}()

	select {
	case <-rootContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

type runtimeConfig struct {
	listenAddress string
	databasePath  string
	edgeID        string
	tenantID      string
	outletID      string
	localToken    string
	allowedOrigin string
	localTLSCert  string
	localTLSKey   string
	syncEndpoint  string
	cloudToken    string
	cloudTLSCert  string
	cloudTLSKey   string
	cloudCA       string
	syncInterval  time.Duration
	syncBatchSize int
	maxBodyBytes  int64
}

func loadConfig() (runtimeConfig, error) {
	config := runtimeConfig{
		listenAddress: environment("FEASTCLOUD_EDGE_LISTEN", "127.0.0.1:8081"),
		databasePath:  environment("FEASTCLOUD_EDGE_DATABASE", filepath.Join("data", "edge.db")),
		edgeID:        strings.TrimSpace(os.Getenv("FEASTCLOUD_EDGE_ID")),
		tenantID:      strings.TrimSpace(os.Getenv("FEASTCLOUD_TENANT_ID")),
		outletID:      strings.TrimSpace(os.Getenv("FEASTCLOUD_OUTLET_ID")),
		localToken:    os.Getenv("FEASTCLOUD_EDGE_TOKEN"),
		allowedOrigin: environment("FEASTCLOUD_EDGE_ALLOWED_ORIGIN", "http://localhost:5173"),
		localTLSCert:  os.Getenv("FEASTCLOUD_EDGE_TLS_CERT"), localTLSKey: os.Getenv("FEASTCLOUD_EDGE_TLS_KEY"),
		cloudToken:   os.Getenv("FEASTCLOUD_CLOUD_TOKEN"),
		cloudTLSCert: os.Getenv("FEASTCLOUD_CLOUD_TLS_CERT"), cloudTLSKey: os.Getenv("FEASTCLOUD_CLOUD_TLS_KEY"),
		cloudCA: os.Getenv("FEASTCLOUD_CLOUD_CA"),
	}
	if config.edgeID == "" || config.tenantID == "" || config.outletID == "" {
		return runtimeConfig{}, errors.New("FEASTCLOUD_EDGE_ID, FEASTCLOUD_TENANT_ID, and FEASTCLOUD_OUTLET_ID are required")
	}
	if (config.localTLSCert == "") != (config.localTLSKey == "") {
		return runtimeConfig{}, errors.New("FEASTCLOUD_EDGE_TLS_CERT and FEASTCLOUD_EDGE_TLS_KEY must be configured together")
	}
	if (config.cloudTLSCert == "") != (config.cloudTLSKey == "") {
		return runtimeConfig{}, errors.New("FEASTCLOUD_CLOUD_TLS_CERT and FEASTCLOUD_CLOUD_TLS_KEY must be configured together")
	}
	if config.allowedOrigin == "*" {
		return runtimeConfig{}, errors.New("FEASTCLOUD_EDGE_ALLOWED_ORIGIN must be an exact origin, not a wildcard")
	}
	if config.allowedOrigin != "" {
		origin, err := url.Parse(config.allowedOrigin)
		if err != nil || origin.Scheme == "" || origin.Host == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
			return runtimeConfig{}, errors.New("FEASTCLOUD_EDGE_ALLOWED_ORIGIN must be one exact scheme-and-host origin")
		}
	}
	host, _, err := net.SplitHostPort(config.listenAddress)
	if err != nil {
		return runtimeConfig{}, fmt.Errorf("invalid FEASTCLOUD_EDGE_LISTEN: %w", err)
	}
	if !isLoopback(host) && (config.localToken == "" || config.localTLSCert == "") {
		return runtimeConfig{}, errors.New("non-loopback listeners require FEASTCLOUD_EDGE_TOKEN and local TLS certificate/key")
	}

	config.syncEndpoint = strings.TrimSpace(os.Getenv("FEASTCLOUD_SYNC_ENDPOINT"))
	if config.syncEndpoint == "" {
		if base := strings.TrimSpace(os.Getenv("FEASTCLOUD_CLOUD_URL")); base != "" {
			config.syncEndpoint = strings.TrimRight(base, "/") + "/api/v1/sync/operations"
		}
	}
	if config.syncEndpoint != "" {
		parsed, err := url.Parse(config.syncEndpoint)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return runtimeConfig{}, errors.New("FEASTCLOUD_SYNC_ENDPOINT must be an absolute HTTP(S) URL")
		}
		if parsed.Scheme != "https" && !isLoopback(parsed.Hostname()) {
			return runtimeConfig{}, errors.New("remote cloud synchronization requires HTTPS")
		}
	}
	config.syncInterval, err = durationEnvironment("FEASTCLOUD_SYNC_INTERVAL", 5*time.Second)
	if err != nil {
		return runtimeConfig{}, err
	}
	config.syncBatchSize, err = integerEnvironment("FEASTCLOUD_SYNC_BATCH_SIZE", 100, 1, 500)
	if err != nil {
		return runtimeConfig{}, err
	}
	maxBody, err := integerEnvironment("FEASTCLOUD_EDGE_MAX_BODY_BYTES", 1<<20, 1_024, 5<<20)
	if err != nil {
		return runtimeConfig{}, err
	}
	config.maxBodyBytes = int64(maxBody)
	return config, nil
}

func cloudHTTPClient(config runtimeConfig) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	if config.cloudTLSCert != "" {
		certificate, err := tls.LoadX509KeyPair(config.cloudTLSCert, config.cloudTLSKey)
		if err != nil {
			return nil, fmt.Errorf("load cloud client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	if config.cloudCA != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system certificate roots: %w", err)
		}
		certificate, err := os.ReadFile(config.cloudCA)
		if err != nil {
			return nil, fmt.Errorf("read cloud CA: %w", err)
		}
		if !roots.AppendCertsFromPEM(certificate) {
			return nil, errors.New("FEASTCLOUD_CLOUD_CA contains no valid certificates")
		}
		tlsConfig.RootCAs = roots
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, nil
}

func isLoopback(host string) bool {
	if host == "localhost" || host == "" {
		return true
	}
	address := net.ParseIP(strings.Trim(host, "[]"))
	return address != nil && address.IsLoopback()
}

func environment(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func durationEnvironment(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive Go duration", name)
	}
	return parsed, nil
}

func integerEnvironment(name string, fallback, minimum, maximum int) (int, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		return 0, fmt.Errorf("%s must be between %d and %d", name, minimum, maximum)
	}
	return parsed, nil
}
