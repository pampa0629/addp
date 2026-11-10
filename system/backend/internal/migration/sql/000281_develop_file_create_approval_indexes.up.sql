BEGIN;

CREATE INDEX idx_develop_file_create_approvals_approver_principal
    ON system.develop_file_create_approvals (approver_principal_id);
CREATE INDEX idx_develop_file_create_approvals_approver_membership
    ON system.develop_file_create_approvals (approver_membership_id);

COMMIT;
