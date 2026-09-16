-- Quality owns execution policies; a warehouse layer only describes model organization.
UPDATE model.dw_layers SET version = version + 1 WHERE quality_sla IS NOT NULL;
ALTER TABLE model.dw_layers DROP COLUMN quality_sla;
