INSERT INTO search_authorization_revisions(tenant_id,version)
SELECT DISTINCT tenant_id,1 FROM engine_access_grants WHERE tenant_id > 0
ON DUPLICATE KEY UPDATE version=search_authorization_revisions.version
