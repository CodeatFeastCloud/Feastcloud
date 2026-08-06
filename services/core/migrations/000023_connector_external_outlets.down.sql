-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;
ALTER TABLE connector_installations DROP CONSTRAINT IF EXISTS connector_installations_configuration_object;
ALTER TABLE connector_installations DROP COLUMN IF EXISTS configuration;
COMMIT;
