BEGIN;

CREATE TABLE system.develop_file_publications (
    approval_id uuid PRIMARY KEY REFERENCES system.develop_file_create_approvals(id),
    operation_id uuid NOT NULL UNIQUE,
    request_digest text NOT NULL CHECK (request_digest ~ '^[0-9a-f]{64}$'),
    lease_digest text NOT NULL CHECK (lease_digest ~ '^[0-9a-f]{64}$'),
    prepare_admitted_at timestamptz NOT NULL,
    released_at timestamptz,
    preparation_digest text,
    CHECK ((released_at IS NULL AND preparation_digest IS NULL) OR
           (released_at IS NOT NULL AND released_at >= prepare_admitted_at AND
            preparation_digest IS NOT NULL AND preparation_digest ~ '^[0-9a-f]{64}$'))
);

COMMIT;
