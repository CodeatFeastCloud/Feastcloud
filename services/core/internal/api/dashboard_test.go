// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

type dashboardRepositoryStub struct {
	store.Repository
	request  store.DailyDashboardRequest
	response domain.DailyDashboard
	err      error
}

func (repository *dashboardRepositoryStub) DailyDashboard(_ context.Context, request store.DailyDashboardRequest) (domain.DailyDashboard, error) {
	repository.request = request
	return repository.response, repository.err
}

func TestDailyDashboardHandlerUsesNormalEnvelopeAndReadCutoff(t *testing.T) {
	asOf := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	repository := &dashboardRepositoryStub{
		Repository: store.NewMemoryRepository(),
		response: domain.DailyDashboard{
			OutletID: outletOne, BusinessDate: "2026-08-03", Currency: "INR", TimeZone: "Asia/Kolkata", AsOf: asOf,
			Period:    domain.DashboardPeriod{StartsAt: time.Date(2026, 8, 2, 18, 30, 0, 0, time.UTC), EndsAt: time.Date(2026, 8, 3, 18, 30, 0, 0, time.UTC), BoundaryKind: "outlet_local_calendar_day"},
			TenderMix: []domain.DashboardTenderMix{}, FulfillmentMix: []domain.DashboardOrderTypeMix{}, Hourly: []domain.DashboardHourly{}, TopItems: []domain.DashboardTopItem{}, UnavailableFields: []string{"online.prepaidMinor"},
		},
	}
	server := NewServer(repository, nil, Config{})
	server.now = func() time.Time { return asOf }
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/daily?outletId="+outletOne+"&date=2026-08-03", nil)
	request.Header.Set("X-FeastCloud-Tenant-ID", tenantOne)
	request.Header.Set("X-FeastCloud-Actor-ID", actorOne)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if repository.request.TenantID != tenantOne || repository.request.OutletID != outletOne || repository.request.BusinessDate != "2026-08-03" || !repository.request.AsOf.Equal(asOf) {
		t.Fatalf("repository request=%#v", repository.request)
	}
	var envelope struct {
		Data domain.DailyDashboard `json:"data"`
		Meta responseMeta          `json:"meta"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Meta.RequestID == "" || envelope.Data.Period.BoundaryKind != "outlet_local_calendar_day" || envelope.Data.TenderMix == nil || envelope.Data.FulfillmentMix == nil || envelope.Data.Hourly == nil || envelope.Data.TopItems == nil {
		t.Fatalf("dashboard envelope=%#v", envelope)
	}
}

func TestDailyDashboardHandlerRejectsMalformedDateBeforeRepositoryRead(t *testing.T) {
	repository := &dashboardRepositoryStub{Repository: store.NewMemoryRepository()}
	server := NewServer(repository, nil, Config{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/daily?outletId="+outletOne+"&date=2026-8-3", nil)
	request.Header.Set("X-FeastCloud-Tenant-ID", tenantOne)
	request.Header.Set("X-FeastCloud-Actor-ID", actorOne)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || repository.request.TenantID != "" {
		t.Fatalf("status=%d repository request=%#v body=%s", response.Code, repository.request, response.Body.String())
	}
}
