-- Untyped legacy expressions cannot be promoted into executable published
-- revisions. Refuse to discard user data; recreate legacy entries explicitly.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM model.metric_implementations) THEN
  RAISE EXCEPTION 'Metric implementations must be reviewed before independent revision migration';
 END IF;
END $$;
DROP TABLE model.metric_implementations;
CREATE TABLE model.metric_implementations (
 id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL,
 fact_table_id BIGINT NOT NULL, metric_definition_id BIGINT NOT NULL,
 name VARCHAR(200) NOT NULL, note TEXT NOT NULL DEFAULT '',
 version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
 created_by BIGINT NOT NULL, updated_by BIGINT,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 UNIQUE(id,tenant_id),
 FOREIGN KEY(fact_table_id,tenant_id) REFERENCES model.logical_tables(id,tenant_id) ON DELETE RESTRICT
);
CREATE INDEX idx_metric_implementation_fact ON model.metric_implementations(tenant_id,fact_table_id);
CREATE INDEX idx_metric_implementation_definition ON model.metric_implementations(tenant_id,metric_definition_id);
CREATE TABLE model.metric_implementation_revisions (
 id BIGSERIAL PRIMARY KEY, tenant_id BIGINT NOT NULL,
 implementation_id BIGINT NOT NULL, revision_no BIGINT NOT NULL CHECK(revision_no>0),
 metric_definition_revision_id BIGINT NOT NULL,
 contract JSONB NOT NULL CHECK(jsonb_typeof(contract)='object'),
 dependency_snapshot JSONB NOT NULL CHECK(jsonb_typeof(dependency_snapshot)='object'),
 dependency_hash TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('draft','published','withdrawn')),
 published_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 FOREIGN KEY(implementation_id,tenant_id) REFERENCES model.metric_implementations(id,tenant_id) ON DELETE CASCADE,
 UNIQUE(tenant_id,implementation_id,revision_no)
);
CREATE UNIQUE INDEX uq_metric_implementation_draft ON model.metric_implementation_revisions(tenant_id,implementation_id) WHERE status='draft';
