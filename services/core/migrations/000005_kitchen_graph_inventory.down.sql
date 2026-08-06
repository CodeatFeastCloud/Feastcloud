-- SPDX-License-Identifier: AGPL-3.0-only
BEGIN;
DROP TABLE IF EXISTS inventory_events;
DROP TABLE IF EXISTS order_line_recipe_snapshots;
DROP TABLE IF EXISTS menu_items;
DROP TABLE IF EXISTS recipe_components;
DROP TABLE IF EXISTS recipe_versions;
DROP TABLE IF EXISTS recipes;
DROP TABLE IF EXISTS ingredients;
DROP TABLE IF EXISTS units;
COMMIT;
