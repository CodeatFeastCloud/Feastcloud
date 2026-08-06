// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
)

const maxMenuImportDraftBytes = 900 * 1024

type menuImportDraftInput struct {
	ID              string          `json:"id"`
	OutletID        string          `json:"outletId"`
	Name            string          `json:"name"`
	ItemFileName    string          `json:"itemFileName"`
	AddonFileName   string          `json:"addonFileName"`
	SourceSHA256    string          `json:"sourceSha256"`
	ItemCount       int             `json:"itemCount"`
	CategoryCount   int             `json:"categoryCount"`
	AddonGroupCount int             `json:"addonGroupCount"`
	VariationCount  int             `json:"variationCount"`
	Draft           json.RawMessage `json:"draft"`
}

func (v menuImportDraftInput) validate() error {
	if !domain.ValidUUID(v.ID) || !domain.ValidUUID(v.OutletID) {
		return fmt.Errorf("import and outlet ids must be UUID strings")
	}
	if !validShortText(v.Name, 160) || !validShortText(v.ItemFileName, 255) || len(v.AddonFileName) > 255 {
		return fmt.Errorf("menu import name and file names are invalid")
	}
	if !sha256Pattern.MatchString(v.SourceSHA256) {
		return fmt.Errorf("sourceSha256 must be a lowercase SHA-256 digest")
	}
	if v.ItemCount < 1 || v.ItemCount > 500 || v.CategoryCount < 0 || v.CategoryCount > 80 || v.AddonGroupCount < 0 || v.AddonGroupCount > 80 || v.VariationCount < 0 || v.VariationCount > 1000 {
		return fmt.Errorf("menu import counts exceed the supported safe limit")
	}
	if len(v.Draft) == 0 || len(v.Draft) > maxMenuImportDraftBytes || !json.Valid(v.Draft) {
		return fmt.Errorf("draft must be valid JSON below 900 KiB")
	}
	return nil
}

func (s *Server) handleCreateMenuImportDraft(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, meta domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		var input menuImportDraftInput
		// Use a closure so validation observes the value after decodeAndValidate
		// has populated input. A method value here would capture the zero value.
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil {
			return *result
		}
		if input.OutletID != meta.OutletID {
			return outletScopeMismatch()
		}
		repository, ok := s.intelligence()
		if !ok {
			return errorResult(apiError{Status: http.StatusNotImplemented, Code: "menu_import_unavailable", Message: "durable menu imports require PostgreSQL"})
		}
		now := s.now().UTC()
		value := domain.MenuImportDraft{
			ID: input.ID, TenantID: meta.TenantID, OutletID: meta.OutletID,
			Name: strings.TrimSpace(input.Name), ItemFileName: strings.TrimSpace(input.ItemFileName),
			AddonFileName: strings.TrimSpace(input.AddonFileName), SourceSHA256: input.SourceSHA256,
			Status: "applied", ItemCount: input.ItemCount, CategoryCount: input.CategoryCount,
			AddonGroupCount: input.AddonGroupCount, VariationCount: input.VariationCount,
			Draft: input.Draft, ImportedAt: now,
		}
		audit, err := newAuditEvent(meta, "menu_import.applied", "menu_import", value.ID, now)
		if err != nil {
			return internalOperationError()
		}
		created, err := repository.CreateMenuImportDraft(ctx, value, audit)
		if err != nil {
			return repositoryError(err)
		}
		return successResult(http.StatusCreated, created, "/api/v1/menu-imports/"+created.ID)
	})
}

func (s *Server) handleMenuImportDrafts(w http.ResponseWriter, r *http.Request) {
	tenant, outlet, ok := readOutlet(w, r)
	if !ok {
		return
	}
	repository, available := s.intelligence()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{Status: http.StatusNotImplemented, Code: "menu_import_unavailable", Message: "durable menu imports require PostgreSQL"})
		return
	}
	values, err := repository.MenuImportDrafts(r.Context(), tenant, outlet)
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writePaginated(w, r, values, func(value domain.MenuImportDraft) string { return value.ID })
}
