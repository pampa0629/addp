-- 025_drop_raster_cog_source_locator.sql
-- Clean break: raster_cog.locator is the canonical source item ResourceLocator.
-- The generated COG object is identified by storage_ref, so source_locator is duplicate state.

ALTER TABLE manager.raster_cog
    DROP COLUMN IF EXISTS source_locator;
