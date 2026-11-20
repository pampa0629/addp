CREATE PROCEDURE IF NOT EXISTS bump_organization_member(IN member BIGINT)
MODIFIES SQL DATA
UPDATE principals SET authorization_version=authorization_version+1
WHERE id=(SELECT principal_id FROM tenant_memberships WHERE id=member)
