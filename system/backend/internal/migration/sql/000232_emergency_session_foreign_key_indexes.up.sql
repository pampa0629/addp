BEGIN;

-- 000231 has already been published. Its tenant/request-leading indexes
-- serve the application queries; these leading keys support the FK lookups.
CREATE INDEX idx_emergency_session_executor_principal
    ON system.emergency_session_requests(executor_principal_id);
CREATE INDEX idx_emergency_session_executor_membership
    ON system.emergency_session_requests(executor_membership_id);
CREATE INDEX idx_emergency_session_target_principal
    ON system.emergency_session_requests(target_principal_id);
CREATE INDEX idx_emergency_session_target_membership
    ON system.emergency_session_requests(target_membership_id);
CREATE INDEX idx_emergency_session_target_family
    ON system.emergency_session_requests(target_family_id);
CREATE INDEX idx_emergency_session_closed_by_principal
    ON system.emergency_session_requests(closed_by_principal_id);
CREATE INDEX idx_emergency_session_approval_principal
    ON system.emergency_session_approvals(principal_id);

COMMIT;
