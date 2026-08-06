#!/bin/sh
# SPDX-License-Identifier: AGPL-3.0-only

set -eu

repository_root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
state_root="$repository_root/.feastcloud/postgres"
data_directory="$state_root/data"
log_file="$state_root/postgres.log"
postgres_port=${FEASTCLOUD_POSTGRES_PORT:-54329}
admin_user=feastcloud_admin
admin_password=feastcloud_dev_admin
database_name=feastcloud
runtime_url="postgres://feastcloud_runtime:feastcloud_dev_runtime@127.0.0.1:${postgres_port}/${database_name}?sslmode=disable"

case "$postgres_port" in
  ''|*[!0-9]*)
    echo "FEASTCLOUD_POSTGRES_PORT must be a numeric TCP port" >&2
    exit 1
    ;;
esac
if [ "$postgres_port" -lt 1024 ] || [ "$postgres_port" -gt 65535 ]; then
  echo "FEASTCLOUD_POSTGRES_PORT must be between 1024 and 65535" >&2
  exit 1
fi

if ! command -v brew >/dev/null 2>&1; then
  echo "Homebrew is required for the native PostgreSQL profile" >&2
  exit 1
fi
if ! postgres_prefix=$(brew --prefix postgresql@17 2>/dev/null); then
  echo "PostgreSQL 17 is not installed. Run: brew install postgresql@17" >&2
  exit 1
fi

pg_config="$postgres_prefix/bin/pg_config"
if [ ! -f "$("$pg_config" --sharedir)/postgres.bki" ] || [ ! -d "$("$pg_config" --pkglibdir)" ]; then
  echo "PostgreSQL's Homebrew paths are incomplete. Run: npm run db:install" >&2
  exit 1
fi

initdb="$postgres_prefix/bin/initdb"
pg_ctl="$postgres_prefix/bin/pg_ctl"
psql="$postgres_prefix/bin/psql"
createdb="$postgres_prefix/bin/createdb"

mkdir -p "$state_root"

is_running() {
  [ -f "$data_directory/PG_VERSION" ] && "$pg_ctl" -D "$data_directory" status >/dev/null 2>&1
}

initialize_cluster() {
  if [ -f "$data_directory/PG_VERSION" ]; then
    return
  fi
  password_file="$state_root/admin-password.tmp"
  umask 077
  printf '%s\n' "$admin_password" > "$password_file"
  trap 'rm -f "$password_file"' EXIT HUP INT TERM
  "$initdb" \
    -D "$data_directory" \
    --username="$admin_user" \
    --pwfile="$password_file" \
    --encoding=UTF8 \
    --locale=C \
    --auth-local=scram-sha-256 \
    --auth-host=scram-sha-256
  rm -f "$password_file"
  trap - EXIT HUP INT TERM
}

start_server() {
  initialize_cluster
  if ! is_running; then
    "$pg_ctl" \
      -D "$data_directory" \
      -l "$log_file" \
      -o "-h 127.0.0.1 -p $postgres_port -c unix_socket_directories=" \
      -w \
      -t 30 \
      start
  fi
}

admin_psql() {
  PGPASSWORD=$admin_password "$psql" \
    -h 127.0.0.1 \
    -p "$postgres_port" \
    -U "$admin_user" \
    "$@"
}

prepare_database() {
  if [ "$(admin_psql -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$database_name'")" != "1" ]; then
    PGPASSWORD=$admin_password "$createdb" \
      -h 127.0.0.1 \
      -p "$postgres_port" \
      -U "$admin_user" \
      "$database_name"
  fi

  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.sync_inbox') IS NOT NULL")" != "t" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 \
      -f "$repository_root/services/core/migrations/000001_foundation.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.domain_events') IS NOT NULL")" != "t" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 \
      -f "$repository_root/services/core/migrations/000002_domain_events.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'audit_events' AND column_name = 'outlet_id'")" != "YES" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 \
      -f "$repository_root/services/core/migrations/000003_nullable_organization_audit.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.identity_devices') IS NOT NULL")" != "t" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 \
      -f "$repository_root/services/core/migrations/000004_identity_devices.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.inventory_events') IS NOT NULL")" != "t" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 \
      -f "$repository_root/services/core/migrations/000005_kitchen_graph_inventory.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.inventory_counts') IS NOT NULL")" != "t" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 \
      -f "$repository_root/services/core/migrations/000006_inventory_counts.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.production_batches') IS NOT NULL")" != "t" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 \
      -f "$repository_root/services/core/migrations/000007_production_batches.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.planning_runs') IS NOT NULL")" != "t" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 \
      -f "$repository_root/services/core/migrations/000008_imports_planning.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.reconciliation_cases') IS NOT NULL")" != "t" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000009_operational_resilience_reconciliation.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.purchase_orders') IS NOT NULL")" != "t" ]; then
    admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000010_daily_operations_mvp.up.sql"
  fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.tenders') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000011_native_commerce.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.guest_profiles') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000012_guest_growth_refunds.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.tenders_dashboard_idx') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000013_daily_dashboard_indexes.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.sales_channels') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000014_connected_commerce.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.menu_studios') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000015_restaurant_core.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.connector_order_decisions') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000016_aggregator_inbox_decisions.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.web_order_requests') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000017_qr_web_order_requests.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.stock_transfer_events') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000018_stock_transfer_execution.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.replenishment_rules') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000019_replenishment_rules.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.brand_outlet_assignments') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000020_brand_outlet_assignments.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT to_regclass('public.menu_import_drafts') IS NOT NULL")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000021_menu_import_drafts.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT is_nullable FROM information_schema.columns WHERE table_schema='public' AND table_name='menu_items' AND column_name='recipe_id'")" != "YES" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000022_optional_menu_item_recipe.up.sql"; fi
  if [ "$(admin_psql -d "$database_name" -tAc "SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='connector_installations' AND column_name='configuration')")" != "t" ]; then admin_psql -d "$database_name" -v ON_ERROR_STOP=1 -f "$repository_root/services/core/migrations/000023_connector_external_outlets.up.sql"; fi
  admin_psql -d "$database_name" -v ON_ERROR_STOP=1 \
    -f "$repository_root/deploy/postgres/003_development_bootstrap.sql"
}

start_profile() {
  start_server
  prepare_database
  echo "Native FeastCloud PostgreSQL is ready on 127.0.0.1:$postgres_port"
  echo "Runtime URL: $runtime_url"
}

stop_profile() {
  if is_running; then
    "$pg_ctl" -D "$data_directory" -w -t 30 -m fast stop
  else
    echo "Native FeastCloud PostgreSQL is already stopped"
  fi
}

case "${1:-start}" in
  start)
    start_profile
    ;;
  stop)
    stop_profile
    ;;
  status)
    if is_running; then
      echo "Native FeastCloud PostgreSQL is running on 127.0.0.1:$postgres_port"
    else
      echo "Native FeastCloud PostgreSQL is stopped"
      exit 1
    fi
    ;;
  test)
    start_profile
    cd "$repository_root"
    FEASTCLOUD_TEST_DATABASE_URL=${FEASTCLOUD_TEST_DATABASE_URL:-$runtime_url} \
	  go test ./services/core/internal/idempotency ./services/core/internal/store \
	  -run '^(TestPostgres(StoreReplaysResponseAfterRestart|(Resource|Sync)RepositoryIntegration)|TestDailyOperationsMVPIntegration|TestNativeCommerceIntegration|TestConnectedCommercePersistsTheSharedOrderFlow|TestRestaurantCoreIntegration|TestDirectOrderingIntegration|TestStockTransferExecutionIntegration|TestGuestGrowthIntegration|TestDailyDashboardPostgresIntegration)$' \
      -count=1
    ;;
  logs)
    if [ ! -f "$log_file" ]; then
      echo "PostgreSQL has not written a log yet" >&2
      exit 1
    fi
    tail -f "$log_file"
    ;;
  *)
    echo "Usage: $0 {start|stop|status|test|logs}" >&2
    exit 2
    ;;
esac
