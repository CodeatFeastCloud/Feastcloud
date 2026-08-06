// SPDX-License-Identifier: AGPL-3.0-only

package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestFoundationMigrationContainsSafetyConstraints(t *testing.T) {
	t.Parallel()

	up := readMigration(t, "000001_foundation.up.sql")
	required := []string{
		"CONSTRAINT stations_tenant_outlet_id_key UNIQUE (tenant_id, outlet_id, id)",
		"CONSTRAINT orders_tenant_outlet_id_key UNIQUE (tenant_id, outlet_id, id)",
		"FOREIGN KEY (tenant_id, outlet_id, order_id)\n        REFERENCES orders (tenant_id, outlet_id, id)",
		"FOREIGN KEY (tenant_id, outlet_id, station_id)\n        REFERENCES stations (tenant_id, outlet_id, id)",
		"response_body bytea",
		"CREATE TRIGGER sync_inbox_evidence_immutable",
		"BEFORE UPDATE OR DELETE ON sync_inbox",
		"EXECUTE FUNCTION feastcloud.protect_sync_inbox_evidence()",
	}
	for _, fragment := range required {
		if !strings.Contains(up, fragment) {
			t.Errorf("up migration is missing safety fragment %q", fragment)
		}
	}
	if strings.Contains(up, "response_body jsonb") {
		t.Error("idempotency response_body must preserve exact bytes, not use jsonb")
	}
}

func TestSyncInboxTriggerAllowsOnlyResultFieldsToChange(t *testing.T) {
	t.Parallel()

	up := readMigration(t, "000001_foundation.up.sql")
	start := strings.Index(up, "CREATE FUNCTION feastcloud.protect_sync_inbox_evidence()")
	if start < 0 {
		t.Fatal("sync inbox evidence protection function is missing")
	}
	remainder := up[start:]
	end := strings.Index(remainder, "\n$$;")
	if end < 0 {
		t.Fatal("sync inbox evidence protection function is unterminated")
	}
	function := remainder[:end]

	immutableFields := []string{
		"tenant_id",
		"operation_id",
		"edge_id",
		"outlet_id",
		"batch_id",
		"aggregate_type",
		"aggregate_id",
		"aggregate_version",
		"command_type",
		"request_hash",
		"mutation",
		"received_at",
	}
	for _, field := range immutableFields {
		if !strings.Contains(function, "NEW."+field+" IS DISTINCT FROM OLD."+field) {
			t.Errorf("sync inbox evidence field %q is not protected", field)
		}
	}
	for _, allowed := range []string{"status", "problem_code", "processed_at"} {
		if strings.Contains(function, "NEW."+allowed+" IS DISTINCT FROM OLD."+allowed) {
			t.Errorf("result field %q should remain updateable", allowed)
		}
	}
}

func TestDownMigrationRemovesEvidenceProtectionFunction(t *testing.T) {
	t.Parallel()

	down := readMigration(t, "000001_foundation.down.sql")
	if !strings.Contains(down, "DROP FUNCTION IF EXISTS feastcloud.protect_sync_inbox_evidence();") {
		t.Fatal("down migration does not remove sync inbox evidence protection function")
	}
}

func TestDomainEventMigrationIsAppendOnlyTenantScopedAndInboxLinked(t *testing.T) {
	t.Parallel()

	up := readMigration(t, "000002_domain_events.up.sql")
	for _, fragment := range []string{
		"CREATE TABLE domain_events",
		"REFERENCES sync_inbox (tenant_id, operation_id)",
		"CREATE TRIGGER domain_events_immutable",
		"EXECUTE FUNCTION feastcloud.reject_immutable_change()",
		"ALTER TABLE domain_events FORCE ROW LEVEL SECURITY",
		"CREATE POLICY domain_events_isolation ON domain_events",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("domain event migration is missing %q", fragment)
		}
	}
	down := readMigration(t, "000002_domain_events.down.sql")
	if !strings.Contains(down, "DROP TABLE IF EXISTS domain_events;") {
		t.Fatal("domain event down migration does not remove domain_events")
	}
}

func TestDevelopmentPostgresProfileUsesRestrictedLoopbackRuntime(t *testing.T) {
	t.Parallel()

	bootstrap := readFile(t, "../../../deploy/postgres/003_development_bootstrap.sql")
	for _, fragment := range []string{
		"NOSUPERUSER",
		"NOBYPASSRLS",
		"REVOKE DELETE ON sync_inbox, domain_events",
		"REVOKE UPDATE ON domain_events",
		"GRANT SELECT, INSERT, UPDATE ON sync_inbox",
		"GRANT SELECT, INSERT ON domain_events",
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	} {
		if !strings.Contains(bootstrap, fragment) {
			t.Errorf("development bootstrap is missing %q", fragment)
		}
	}
	if strings.Contains(bootstrap, "GRANT ALL") {
		t.Fatal("development runtime role must not receive GRANT ALL")
	}

	native := readFile(t, "../../../scripts/postgres-native.sh")
	for _, fragment := range []string{
		`.feastcloud/postgres`,
		`brew --prefix postgresql@17`,
		`-h 127.0.0.1`,
		`pg_ctl`,
		`initdb`,
	} {
		if !strings.Contains(native, fragment) {
			t.Errorf("native PostgreSQL profile is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{"brew services", "docker compose"} {
		if strings.Contains(native, forbidden) {
			t.Errorf("native PostgreSQL profile unexpectedly depends on %q", forbidden)
		}
	}
	installer := readFile(t, "../../../scripts/postgres-install-native.sh")
	for _, fragment := range []string{
		"brew install postgresql@17",
		"--sharedir",
		"--pkglibdir",
		"if [ -e \"$expected_path\" ]",
	} {
		if !strings.Contains(installer, fragment) {
			t.Errorf("native PostgreSQL installer is missing %q", fragment)
		}
	}

	packageJSON := readFile(t, "../../../package.json")
	for _, fragment := range []string{
		`"db:up": "sh scripts/postgres-native.sh start"`,
		`"db:test": "sh scripts/postgres-native.sh test"`,
		`"db:down": "sh scripts/postgres-native.sh stop"`,
	} {
		if !strings.Contains(packageJSON, fragment) {
			t.Errorf("package scripts do not make native PostgreSQL primary: missing %q", fragment)
		}
	}

	compose := readFile(t, "../../../compose.dev.yaml")
	for _, fragment := range []string{
		`"127.0.0.1:${FEASTCLOUD_POSTGRES_PORT:-54329}:5432"`,
		"000001_foundation.up.sql",
		"000002_domain_events.up.sql",
		"000003_nullable_organization_audit.up.sql",
		"000004_identity_devices.up.sql",
		"000005_kitchen_graph_inventory.up.sql",
		"000006_inventory_counts.up.sql",
		"000007_production_batches.up.sql",
		"000008_imports_planning.up.sql",
		"000009_operational_resilience_reconciliation.up.sql",
		"000010_daily_operations_mvp.up.sql",
		"000011_native_commerce.up.sql",
		"000012_guest_growth_refunds.up.sql",
		"000013_daily_dashboard_indexes.up.sql",
		"000014_connected_commerce.up.sql",
		"000015_restaurant_core.up.sql",
		"000016_aggregator_inbox_decisions.up.sql",
		"000017_qr_web_order_requests.up.sql",
		"000018_stock_transfer_execution.up.sql",
		"000019_replenishment_rules.up.sql",
		"000020_brand_outlet_assignments.up.sql",
		"000021_menu_import_drafts.up.sql",
		"000022_optional_menu_item_recipe.up.sql",
		"000023_connector_external_outlets.up.sql",
		"003_development_bootstrap.sql",
	} {
		if !strings.Contains(compose, fragment) {
			t.Errorf("development compose profile is missing %q", fragment)
		}
	}
}

func TestKitchenGraphInventoryMigrationIsHistoricalAndTenantSafe(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000005_kitchen_graph_inventory.up.sql")
	for _, fragment := range []string{
		"order_line_recipe_snapshots",
		"CREATE TRIGGER inventory_events_immutable",
		"CREATE TRIGGER order_line_recipe_snapshots_immutable",
		"recipe_versions_one_current_idx",
		"ALTER TABLE %I FORCE ROW LEVEL SECURITY",
		"reverses_event_id",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("Kitchen Graph migration is missing %q", fragment)
		}
	}
}

func TestInventoryCountsAreImmutableTenantScopedEvidence(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000006_inventory_counts.up.sql")
	for _, fragment := range []string{"CREATE TABLE inventory_counts", "CREATE TABLE inventory_count_lines", "inventory_counts_immutable", "inventory_count_lines_immutable", "FORCE ROW LEVEL SECURITY", "expected_quantity_base", "variance_quantity_base"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("inventory count migration is missing %q", fragment)
		}
	}
}

func TestConnectedCommerceMigrationUsesSharedLedgersAndTenantIsolation(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000014_connected_commerce.up.sql")
	for _, fragment := range []string{
		"CREATE TABLE sales_channels",
		"CREATE TABLE connector_order_inbox",
		"CREATE TABLE kitchen_print_jobs",
		"CREATE TABLE pickup_tokens",
		"CREATE TABLE qr_ordering_links",
		"CREATE TABLE stock_transfers",
		"CREATE TABLE hardware_devices",
		"connector_order_inbox_immutable",
		"FOREIGN KEY (tenant_id,outlet_id,order_id) REFERENCES orders(tenant_id,outlet_id,id)",
		"ALTER TABLE %I FORCE ROW LEVEL SECURITY",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("connected commerce migration is missing %q", fragment)
		}
	}
	bootstrap := readFile(t, "../../../deploy/postgres/003_development_bootstrap.sql")
	for _, fragment := range []string{"sales_channels", "connector_order_inbox", "station_capacity_limits", "hardware_devices"} {
		if !strings.Contains(bootstrap, fragment) {
			t.Errorf("development runtime grant is missing %q", fragment)
		}
	}
}

func TestRestaurantCoreMigrationIsVersionedAndAuditable(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000015_restaurant_core.up.sql")
	for _, fragment := range []string{
		"CREATE TABLE menu_studios",
		"CREATE TABLE menu_studio_versions",
		"CREATE TABLE menu_modifier_groups",
		"CREATE TABLE menu_publications",
		"CREATE TABLE pos_checkout_records",
		"CREATE TABLE kitchen_print_job_events",
		"CREATE TABLE pickup_token_events",
		"menu_studio_versions_immutable",
		"FORCE ROW LEVEL SECURITY",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("restaurant core migration is missing %q", fragment)
		}
	}
	bootstrap := readFile(t, "../../../deploy/postgres/003_development_bootstrap.sql")
	for _, fragment := range []string{"menu_studios", "pos_checkout_records", "kitchen_print_job_events"} {
		if !strings.Contains(bootstrap, fragment) {
			t.Errorf("development runtime grant is missing %q", fragment)
		}
	}
}

func TestAggregatorInboxDecisionsAreImmutableTenantScopedEvidence(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000016_aggregator_inbox_decisions.up.sql")
	for _, fragment := range []string{
		"CREATE TABLE connector_order_decisions",
		"normalized_order_id",
		"connector_order_decisions_immutable",
		"FORCE ROW LEVEL SECURITY",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("aggregator decision migration is missing %q", fragment)
		}
	}
}

func TestQROrderRequestsAreImmutableAndIdempotent(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000017_qr_web_order_requests.up.sql")
	for _, fragment := range []string{"CREATE TABLE web_order_requests", "tracking_code", "client_request_id", "web_order_requests_immutable", "FORCE ROW LEVEL SECURITY"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("QR web order migration is missing %q", fragment)
		}
	}
}

func TestStockTransferExecutionEvidenceIsImmutableAndTenantScoped(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000018_stock_transfer_execution.up.sql")
	for _, fragment := range []string{"CREATE TABLE stock_transfer_events", "event_type", "stock_transfer_events_immutable", "FORCE ROW LEVEL SECURITY"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("stock transfer execution migration is missing %q", fragment)
		}
	}
}

func TestReplenishmentRulesAreTenantScopedAndVersioned(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000019_replenishment_rules.up.sql")
	for _, fragment := range []string{"CREATE TABLE replenishment_rules", "reorder_point_base", "target_level_base", "version bigint", "FORCE ROW LEVEL SECURITY"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("replenishment migration is missing %q", fragment)
		}
	}
}

func TestProductionBatchesAreVersionedAndTenantScoped(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000007_production_batches.up.sql")
	for _, fragment := range []string{"CREATE TABLE production_batches", "recipe_version_id", "output_ingredient_id", "actual_quantity", "expires_at", "version bigint", "FORCE ROW LEVEL SECURITY", "production_batches_queue_idx"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("production batch migration is missing %q", fragment)
		}
	}
}

func TestImportsAndPlanningEvidenceAreImmutableAndTenantScoped(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000008_imports_planning.up.sql")
	for _, fragment := range []string{"CREATE TABLE order_imports", "CREATE TABLE order_import_rows", "file_sha256", "CREATE TABLE planning_runs", "CREATE TABLE planning_recommendations", "stockout_warning", "model_version", "reject_immutable_change", "FORCE ROW LEVEL SECURITY"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("imports/planning migration is missing %q", fragment)
		}
	}
}

func TestMenuImportDraftsAreImmutableTenantScopedAndNeverPublished(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000021_menu_import_drafts.up.sql")
	for _, fragment := range []string{"CREATE TABLE menu_import_drafts", "source_sha256", "status IN ('staged','mapping','applied','rejected')", "menu_import_drafts_immutable", "FORCE ROW LEVEL SECURITY"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("menu import migration is missing %q", fragment)
		}
	}
	bootstrap := readFile(t, "../../../deploy/postgres/003_development_bootstrap.sql")
	if !strings.Contains(bootstrap, "menu_import_drafts") {
		t.Error("development runtime grant is missing menu_import_drafts")
	}
}

func TestOperationalResilienceEvidenceIsTenantScopedAndVersioned(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000009_operational_resilience_reconciliation.up.sql")
	for _, fragment := range []string{"CREATE TABLE configuration_snapshots", "CREATE TABLE edge_sync_checkpoints", "CREATE TABLE reconciliation_cases", "CREATE TABLE reconciliation_actions", "CREATE TABLE operational_incidents", "CREATE TABLE incident_events", "CREATE TABLE backup_manifests", "CREATE TABLE restore_drills", "FORCE ROW LEVEL SECURITY", "version bigint", "reject_immutable_change"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("operational resilience migration is missing %q", fragment)
		}
	}
}

func TestDailyOperationsMVPIsTenantScopedAndReceiptEvidenceIsImmutable(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000010_daily_operations_mvp.up.sql")
	for _, fragment := range []string{"CREATE TABLE suppliers", "CREATE TABLE purchase_orders", "CREATE TABLE goods_receipts", "CREATE TABLE temperature_logs", "CREATE TABLE operational_checklists", "CREATE TABLE staff_members", "CREATE TABLE staff_shifts", "CREATE TABLE operational_tasks", "goods_receipts_immutable", "temperature_logs_immutable", "FORCE ROW LEVEL SECURITY"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("daily operations migration is missing %q", fragment)
		}
	}
}

func TestNativeCommerceLedgersAreImmutableAndTenantScoped(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000011_native_commerce.up.sql")
	for _, fragment := range []string{"CREATE TABLE menu_item_availability", "CREATE TABLE dining_tables", "CREATE TABLE dining_sessions", "CREATE TABLE cash_shifts", "CREATE TABLE cash_events", "CREATE TABLE tenders", "CREATE TABLE fiscal_receipts", "CREATE TABLE tender_settlements", "tenders_immutable", "fiscal_receipts_immutable", "FORCE ROW LEVEL SECURITY"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("native commerce migration is missing %q", fragment)
		}
	}
}

func TestGuestGrowthAndRefundsAreVersionedAndTenantScoped(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000012_guest_growth_refunds.up.sql")
	for _, fragment := range []string{"CREATE TABLE guest_profiles", "CREATE TABLE guest_consent_events", "CREATE TABLE reservations", "CREATE TABLE promotions", "CREATE TABLE promotion_redemptions", "CREATE TABLE loyalty_accounts", "CREATE TABLE loyalty_events", "cash_refund", "reject_immutable_change", "FORCE ROW LEVEL SECURITY"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("guest growth migration is missing %q", fragment)
		}
	}
}

func TestDailyDashboardIndexesCoverEventTimeReads(t *testing.T) {
	t.Parallel()
	up := readMigration(t, "000013_daily_dashboard_indexes.up.sql")
	for _, fragment := range []string{
		"CREATE INDEX tenders_dashboard_idx",
		"CREATE INDEX fiscal_receipts_dashboard_idx",
		"CREATE INDEX promotion_redemptions_dashboard_idx",
		"tenant_id, outlet_id, occurred_at, id",
		"tenant_id, outlet_id, issued_at, id",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("daily dashboard index migration is missing %q", fragment)
		}
	}
}

func readMigration(t *testing.T, name string) string {
	t.Helper()
	return readFile(t, name)
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(contents)
}
