BEGIN;

CREATE TABLE system.develop_file_create_approvals (
    id uuid PRIMARY KEY,
    authorization_id bigint NOT NULL UNIQUE REFERENCES system.execution_authorizations(id),
    scope_digest text NOT NULL CHECK (scope_digest ~ '^[0-9a-f]{64}$'),
    approver_principal_id bigint NOT NULL REFERENCES system.principals(id),
    approver_membership_id bigint NOT NULL REFERENCES system.tenant_memberships(id),
    approver_authorization_version bigint NOT NULL CHECK (approver_authorization_version > 0),
    approved_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > approved_at)
);

INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,
 delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES ('system.develop_file_create_approval.create','system','create','high',false,ARRAY['tenant']::text[],true,
 'permissions.system.develop_file_create_approval.create.name',
 'permissions.system.develop_file_create_approval.create.description','active');

COMMIT;
