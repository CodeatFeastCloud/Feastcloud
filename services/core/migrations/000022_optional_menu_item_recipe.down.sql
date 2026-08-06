BEGIN;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM menu_items WHERE recipe_id IS NULL) THEN
        RAISE EXCEPTION 'cannot require menu item recipes while recipe-less menu items exist';
    END IF;
END $$;
ALTER TABLE menu_items ALTER COLUMN recipe_id SET NOT NULL;
COMMIT;
