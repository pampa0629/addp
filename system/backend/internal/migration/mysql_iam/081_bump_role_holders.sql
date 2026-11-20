CREATE PROCEDURE IF NOT EXISTS bump_role_holders(IN target_role BIGINT)
MODIFIES SQL DATA
UPDATE principals p SET p.authorization_version=p.authorization_version+1
WHERE EXISTS (SELECT 1 FROM role_assignments a WHERE a.principal_id=p.id AND a.role_id=target_role AND a.status='active')
