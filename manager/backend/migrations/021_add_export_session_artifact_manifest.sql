-- 021_add_export_session_artifact_manifest.sql

ALTER TABLE manager.export_sessions
    ADD COLUMN IF NOT EXISTS artifact_manifest JSONB NOT NULL DEFAULT '{}';
