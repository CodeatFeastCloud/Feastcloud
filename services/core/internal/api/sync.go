// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

const maxSyncBodyBytes = int64(5 << 20)

type syncOperationsRequest struct {
	BatchID    string          `json:"batchId"`
	EdgeID     string          `json:"edgeId"`
	OutletID   string          `json:"outletId"`
	Operations []syncOperation `json:"operations"`
}

type syncOperation struct {
	OperationID      string           `json:"operationId"`
	AggregateType    string           `json:"aggregateType"`
	AggregateID      string           `json:"aggregateId"`
	AggregateVersion uint64           `json:"aggregateVersion"`
	CommandType      string           `json:"commandType"`
	Mutation         mutationEnvelope `json:"mutation"`
	RecordedAt       time.Time        `json:"recordedAt,omitempty"`
}

type syncOperationsResponse struct {
	BatchID string                `json:"batchId"`
	Results []syncOperationResult `json:"results"`
}

type syncOperationResult struct {
	OperationID       string `json:"operationId"`
	Status            string `json:"status"`
	ProblemCode       string `json:"problemCode,omitempty"`
	RetryAfterSeconds int    `json:"retryAfterSeconds,omitempty"`
}

func (s *Server) handleSyncOperations(w http.ResponseWriter, r *http.Request) {
	requestID := requestIDFrom(r.Context())
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, requestID, apiError{
			Status:  http.StatusUnsupportedMediaType,
			Code:    "unsupported_media_type",
			Message: "Content-Type must be application/json",
		})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxSyncBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, requestID, apiError{
				Status:  http.StatusRequestEntityTooLarge,
				Code:    "sync_batch_too_large",
				Message: "sync batch must not exceed 5 MiB",
			})
			return
		}
		writeError(w, requestID, apiError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request",
			Message: "could not read sync batch",
		})
		return
	}
	var request syncOperationsRequest
	if err := decodeStrict(body, &request); err != nil {
		writeError(w, requestID, apiError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_sync_batch",
			Message: err.Error(),
		})
		return
	}
	if strings.TrimSpace(request.BatchID) == "" || len(request.BatchID) > 128 ||
		strings.TrimSpace(request.EdgeID) == "" || len(request.EdgeID) > 128 ||
		strings.TrimSpace(request.OutletID) == "" || len(request.OutletID) > 128 {
		writeError(w, requestID, apiError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "invalid_sync_batch",
			Message: "batchId, edgeId, and outletId are required and must be at most 128 characters",
		})
		return
	}
	if len(request.Operations) == 0 || len(request.Operations) > 500 {
		writeError(w, requestID, apiError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "invalid_sync_batch_size",
			Message: "operations must contain between 1 and 500 items",
		})
		return
	}
	requestPrincipal, ok := principalFrom(r.Context())
	if !ok {
		writeError(w, requestID, apiError{
			Status:  http.StatusUnauthorized,
			Code:    "authentication_required",
			Message: "an authenticated principal is required",
		})
		return
	}
	if requestPrincipal.ActorID != "edge:"+request.EdgeID {
		writeError(w, requestID, apiError{
			Status:  http.StatusForbidden,
			Code:    "edge_principal_mismatch",
			Message: "the authenticated edge principal must match edgeId",
		})
		return
	}
	if requestPrincipal.Kind!="device"||!requestPrincipal.AllowsOutlet(request.OutletID){writeError(w,requestID,apiError{Status:http.StatusForbidden,Code:"device_scope_mismatch",Message:"the enrolled device is not authorized for this outlet"});return}
	for _, operation := range request.Operations {
		if operation.Mutation.TenantID != requestPrincipal.TenantID {
			writeError(w, requestID, apiError{
				Status:  http.StatusForbidden,
				Code:    "principal_scope_mismatch",
				Message: "every operation tenantId must match the authenticated scope",
			})
			return
		}
	}

	response := syncOperationsResponse{
		BatchID: request.BatchID,
		Results: make([]syncOperationResult, 0, len(request.Operations)),
	}
	for _, operation := range request.Operations {
		response.Results = append(response.Results, s.acceptSyncOperation(r, request, operation))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) acceptSyncOperation(
	r *http.Request,
	batch syncOperationsRequest,
	operation syncOperation,
) syncOperationResult {
	result := syncOperationResult{OperationID: operation.OperationID}
	if problemCode := validateSyncOperation(batch, operation); problemCode != "" {
		result.Status = "REJECTED"
		result.ProblemCode = problemCode
		return result
	}
	fingerprintBody, err := json.Marshal(operation)
	if err != nil {
		result.Status = "RETRY"
		result.ProblemCode = "operation_encoding_failed"
		result.RetryAfterSeconds = 5
		return result
	}
	fingerprintBytes := sha256.Sum256(fingerprintBody)
	mutation, err := json.Marshal(operation.Mutation)
	if err != nil {
		result.Status = "RETRY"
		result.ProblemCode = "operation_encoding_failed"
		result.RetryAfterSeconds = 5
		return result
	}
	outcome, problemCode, err := s.syncRepository.ApplySyncOperation(r.Context(), store.SyncOperation{
		TenantID: operation.Mutation.TenantID, OperationID: operation.OperationID,
		EdgeID: batch.EdgeID, OutletID: batch.OutletID, BatchID: batch.BatchID,
		AggregateType: operation.AggregateType, AggregateID: operation.AggregateID,
		AggregateVersion: operation.AggregateVersion, CommandType: operation.CommandType,
		RequestHash: fingerprintBytes[:], Mutation: mutation, RecordedAt: operation.RecordedAt,
		ReceivedAt: s.now().UTC(),
	})
	switch {
	case errors.Is(err, store.ErrCausalPredecessor):
		result.Status = "RETRY"
		result.ProblemCode = "causal_predecessor_missing"
		result.RetryAfterSeconds = 1
	case err != nil:
		result.Status = "RETRY"
		result.ProblemCode = "sync_inbox_unavailable"
		result.RetryAfterSeconds = 5
	case outcome != store.SyncAccepted && outcome != store.SyncDuplicate &&
		outcome != store.SyncRejected && outcome != store.SyncConflict:
		result.Status = "RETRY"
		result.ProblemCode = "invalid_sync_repository_result"
		result.RetryAfterSeconds = 5
	default:
		result.Status = string(outcome)
		result.ProblemCode = problemCode
	}
	return result
}

func validateSyncOperation(batch syncOperationsRequest, operation syncOperation) string {
	if !domain.ValidUUID(operation.OperationID) {
		return "invalid_operation_id"
	}
	if !domain.ValidUUID(operation.AggregateID) {
		return "invalid_aggregate_id"
	}
	if operation.AggregateVersion < 1 {
		return "invalid_aggregate_version"
	}
	if strings.TrimSpace(operation.AggregateType) == "" || len(operation.AggregateType) > 128 {
		return "invalid_aggregate_type"
	}
	if strings.TrimSpace(operation.CommandType) == "" || len(operation.CommandType) > 128 {
		return "invalid_command_type"
	}
	if err := operation.Mutation.MutationMetadata.Validate(); err != nil {
		return "invalid_mutation_metadata"
	}
	if operation.Mutation.ID != operation.OperationID {
		return "operation_id_mismatch"
	}
	if operation.Mutation.OutletID != batch.OutletID {
		return "outlet_scope_mismatch"
	}
	payload := bytes.TrimSpace(operation.Mutation.Payload)
	if len(payload) == 0 || bytes.Equal(payload, []byte("null")) || payload[0] != '{' {
		return "invalid_mutation_payload"
	}
	if !json.Valid(payload) {
		return "invalid_mutation_payload"
	}
	return ""
}
