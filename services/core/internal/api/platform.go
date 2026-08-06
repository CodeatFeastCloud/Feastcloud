// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/idempotency"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

type tenantProvisionResponse struct {
	Organization domain.Organization `json:"organization"`
	Outlet       domain.Outlet       `json:"outlet"`
	Brand        domain.Brand        `json:"brand"`
	Stations     []domain.Station    `json:"stations"`
	OwnerHandoff struct {
		Name   string `json:"name"`
		Email  string `json:"email"`
		Status string `json:"status"`
	} `json:"ownerHandoff"`
}

func provisionStations(template string, tenantID, outletID string, now time.Time) ([]domain.Station, error) {
	types := []struct{ name, code string; kind domain.StationType }{{"Hot kitchen", "HOT", domain.StationTypeCooking}, {"Expo", "EXPO", domain.StationTypeExpo}, {"Packing", "PACK", domain.StationTypePacking}}
	if template == "restaurant" { types = append([]struct{ name, code string; kind domain.StationType }{{"Beverages", "BAR", domain.StationTypeBeverage}}, types...) }
	if template == "central" { types = []struct{ name, code string; kind domain.StationType }{{"Preparation", "PREP", domain.StationTypePreparation}, {"Production", "COOK", domain.StationTypeCooking}, {"Dispatch", "PACK", domain.StationTypePacking}} }
	stations := make([]domain.Station, 0, len(types))
	for _, item := range types { id, err := newUUIDv7(now); if err != nil { return nil, err }; stations = append(stations, domain.Station{ID:id,TenantID:tenantID,OutletID:outletID,Name:item.name,Code:item.code,Type:item.kind,Active:true,RecordMetadata:newRecordMetadata(now)}) }
	return stations, nil
}

func (s *Server) handleProvisionTenant(w http.ResponseWriter, r *http.Request) {
	s.executeMutation(w, r, func(ctx context.Context, metadata domain.MutationMetadata, payload json.RawMessage) idempotency.Result {
		principal, ok := principalFrom(ctx)
		if !ok || !principal.IsPlatformAdmin() { return errorResult(apiError{Status:http.StatusForbidden, Code:"platform_admin_required", Message:"a FeastCloud platform administrator is required"}) }
		var input tenantProvisionInput
		if result := decodeAndValidate(payload, &input, func() error { return input.validate() }); result != nil { return *result }
		now := s.now().UTC()
		brandID, err := newUUIDv7(now); if err != nil { return internalOperationError() }
		stations, err := provisionStations(input.Template, metadata.TenantID, metadata.OutletID, now); if err != nil { return internalOperationError() }
		organization := domain.Organization{ID:metadata.TenantID,TenantID:metadata.TenantID,Name:strings.TrimSpace(input.OrganizationName),LegalName:strings.TrimSpace(input.LegalName),DefaultLocale:input.DefaultLocale,DefaultCurrency:input.DefaultCurrency,Active:true,RecordMetadata:newRecordMetadata(now)}
		outlet := domain.Outlet{ID:metadata.OutletID,TenantID:metadata.TenantID,OrganizationID:metadata.TenantID,Name:strings.TrimSpace(input.OutletName),Code:strings.TrimSpace(input.OutletCode),TimeZone:input.TimeZone,Currency:input.DefaultCurrency,Active:true,RecordMetadata:newRecordMetadata(now)}
		brand := domain.Brand{ID:brandID,TenantID:metadata.TenantID,OrganizationID:metadata.TenantID,Name:strings.TrimSpace(input.BrandName),Code:strings.TrimSpace(input.BrandCode),Active:true,RecordMetadata:newRecordMetadata(now)}
		assignment := domain.BrandOutletAssignment{TenantID:metadata.TenantID,BrandID:brandID,OutletID:metadata.OutletID,Active:true,RecordMetadata:newRecordMetadata(now)}
		audit, err := newAuditEvent(metadata, "tenant.provisioned", "organization", organization.ID, now); if err != nil { return internalOperationError() }
		provisioner, available := s.repository.(store.PlatformProvisioner); if !available { return errorResult(apiError{Status:http.StatusNotImplemented,Code:"platform_provisioning_unavailable",Message:"tenant provisioning requires a platform-capable repository"}) }
		if err := provisioner.ProvisionTenant(ctx, organization, outlet, brand, assignment, stations, audit); err != nil { return repositoryError(err) }
		response := tenantProvisionResponse{Organization:organization,Outlet:outlet,Brand:brand,Stations:stations}
		response.OwnerHandoff.Name, response.OwnerHandoff.Email, response.OwnerHandoff.Status = strings.TrimSpace(input.OwnerName), strings.TrimSpace(input.OwnerEmail), "identity_invite_pending"
		return successResult(http.StatusCreated, response, "/api/v1/organizations/"+organization.ID)
	})
}
