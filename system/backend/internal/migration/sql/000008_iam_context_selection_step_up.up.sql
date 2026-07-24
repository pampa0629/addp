BEGIN;

ALTER TABLE system.context_selection_tickets
    ADD COLUMN step_up_expires_at timestamptz,
    ADD CONSTRAINT ck_context_selection_tickets_step_up
        CHECK (
            step_up_expires_at IS NULL
            OR step_up_expires_at >= authenticated_at
        );

CREATE OR REPLACE FUNCTION system.validate_context_selection_ticket_update()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.token_hash <> OLD.token_hash
       OR NEW.principal_id <> OLD.principal_id
       OR NEW.issued_authorization_version <> OLD.issued_authorization_version
       OR NEW.client_id <> OLD.client_id
       OR NEW.authentication_methods <> OLD.authentication_methods
       OR NEW.assurance_level <> OLD.assurance_level
       OR NEW.authenticated_at <> OLD.authenticated_at
       OR NEW.step_up_expires_at IS DISTINCT FROM OLD.step_up_expires_at
       OR NEW.expires_at <> OLD.expires_at
       OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'context selection ticket facts are immutable'
            USING ERRCODE = '23514';
    END IF;
    IF OLD.consumed_at IS NOT NULL OR NEW.consumed_at IS NULL THEN
        RAISE EXCEPTION 'context selection ticket may only be consumed once'
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;

COMMIT;
