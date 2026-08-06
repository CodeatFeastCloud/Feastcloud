-- SPDX-License-Identifier: AGPL-3.0-only
-- Development and CI bootstrap only. Production credentials must come from a
-- secret manager and roles must be provisioned outside application migrations.

\set ON_ERROR_STOP on

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'feastcloud_runtime') THEN
        CREATE ROLE feastcloud_runtime
            LOGIN
            PASSWORD 'feastcloud_dev_runtime'
            NOSUPERUSER
            NOCREATEDB
            NOCREATEROLE
            NOINHERIT
            NOBYPASSRLS;
    ELSE
        ALTER ROLE feastcloud_runtime
            WITH LOGIN PASSWORD 'feastcloud_dev_runtime'
            NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
    END IF;
END
$$;

ALTER ROLE feastcloud_runtime SET statement_timeout = '15s';
ALTER ROLE feastcloud_runtime SET lock_timeout = '5s';
ALTER ROLE feastcloud_runtime SET idle_in_transaction_session_timeout = '30s';

GRANT CONNECT ON DATABASE feastcloud TO feastcloud_runtime;
GRANT USAGE ON SCHEMA public, feastcloud TO feastcloud_runtime;
GRANT SELECT, INSERT, UPDATE ON sync_inbox TO feastcloud_runtime;
GRANT SELECT, INSERT ON domain_events TO feastcloud_runtime;
GRANT SELECT, INSERT ON
    organizations, outlets, brands, brand_outlet_assignments, stations, orders, order_lines,
    kitchen_tickets, ticket_lines, audit_events
TO feastcloud_runtime;
GRANT UPDATE (active, version, updated_at) ON brand_outlet_assignments TO feastcloud_runtime;
GRANT SELECT, INSERT, DELETE ON idempotency_records TO feastcloud_runtime;
GRANT UPDATE (status, version, updated_at) ON orders, kitchen_tickets TO feastcloud_runtime;
GRANT SELECT, INSERT ON identity_devices TO feastcloud_runtime;
GRANT UPDATE (status, revoked_by, revoked_at, version) ON identity_devices TO feastcloud_runtime;
GRANT SELECT, INSERT ON
    units, ingredients, recipes, recipe_versions, recipe_components,
    menu_items, order_line_recipe_snapshots, inventory_events,
    inventory_counts, inventory_count_lines
TO feastcloud_runtime;
GRANT SELECT, INSERT ON production_batches TO feastcloud_runtime;
GRANT UPDATE (status, actual_quantity, started_at, completed_at, expires_at, lot_code, notes, version, updated_at) ON production_batches TO feastcloud_runtime;
GRANT SELECT, INSERT ON order_imports, order_import_rows, menu_import_drafts, planning_runs, planning_recommendations TO feastcloud_runtime;
GRANT SELECT, INSERT ON configuration_snapshots, reconciliation_actions, incident_events, backup_manifests, restore_drills TO feastcloud_runtime;
GRANT SELECT, INSERT, UPDATE ON edge_sync_checkpoints, reconciliation_cases, operational_incidents TO feastcloud_runtime;
GRANT SELECT, INSERT ON suppliers, purchase_orders, purchase_order_lines, goods_receipts, goods_receipt_lines, temperature_logs, operational_checklists, operational_checklist_items, staff_members, staff_shifts, operational_tasks TO feastcloud_runtime;
GRANT UPDATE (status, total_minor, version, updated_at) ON purchase_orders TO feastcloud_runtime;
GRANT UPDATE (received_quantity) ON purchase_order_lines TO feastcloud_runtime;
GRANT UPDATE (status, version, completed_at, updated_at) ON operational_checklists TO feastcloud_runtime;
GRANT UPDATE (completed, completed_by, completed_at) ON operational_checklist_items TO feastcloud_runtime;
GRANT UPDATE (status, version, updated_at) ON staff_shifts TO feastcloud_runtime;
GRANT UPDATE (status, version, completed_at, updated_at) ON operational_tasks TO feastcloud_runtime;
GRANT SELECT,INSERT ON menu_item_availability,menu_availability_events,dining_tables,dining_sessions,cash_shifts,cash_events,tenders,fiscal_receipts,tender_settlements TO feastcloud_runtime;
GRANT UPDATE (available,reason,version,updated_at) ON menu_item_availability TO feastcloud_runtime;
GRANT UPDATE (status,version,updated_at) ON dining_tables TO feastcloud_runtime;
GRANT UPDATE (status,closed_at,version) ON dining_sessions TO feastcloud_runtime;
GRANT UPDATE (status,expected_cash_minor,closing_count_minor,variance_minor,closed_at,version) ON cash_shifts TO feastcloud_runtime;
GRANT SELECT,INSERT ON guest_profiles,guest_consent_events,reservations,promotions,promotion_redemptions,loyalty_accounts,loyalty_events TO feastcloud_runtime;
GRANT UPDATE (marketing_consent,consent_updated_at,version,updated_at) ON guest_profiles TO feastcloud_runtime;
GRANT UPDATE (status,version,updated_at) ON reservations TO feastcloud_runtime;
GRANT UPDATE (active,redemption_count,version,updated_at) ON promotions TO feastcloud_runtime;
GRANT UPDATE (points_balance,lifetime_earned,version,updated_at) ON loyalty_accounts TO feastcloud_runtime;
GRANT UPDATE (version, updated_at) ON recipes TO feastcloud_runtime;
GRANT UPDATE (effective_to) ON recipe_versions TO feastcloud_runtime;
GRANT SELECT, INSERT ON sales_channels, channel_menu_items, connector_installations,
    connector_order_inbox, kitchen_print_jobs, pickup_tokens, qr_ordering_links,
    stock_transfers, stock_transfer_lines, hardware_devices, implementation_runbooks
TO feastcloud_runtime;
GRANT SELECT, INSERT ON menu_studios, menu_studio_versions, menu_studio_categories,
    menu_modifier_groups, menu_modifier_options, menu_version_items,
    menu_version_item_modifiers, menu_version_item_prices, menu_publications,
    order_line_modifier_snapshots, pos_checkout_records, kitchen_print_job_events,
    pickup_token_events
TO feastcloud_runtime;
GRANT SELECT, INSERT ON connector_order_decisions TO feastcloud_runtime;
GRANT SELECT, INSERT ON web_order_requests TO feastcloud_runtime;
GRANT SELECT, INSERT ON stock_transfer_events TO feastcloud_runtime;
GRANT SELECT, INSERT, UPDATE ON replenishment_rules TO feastcloud_runtime;
GRANT UPDATE (status, dispatched_at, received_at, version) ON stock_transfers TO feastcloud_runtime;
GRANT UPDATE (dispatched_quantity_base, received_quantity_base) ON stock_transfer_lines TO feastcloud_runtime;
GRANT SELECT, INSERT, UPDATE ON station_capacity_limits, outlet_control_profiles
TO feastcloud_runtime;
GRANT UPDATE (status, current_version_id, version, updated_at) ON menu_studios TO feastcloud_runtime;
GRANT UPDATE (status, attempts, acknowledged_at) ON kitchen_print_jobs TO feastcloud_runtime;
GRANT UPDATE (status, collected_at, version) ON pickup_tokens TO feastcloud_runtime;

REVOKE DELETE ON sync_inbox, domain_events FROM feastcloud_runtime;
REVOKE UPDATE ON domain_events FROM feastcloud_runtime;
REVOKE UPDATE, DELETE ON inventory_events, order_line_recipe_snapshots,
    inventory_counts, inventory_count_lines FROM feastcloud_runtime;

INSERT INTO tenants (id, name) VALUES
    ('11111111-1111-4111-8111-111111111111', 'FeastCloud Integration Tenant A'),
    ('22222222-2222-4222-8222-222222222222', 'FeastCloud Integration Tenant B')
ON CONFLICT (id) DO NOTHING;

INSERT INTO organizations (
    id, tenant_id, name, legal_name, default_locale, default_currency
) VALUES
    (
        '11111111-1111-4111-8111-111111111111',
        '11111111-1111-4111-8111-111111111111',
        'FeastCloud Integration Tenant A',
        'FeastCloud Integration Tenant A',
        'en-IN',
        'INR'
    ),
    (
        '22222222-2222-4222-8222-222222222222',
        '22222222-2222-4222-8222-222222222222',
        'FeastCloud Integration Tenant B',
        'FeastCloud Integration Tenant B',
        'en-IN',
        'INR'
    )
ON CONFLICT (id) DO NOTHING;

INSERT INTO outlets (
    id, tenant_id, organization_id, name, code, time_zone, currency
) VALUES
    (
        '33333333-3333-4333-8333-333333333333',
        '11111111-1111-4111-8111-111111111111',
        '11111111-1111-4111-8111-111111111111',
        'Integration Outlet A',
        'integration-a',
        'Asia/Kolkata',
        'INR'
    ),
    (
        '55555555-5555-4555-8555-555555555555',
        '22222222-2222-4222-8222-222222222222',
        '22222222-2222-4222-8222-222222222222',
        'Integration Outlet B',
        'integration-b',
        'Asia/Kolkata',
        'INR'
    )
ON CONFLICT (id) DO NOTHING;
