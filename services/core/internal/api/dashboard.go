// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"time"

	"github.com/feastcloud/feastcloud/services/core/internal/store"
)

func (s *Server) dashboard() (store.DailyDashboardRepository, bool) {
	repository, ok := s.repository.(store.DailyDashboardRepository)
	return repository, ok
}

func (s *Server) handleDailyDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID, outletID, ok := readOutlet(w, r)
	if !ok {
		return
	}
	businessDate := r.URL.Query().Get("date")
	parsed, err := time.Parse("2006-01-02", businessDate)
	if err != nil || parsed.Format("2006-01-02") != businessDate {
		writeError(w, requestIDFrom(r.Context()), apiError{
			Status:  http.StatusUnprocessableEntity,
			Code:    "invalid_business_date",
			Message: "date must use the exact YYYY-MM-DD format",
			Details: map[string]string{"date": "invalid_date"},
		})
		return
	}
	repository, available := s.dashboard()
	if !available {
		writeError(w, requestIDFrom(r.Context()), apiError{
			Status:  http.StatusNotImplemented,
			Code:    "dashboard_unavailable",
			Message: "the daily dashboard requires PostgreSQL",
		})
		return
	}
	value, err := repository.DailyDashboard(r.Context(), store.DailyDashboardRequest{
		TenantID:     tenantID,
		OutletID:     outletID,
		BusinessDate: businessDate,
		AsOf:         s.now().UTC(),
	})
	if err != nil {
		writeReadRepositoryError(w, r, err)
		return
	}
	writeData(w, requestIDFrom(r.Context()), http.StatusOK, value)
}
