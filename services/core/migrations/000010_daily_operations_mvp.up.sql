-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;

CREATE TABLE suppliers (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), name text NOT NULL, code text NOT NULL,
 contact_name text NOT NULL DEFAULT '', phone text NOT NULL DEFAULT '', email text NOT NULL DEFAULT '', tax_id text NOT NULL DEFAULT '',
 active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1 CHECK(version>0), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE TABLE purchase_orders (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid NOT NULL, supplier_id uuid NOT NULL,
 po_number text NOT NULL, status text NOT NULL CHECK(status IN('draft','submitted','partially_received','received','cancelled')),
 expected_at timestamptz, currency char(3) NOT NULL, notes text NOT NULL DEFAULT '', total_minor bigint NOT NULL DEFAULT 0 CHECK(total_minor>=0),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,po_number), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id), FOREIGN KEY(tenant_id,supplier_id) REFERENCES suppliers(tenant_id,id)
);
CREATE TABLE purchase_order_lines (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), purchase_order_id uuid NOT NULL, ingredient_id uuid NOT NULL, unit_id uuid NOT NULL,
 ordered_quantity numeric(18,6) NOT NULL CHECK(ordered_quantity>0), received_quantity numeric(18,6) NOT NULL DEFAULT 0 CHECK(received_quantity>=0 AND received_quantity<=ordered_quantity),
 unit_cost_minor bigint NOT NULL CHECK(unit_cost_minor>=0), UNIQUE(tenant_id,id), UNIQUE(tenant_id,purchase_order_id,ingredient_id),
 FOREIGN KEY(tenant_id,purchase_order_id) REFERENCES purchase_orders(tenant_id,id), FOREIGN KEY(tenant_id,ingredient_id) REFERENCES ingredients(tenant_id,id), FOREIGN KEY(tenant_id,unit_id) REFERENCES units(tenant_id,id)
);
CREATE TABLE goods_receipts (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid NOT NULL, purchase_order_id uuid NOT NULL,
 received_at timestamptz NOT NULL, supplier_document text NOT NULL DEFAULT '', notes text NOT NULL DEFAULT '', actor_id text NOT NULL, operation_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,operation_id), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id), FOREIGN KEY(tenant_id,purchase_order_id) REFERENCES purchase_orders(tenant_id,id)
);
CREATE TABLE goods_receipt_lines (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), goods_receipt_id uuid NOT NULL, purchase_order_line_id uuid NOT NULL,
 ingredient_id uuid NOT NULL, unit_id uuid NOT NULL, quantity numeric(18,6) NOT NULL CHECK(quantity>0), unit_cost_minor bigint NOT NULL CHECK(unit_cost_minor>=0),
 lot_code text NOT NULL DEFAULT '', expires_at timestamptz, inventory_event_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,inventory_event_id), FOREIGN KEY(tenant_id,goods_receipt_id) REFERENCES goods_receipts(tenant_id,id),
 FOREIGN KEY(tenant_id,purchase_order_line_id) REFERENCES purchase_order_lines(tenant_id,id), FOREIGN KEY(tenant_id,ingredient_id) REFERENCES ingredients(tenant_id,id), FOREIGN KEY(tenant_id,unit_id) REFERENCES units(tenant_id,id),
 FOREIGN KEY(tenant_id,inventory_event_id) REFERENCES inventory_events(tenant_id,id)
);

CREATE TABLE temperature_logs (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid NOT NULL, location text NOT NULL,
 temperature_c numeric(6,2) NOT NULL CHECK(temperature_c BETWEEN -100 AND 300), safe_min_c numeric(6,2) NOT NULL, safe_max_c numeric(6,2) NOT NULL,
 compliant boolean NOT NULL, corrective_action text NOT NULL DEFAULT '', measured_at timestamptz NOT NULL, actor_id text NOT NULL, operation_id uuid NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,operation_id), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id), CHECK(safe_min_c<=safe_max_c)
);

CREATE TABLE operational_checklists (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid NOT NULL, checklist_type text NOT NULL CHECK(checklist_type IN('opening','closing','food_safety')),
 business_date date NOT NULL, status text NOT NULL CHECK(status IN('open','completed')), version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL, completed_at timestamptz, updated_at timestamptz NOT NULL,
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id)
);
CREATE TABLE operational_checklist_items (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), checklist_id uuid NOT NULL, label text NOT NULL, required boolean NOT NULL DEFAULT true,
 completed boolean NOT NULL DEFAULT false, completed_by text NOT NULL DEFAULT '', completed_at timestamptz, position integer NOT NULL CHECK(position>=0),
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,checklist_id) REFERENCES operational_checklists(tenant_id,id)
);

CREATE TABLE staff_members (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), employee_code text NOT NULL, display_name text NOT NULL, role text NOT NULL,
 phone text NOT NULL DEFAULT '', active boolean NOT NULL DEFAULT true, version bigint NOT NULL DEFAULT 1 CHECK(version>0), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,employee_code)
);
CREATE TABLE staff_shifts (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid NOT NULL, staff_member_id uuid NOT NULL,
 starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL, station_id uuid, status text NOT NULL CHECK(status IN('scheduled','checked_in','completed','cancelled')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id), FOREIGN KEY(tenant_id,staff_member_id) REFERENCES staff_members(tenant_id,id), FOREIGN KEY(tenant_id,outlet_id,station_id) REFERENCES stations(tenant_id,outlet_id,id), CHECK(ends_at>starts_at)
);
CREATE TABLE operational_tasks (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL REFERENCES tenants(id), outlet_id uuid NOT NULL, staff_member_id uuid,
 title text NOT NULL, due_at timestamptz, priority text NOT NULL CHECK(priority IN('low','normal','high')), status text NOT NULL CHECK(status IN('open','completed','cancelled')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), created_at timestamptz NOT NULL, completed_at timestamptz, updated_at timestamptz NOT NULL,
 UNIQUE(tenant_id,id), FOREIGN KEY(tenant_id,outlet_id) REFERENCES outlets(tenant_id,id), FOREIGN KEY(tenant_id,staff_member_id) REFERENCES staff_members(tenant_id,id)
);

CREATE TRIGGER goods_receipts_immutable BEFORE UPDATE OR DELETE ON goods_receipts FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER goods_receipt_lines_immutable BEFORE UPDATE OR DELETE ON goods_receipt_lines FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();
CREATE TRIGGER temperature_logs_immutable BEFORE UPDATE OR DELETE ON temperature_logs FOR EACH ROW EXECUTE FUNCTION feastcloud.reject_immutable_change();

DO $$ DECLARE table_name text; BEGIN
 FOREACH table_name IN ARRAY ARRAY['suppliers','purchase_orders','purchase_order_lines','goods_receipts','goods_receipt_lines','temperature_logs','operational_checklists','operational_checklist_items','staff_members','staff_shifts','operational_tasks'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',table_name); EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',table_name);
  EXECUTE format('CREATE POLICY %I ON %I USING (tenant_id=feastcloud.current_tenant_id()) WITH CHECK (tenant_id=feastcloud.current_tenant_id())',table_name||'_isolation',table_name);
 END LOOP;
END $$;
CREATE INDEX purchase_orders_queue_idx ON purchase_orders(tenant_id,outlet_id,status,expected_at,id);
CREATE INDEX temperature_logs_recent_idx ON temperature_logs(tenant_id,outlet_id,measured_at DESC,id);
CREATE INDEX operational_checklists_daily_idx ON operational_checklists(tenant_id,outlet_id,business_date,status,id);
CREATE INDEX staff_shifts_schedule_idx ON staff_shifts(tenant_id,outlet_id,starts_at,id);
CREATE INDEX operational_tasks_queue_idx ON operational_tasks(tenant_id,outlet_id,status,due_at,id);
COMMIT;
