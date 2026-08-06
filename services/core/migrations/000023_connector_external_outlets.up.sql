-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;

-- Provider accounts frequently expose several virtual brands from one physical
-- kitchen. The mapping is non-secret connector configuration; credentials stay
-- in credential_reference and are resolved only by the connector runtime.
ALTER TABLE connector_installations
    ADD COLUMN configuration jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE connector_installations
    ADD CONSTRAINT connector_installations_configuration_object
    CHECK (jsonb_typeof(configuration) = 'object');

COMMIT;
