BEGIN;

-- Explicitly approved development reset, including the two formerly retained
-- long-lived Grants. Do not touch configurations (mapped by the subsequent
-- permission-model migration), IAM, delegations, collection qualifications,
-- source data, or generic audit. Deploy with all old issuers stopped.
LOCK TABLE system.engine_access_grant_revocations, system.engine_access_grants,
 system.engine_access_initial_grant_outcomes, system.engine_access_fulfillment_outcomes,
 system.engine_access_deny_releases, system.engine_access_denies IN ACCESS EXCLUSIVE MODE;
ALTER TABLE system.engine_access_grant_revocations DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_grants DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_initial_grant_outcomes DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_fulfillment_outcomes DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_deny_releases DISABLE TRIGGER USER;
ALTER TABLE system.engine_access_denies DISABLE TRIGGER USER;

-- Both directions of the production outcome / Grant relation are deferred.
-- Keep only the terminal refusal keys: forgetting them would make a replay of
-- an already issued production look like a first-ever authorization request.
SET CONSTRAINTS ALL DEFERRED;
DELETE FROM system.engine_access_grant_revocations;
DELETE FROM system.engine_access_grants;
UPDATE system.engine_access_initial_grant_outcomes
 SET outcome = 'denied', grant_request_id = NULL, denial_code = 'target_history_exists'
 WHERE outcome = 'issued';
DELETE FROM system.engine_access_fulfillment_outcomes;
DELETE FROM system.engine_access_deny_releases;
DELETE FROM system.engine_access_denies;
SET CONSTRAINTS ALL IMMEDIATE;

ALTER TABLE system.engine_access_grant_revocations ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_grants ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_initial_grant_outcomes ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_fulfillment_outcomes ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_deny_releases ENABLE TRIGGER USER;
ALTER TABLE system.engine_access_denies ENABLE TRIGGER USER;

COMMIT;
