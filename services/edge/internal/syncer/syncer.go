// SPDX-License-Identifier: AGPL-3.0-only

// Package syncer owns the configurable cloud transport. Local order and KDS
// handling never imports or depends on a concrete cloud client.
package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/feastcloud/feastcloud/services/edge/internal/model"
)

const (
	maximumProtocolOperations = 500
	maximumProtocolBytes      = 5 << 20
)

type Adapter interface {
	Push(context.Context, model.PushOperationsRequest) (model.PushOperationsResponse, error)
}

type Repository interface {
	PendingOperations(context.Context, int, time.Time) ([]model.Operation, error)
	RecordSyncResults(context.Context, []string, []model.PushResult, time.Time) error
	RecordSyncFailure(context.Context, []string, string, time.Time) error
}

type Config struct {
	EdgeID        string
	TenantID      string
	OutletID      string
	Interval      time.Duration
	BatchSize     int
	MaxBatchBytes int
}

type Coordinator struct {
	repository Repository
	adapter    Adapter
	logger     *slog.Logger
	config     Config
	now        func() time.Time
	runMu      sync.Mutex
}

func NewCoordinator(repository Repository, adapter Adapter, logger *slog.Logger, config Config) *Coordinator {
	if repository == nil {
		panic("syncer: repository is required")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if config.Interval <= 0 {
		config.Interval = 5 * time.Second
	}
	if config.BatchSize <= 0 || config.BatchSize > maximumProtocolOperations {
		config.BatchSize = 100
	}
	if config.MaxBatchBytes <= 0 || config.MaxBatchBytes > maximumProtocolBytes {
		config.MaxBatchBytes = maximumProtocolBytes
	}
	return &Coordinator{repository: repository, adapter: adapter, logger: logger, config: config, now: time.Now}
}

func (coordinator *Coordinator) Enabled() bool { return coordinator.adapter != nil }

// Run retries due outbox entries until context cancellation.
func (coordinator *Coordinator) Run(ctx context.Context) {
	if !coordinator.Enabled() {
		return
	}
	coordinator.runOnceLogged(ctx)
	ticker := time.NewTicker(coordinator.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			coordinator.runOnceLogged(ctx)
		}
	}
}

func (coordinator *Coordinator) runOnceLogged(ctx context.Context) {
	if err := coordinator.SyncOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
		coordinator.logger.WarnContext(ctx, "edge synchronization failed", "error", err)
	}
}

// SyncOnce sends at most one bounded batch. It is safe to call concurrently;
// concurrent callers are serialized so an operation is not actively pushed twice.
func (coordinator *Coordinator) SyncOnce(ctx context.Context) error {
	if !coordinator.Enabled() {
		return nil
	}
	coordinator.runMu.Lock()
	defer coordinator.runMu.Unlock()

	now := coordinator.now().UTC()
	operations, err := coordinator.repository.PendingOperations(ctx, coordinator.config.BatchSize, now)
	if err != nil {
		return err
	}
	operations, err = fitBatch(coordinator.config.EdgeID, coordinator.config.OutletID, operations, coordinator.config.MaxBatchBytes)
	if err != nil {
		return err
	}
	if len(operations) == 0 {
		return nil
	}
	operationIDs := make([]string, len(operations))
	for index := range operations {
		operationIDs[index] = operations[index].OperationID
		if err := validateOperationScope(coordinator.config, operations[index]); err != nil {
			if recordErr := coordinator.repository.RecordSyncFailure(ctx, operationIDs[:index+1], err.Error(), now); recordErr != nil {
				return errors.Join(err, recordErr)
			}
			return err
		}
	}
	request := model.PushOperationsRequest{
		BatchID: model.NewUUIDv7(), EdgeID: coordinator.config.EdgeID,
		OutletID: coordinator.config.OutletID, Operations: operations,
	}
	response, err := coordinator.adapter.Push(ctx, request)
	if err != nil {
		if recordErr := coordinator.repository.RecordSyncFailure(ctx, operationIDs, err.Error(), now); recordErr != nil {
			return errors.Join(err, recordErr)
		}
		return err
	}
	if response.BatchID != "" && response.BatchID != request.BatchID {
		err := fmt.Errorf("syncer: cloud returned batchId %q for %q", response.BatchID, request.BatchID)
		if recordErr := coordinator.repository.RecordSyncFailure(ctx, operationIDs, err.Error(), now); recordErr != nil {
			return errors.Join(err, recordErr)
		}
		return err
	}
	if err := validateResults(operationIDs, response.Results); err != nil {
		if recordErr := coordinator.repository.RecordSyncFailure(ctx, operationIDs, err.Error(), now); recordErr != nil {
			return errors.Join(err, recordErr)
		}
		return err
	}
	return coordinator.repository.RecordSyncResults(ctx, operationIDs, response.Results, now)
}

func validateOperationScope(config Config, operation model.Operation) error {
	if config.EdgeID == "" || config.TenantID == "" || config.OutletID == "" {
		return errors.New("syncer: edge, tenant, and outlet identity must be configured")
	}
	if !model.IsUUIDv7(operation.OperationID) || operation.Mutation.ID != operation.OperationID {
		return fmt.Errorf("syncer: operation %q does not have a matching UUIDv7 mutation id", operation.OperationID)
	}
	if operation.Mutation.TenantID != config.TenantID {
		return fmt.Errorf("syncer: operation %q is outside tenant scope", operation.OperationID)
	}
	if operation.Mutation.OutletID != config.OutletID {
		return fmt.Errorf("syncer: operation %q is outside outlet scope", operation.OperationID)
	}
	if !model.IsUUIDv7(operation.AggregateID) {
		return fmt.Errorf("syncer: operation %q aggregateId must be a UUIDv7", operation.OperationID)
	}
	return nil
}

func fitBatch(edgeID, outletID string, operations []model.Operation, maximum int) ([]model.Operation, error) {
	if len(operations) == 0 {
		return operations, nil
	}
	for count := len(operations); count > 0; count-- {
		probe := model.PushOperationsRequest{
			BatchID: "00000000-0000-7000-8000-000000000000", EdgeID: edgeID,
			OutletID: outletID, Operations: operations[:count],
		}
		encoded, err := json.Marshal(probe)
		if err != nil {
			return nil, fmt.Errorf("syncer: encode batch: %w", err)
		}
		if len(encoded) <= maximum {
			return operations[:count], nil
		}
	}
	return nil, errors.New("syncer: first pending operation exceeds the 5 MiB protocol limit")
}

func validateResults(operationIDs []string, results []model.PushResult) error {
	requested := make(map[string]struct{}, len(operationIDs))
	for _, id := range operationIDs {
		requested[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		if _, exists := requested[result.OperationID]; !exists {
			return fmt.Errorf("syncer: cloud returned unknown operationId %q", result.OperationID)
		}
		if _, duplicate := seen[result.OperationID]; duplicate {
			return fmt.Errorf("syncer: cloud returned operationId %q more than once", result.OperationID)
		}
		if !result.Status.Valid() {
			return fmt.Errorf("syncer: cloud returned invalid status %q", result.Status)
		}
		seen[result.OperationID] = struct{}{}
	}
	return nil
}

type HTTPAdapterConfig struct {
	Endpoint         string
	BearerToken      string
	TenantID         string
	ActorID          string
	Client           *http.Client
	MaxResponseBytes int64
}

type HTTPAdapter struct {
	endpoint    string
	bearerToken string
	tenantID    string
	actorID     string
	client      *http.Client
	maxResponse int64
}

func NewHTTPAdapter(config HTTPAdapterConfig) (*HTTPAdapter, error) {
	if strings.TrimSpace(config.Endpoint) == "" {
		return nil, errors.New("syncer: cloud sync endpoint is required")
	}
	if config.Client == nil {
		config.Client = &http.Client{Timeout: 15 * time.Second}
	}
	if config.MaxResponseBytes <= 0 {
		config.MaxResponseBytes = 1 << 20
	}
	if strings.TrimSpace(config.TenantID) == "" || strings.TrimSpace(config.ActorID) == "" {
		return nil, errors.New("syncer: tenant and actor identity are required")
	}
	return &HTTPAdapter{
		endpoint: config.Endpoint, bearerToken: config.BearerToken,
		tenantID: config.TenantID, actorID: config.ActorID,
		client: config.Client, maxResponse: config.MaxResponseBytes,
	}, nil
}

func (adapter *HTTPAdapter) Push(ctx context.Context, batch model.PushOperationsRequest) (model.PushOperationsResponse, error) {
	body, err := json.Marshal(batch)
	if err != nil {
		return model.PushOperationsResponse{}, fmt.Errorf("syncer: encode request: %w", err)
	}
	if len(body) > maximumProtocolBytes {
		return model.PushOperationsResponse{}, errors.New("syncer: batch exceeds protocol limit")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, adapter.endpoint, bytes.NewReader(body))
	if err != nil {
		return model.PushOperationsResponse{}, fmt.Errorf("syncer: create request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Idempotency-Key", batch.BatchID)
	request.Header.Set("X-Edge-ID", batch.EdgeID)
	request.Header.Set("X-FeastCloud-Tenant-ID", adapter.tenantID)
	request.Header.Set("X-FeastCloud-Actor-ID", adapter.actorID)
	if adapter.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+adapter.bearerToken)
	}
	response, err := adapter.client.Do(request)
	if err != nil {
		return model.PushOperationsResponse{}, fmt.Errorf("syncer: push operations: %w", err)
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, adapter.maxResponse+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return model.PushOperationsResponse{}, fmt.Errorf("syncer: read response: %w", err)
	}
	if int64(len(raw)) > adapter.maxResponse {
		return model.PushOperationsResponse{}, errors.New("syncer: cloud response exceeds configured limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return model.PushOperationsResponse{}, fmt.Errorf("syncer: cloud returned HTTP %d", response.StatusCode)
	}
	var result model.PushOperationsResponse
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return model.PushOperationsResponse{}, fmt.Errorf("syncer: decode response: %w", err)
	}
	return result, nil
}
