package store

import (
	"context"
	"fmt"

	"github.com/feastcloud/feastcloud/services/core/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) CreateGuest(ctx context.Context, v domain.GuestProfile, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO guest_profiles(id,tenant_id,full_name,phone,email,locale,dietary_labels,notes,marketing_consent,consent_updated_at,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`, v.ID, v.TenantID, v.FullName, v.Phone, v.Email, v.Locale, v.DietaryLabels, v.Notes, v.MarketingConsent, v.ConsentUpdatedAt, v.CreatedAt)
		if err != nil {
			return err
		}
		accountID := inventoryEventUUID(v.TenantID, v.ID, "loyalty-account")
		if _, err = tx.Exec(ctx, `INSERT INTO loyalty_accounts(id,tenant_id,guest_id,created_at,updated_at)VALUES($1,$2,$3,$4,$4)`, accountID, v.TenantID, v.ID, v.CreatedAt); err != nil {
			return err
		}
		if v.MarketingConsent {
			if _, err = tx.Exec(ctx, `INSERT INTO guest_consent_events(id,tenant_id,guest_id,purpose,granted,source,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,'marketing',true,'profile_create',$4,$5,$6)`, a.ID, v.TenantID, v.ID, a.RecordedAt, a.ActorID, a.OperationID); err != nil {
				return err
			}
		}
		return insertAudit(ctx, tx, a)
	})
}

func (r *PostgresRepository) Guests(ctx context.Context, tenant string) ([]domain.GuestProfile, error) {
	values := []domain.GuestProfile{}
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,full_name,phone,email,locale,dietary_labels,notes,marketing_consent,consent_updated_at,version,created_at,updated_at FROM guest_profiles WHERE tenant_id=$1 ORDER BY updated_at DESC,id DESC LIMIT 200`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.GuestProfile
			if err := rows.Scan(&v.ID, &v.TenantID, &v.FullName, &v.Phone, &v.Email, &v.Locale, &v.DietaryLabels, &v.Notes, &v.MarketingConsent, &v.ConsentUpdatedAt, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}

func (r *PostgresRepository) SetGuestConsent(ctx context.Context, tenant, id string, granted bool, expected uint64, source string, a domain.AuditEvent) (domain.GuestProfile, error) {
	var v domain.GuestProfile
	err := r.withTenant(ctx, tenant, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,full_name,phone,email,locale,dietary_labels,notes,marketing_consent,consent_updated_at,version,created_at,updated_at FROM guest_profiles WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&v.ID, &v.TenantID, &v.FullName, &v.Phone, &v.Email, &v.Locale, &v.DietaryLabels, &v.Notes, &v.MarketingConsent, &v.ConsentUpdatedAt, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return err
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE guest_profiles SET marketing_consent=$3,consent_updated_at=$4,version=version+1,updated_at=$4 WHERE tenant_id=$1 AND id=$2`, tenant, id, granted, a.RecordedAt); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO guest_consent_events(id,tenant_id,guest_id,purpose,granted,source,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,'marketing',$4,$5,$6,$7,$8)`, a.ID, tenant, id, granted, source, a.RecordedAt, a.ActorID, a.OperationID); err != nil {
			return err
		}
		v.MarketingConsent = granted
		v.ConsentUpdatedAt = &a.RecordedAt
		v.Version++
		v.UpdatedAt = a.RecordedAt
		return insertAudit(ctx, tx, a)
	})
	return v, err
}

func (r *PostgresRepository) CreateReservation(ctx context.Context, v domain.Reservation, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO reservations(id,tenant_id,outlet_id,guest_id,guest_name,phone,party_size,scheduled_for,duration_minutes,status,source,notes,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)`, v.ID, v.TenantID, v.OutletID, nullable(v.GuestID), v.GuestName, v.Phone, v.PartySize, v.ScheduledFor, v.DurationMinutes, v.Status, v.Source, v.Notes, v.CreatedAt)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func (r *PostgresRepository) Reservations(ctx context.Context, t, o string) ([]domain.Reservation, error) {
	values := []domain.Reservation{}
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,COALESCE(guest_id::text,''),guest_name,phone,party_size,scheduled_for,duration_minutes,status,source,notes,version,created_at,updated_at FROM reservations WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY CASE status WHEN 'waiting' THEN 0 WHEN 'booked' THEN 1 ELSE 2 END,scheduled_for,id LIMIT 200`, t, o)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.Reservation
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.GuestID, &v.GuestName, &v.Phone, &v.PartySize, &v.ScheduledFor, &v.DurationMinutes, &v.Status, &v.Source, &v.Notes, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (r *PostgresRepository) TransitionReservation(ctx context.Context, t, o, id, status string, expected uint64, a domain.AuditEvent) (domain.Reservation, error) {
	var v domain.Reservation
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,outlet_id,COALESCE(guest_id::text,''),guest_name,phone,party_size,scheduled_for,duration_minutes,status,source,notes,version,created_at,updated_at FROM reservations WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR UPDATE`, t, o, id).Scan(&v.ID, &v.TenantID, &v.OutletID, &v.GuestID, &v.GuestName, &v.Phone, &v.PartySize, &v.ScheduledFor, &v.DurationMinutes, &v.Status, &v.Source, &v.Notes, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return err
		}
		if v.Version != expected {
			return ErrVersionConflict
		}
		allowed := map[string]map[string]bool{"booked": {"waiting": true, "seated": true, "cancelled": true, "no_show": true}, "waiting": {"seated": true, "cancelled": true, "no_show": true}, "seated": {"completed": true}}
		if !allowed[v.Status][status] {
			return ErrInvalidTransition
		}
		if _, err := tx.Exec(ctx, `UPDATE reservations SET status=$4,version=version+1,updated_at=$5 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, t, o, id, status, a.RecordedAt); err != nil {
			return err
		}
		v.Status = status
		v.Version++
		v.UpdatedAt = a.RecordedAt
		return insertAudit(ctx, tx, a)
	})
	return v, err
}

func (r *PostgresRepository) CreatePromotion(ctx context.Context, v domain.Promotion, a domain.AuditEvent) error {
	return r.withTenant(ctx, v.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO promotions(id,tenant_id,outlet_id,code,name,discount_type,discount_value,min_order_minor,starts_at,ends_at,redemption_limit,active,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,true,$12,$12)`, v.ID, v.TenantID, v.OutletID, v.Code, v.Name, v.DiscountType, v.DiscountValue, v.MinOrderMinor, v.StartsAt, v.EndsAt, v.RedemptionLimit, v.CreatedAt)
		if err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
}
func (r *PostgresRepository) Promotions(ctx context.Context, t, o string) ([]domain.Promotion, error) {
	values := []domain.Promotion{}
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id,tenant_id,outlet_id,code,name,discount_type,discount_value,min_order_minor,starts_at,ends_at,redemption_limit,redemption_count,active,version,created_at,updated_at FROM promotions WHERE tenant_id=$1 AND outlet_id=$2 ORDER BY active DESC,ends_at DESC LIMIT 200`, t, o)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.Promotion
			if err := rows.Scan(&v.ID, &v.TenantID, &v.OutletID, &v.Code, &v.Name, &v.DiscountType, &v.DiscountValue, &v.MinOrderMinor, &v.StartsAt, &v.EndsAt, &v.RedemptionLimit, &v.RedemptionCount, &v.Active, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (r *PostgresRepository) RedeemPromotion(ctx context.Context, t, o, code string, v domain.PromotionRedemption, a domain.AuditEvent) (domain.PromotionRedemption, error) {
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		var p domain.Promotion
		if err := tx.QueryRow(ctx, `SELECT id,discount_type,discount_value,min_order_minor,starts_at,ends_at,redemption_limit,redemption_count,active FROM promotions WHERE tenant_id=$1 AND outlet_id=$2 AND upper(code)=upper($3) FOR UPDATE`, t, o, code).Scan(&p.ID, &p.DiscountType, &p.DiscountValue, &p.MinOrderMinor, &p.StartsAt, &p.EndsAt, &p.RedemptionLimit, &p.RedemptionCount, &p.Active); err != nil {
			return err
		}
		if !p.Active || a.RecordedAt.Before(p.StartsAt) || !a.RecordedAt.Before(p.EndsAt) || v.BasketMinor < p.MinOrderMinor || (p.RedemptionLimit != nil && p.RedemptionCount >= *p.RedemptionLimit) {
			return ErrInvalidReference
		}
		v.PromotionID = p.ID
		if p.DiscountType == "percentage" {
			v.DiscountMinor = v.BasketMinor * p.DiscountValue / 10000
		} else {
			v.DiscountMinor = p.DiscountValue
		}
		if v.DiscountMinor > v.BasketMinor {
			v.DiscountMinor = v.BasketMinor
		}
		v.OccurredAt = a.RecordedAt
		if _, err := tx.Exec(ctx, `INSERT INTO promotion_redemptions(id,tenant_id,outlet_id,promotion_id,guest_id,order_id,basket_minor,discount_minor,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, v.ID, t, o, v.PromotionID, nullable(v.GuestID), nullable(v.OrderID), v.BasketMinor, v.DiscountMinor, v.OccurredAt, a.ActorID, a.OperationID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE promotions SET redemption_count=redemption_count+1,version=version+1,updated_at=$4 WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3`, t, o, p.ID, a.RecordedAt); err != nil {
			return err
		}
		return insertAudit(ctx, tx, a)
	})
	return v, err
}

func (r *PostgresRepository) LoyaltyAccounts(ctx context.Context, t string) ([]domain.LoyaltyAccount, error) {
	values := []domain.LoyaltyAccount{}
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT a.id,a.tenant_id,a.guest_id,g.full_name,a.points_balance,a.lifetime_earned,a.version,a.created_at,a.updated_at FROM loyalty_accounts a JOIN guest_profiles g ON g.tenant_id=a.tenant_id AND g.id=a.guest_id WHERE a.tenant_id=$1 ORDER BY a.updated_at DESC LIMIT 200`, t)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var v domain.LoyaltyAccount
			if err := rows.Scan(&v.ID, &v.TenantID, &v.GuestID, &v.GuestName, &v.PointsBalance, &v.LifetimeEarned, &v.Version, &v.CreatedAt, &v.UpdatedAt); err != nil {
				return err
			}
			values = append(values, v)
		}
		return rows.Err()
	})
	return values, err
}
func (r *PostgresRepository) AdjustLoyalty(ctx context.Context, t, accountID, eventType string, expected uint64, v domain.LoyaltyEvent, a domain.AuditEvent) (domain.LoyaltyAccount, error) {
	var account domain.LoyaltyAccount
	err := r.withTenant(ctx, t, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT id,tenant_id,guest_id,points_balance,lifetime_earned,version,created_at,updated_at FROM loyalty_accounts WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, t, accountID).Scan(&account.ID, &account.TenantID, &account.GuestID, &account.PointsBalance, &account.LifetimeEarned, &account.Version, &account.CreatedAt, &account.UpdatedAt); err != nil {
			return err
		}
		if account.Version != expected {
			return ErrVersionConflict
		}
		if account.PointsBalance+v.PointsDelta < 0 {
			return fmt.Errorf("%w: insufficient loyalty points", ErrInvalidReference)
		}
		earned := int64(0)
		if v.PointsDelta > 0 {
			earned = v.PointsDelta
		}
		if _, err := tx.Exec(ctx, `UPDATE loyalty_accounts SET points_balance=points_balance+$3,lifetime_earned=lifetime_earned+$4,version=version+1,updated_at=$5 WHERE tenant_id=$1 AND id=$2`, t, accountID, v.PointsDelta, earned, a.RecordedAt); err != nil {
			return err
		}
		v.AccountID = accountID
		v.EventType = eventType
		v.OccurredAt = a.RecordedAt
		if _, err := tx.Exec(ctx, `INSERT INTO loyalty_events(id,tenant_id,account_id,event_type,points_delta,reason,order_id,occurred_at,actor_id,operation_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, v.ID, t, accountID, eventType, v.PointsDelta, v.Reason, nullable(v.OrderID), v.OccurredAt, a.ActorID, a.OperationID); err != nil {
			return err
		}
		account.PointsBalance += v.PointsDelta
		account.LifetimeEarned += earned
		account.Version++
		account.UpdatedAt = a.RecordedAt
		return insertAudit(ctx, tx, a)
	})
	return account, err
}
