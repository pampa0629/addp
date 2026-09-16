DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM common.task_executions WHERE module='quality' AND status IN ('pending','running')) THEN
  RAISE EXCEPTION 'drain active Quality executions before extracting reusable rules';
 END IF;
END $$;

CREATE TABLE quality.rules (
 id BIGSERIAL PRIMARY KEY,
 tenant_id BIGINT NOT NULL,
 code VARCHAR(100) NOT NULL CHECK (code ~ '^[a-z][a-z0-9_]*$'),
 version BIGINT NOT NULL DEFAULT 1 CHECK(version>0),
 revision_no BIGINT NOT NULL DEFAULT 1 CHECK(revision_no>0),
 name VARCHAR(200) NOT NULL,
 description TEXT NOT NULL DEFAULT '',
 type VARCHAR(40) NOT NULL,
 params JSONB NOT NULL CHECK(jsonb_typeof(params)='object'),
 source JSONB,
 created_by BIGINT NOT NULL, updated_by BIGINT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE INDEX idx_quality_rules_tenant_updated ON quality.rules(tenant_id,updated_at DESC,id DESC);
CREATE TABLE quality.rule_revisions (
 tenant_id BIGINT NOT NULL,
 rule_id BIGINT NOT NULL,
 revision_no BIGINT NOT NULL CHECK(revision_no>0),
 name VARCHAR(200) NOT NULL, description TEXT NOT NULL DEFAULT '',
 type VARCHAR(40) NOT NULL,
 params JSONB NOT NULL CHECK(jsonb_typeof(params)='object'),
 source JSONB,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(), created_by BIGINT NOT NULL,
 PRIMARY KEY(tenant_id,rule_id,revision_no),
 FOREIGN KEY(tenant_id,rule_id) REFERENCES quality.rules(tenant_id,id)
);
CREATE TABLE quality.plan_check_items (
 tenant_id BIGINT NOT NULL, plan_id BIGINT NOT NULL,
 rule_key UUID NOT NULL, position INTEGER NOT NULL CHECK(position>=0),
 rule_id BIGINT NOT NULL, revision_no BIGINT NOT NULL,
 bindings JSONB NOT NULL CHECK(jsonb_typeof(bindings)='object'),
 severity VARCHAR(10) NOT NULL CHECK(severity IN ('error','warning','info')),
 disabled BOOLEAN NOT NULL DEFAULT false,
 PRIMARY KEY(tenant_id,plan_id,rule_key), UNIQUE(tenant_id,plan_id,position),
 FOREIGN KEY(tenant_id,plan_id) REFERENCES quality.plans(tenant_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,rule_id,revision_no) REFERENCES quality.rule_revisions(tenant_id,rule_id,revision_no)
);
CREATE INDEX idx_quality_plan_items_rule ON quality.plan_check_items(tenant_id,rule_id,plan_id);

DO $$ DECLARE p RECORD; r RECORD; rule_id BIGINT; constraint_params JSONB; target JSONB; rule_name TEXT;
BEGIN
 FOR p IN SELECT * FROM quality.plans ORDER BY id LOOP
  FOR r IN SELECT value AS doc, ordinality AS ordinal FROM jsonb_array_elements(p.rules->'rules') WITH ORDINALITY LOOP
   target := (r.doc->'params') - 'constraint' - 'values' - 'min' - 'max' - 'exact' - 'when' - 'then';
   constraint_params := (r.doc->'params') - 'table' - 'column' - 'columns' - 'reference_table' - 'reference_columns';
   IF r.doc->>'type'='predicate_implication' THEN
    target := target || jsonb_build_object('when_column',r.doc#>'{params,when,column}','then_column',r.doc#>'{params,then,column}');
    constraint_params := jsonb_build_object('when',(r.doc#>'{params,when}')-'column','then',(r.doc#>'{params,then}')-'column');
   END IF;
   rule_name := left(COALESCE(NULLIF(r.doc->>'name',''),p.name||' / '||(r.doc->>'type')||' #'||r.ordinal),200);
   INSERT INTO quality.rules(tenant_id,code,name,type,params,source,created_by,updated_by,created_at,updated_at)
   VALUES(p.tenant_id,'plan_'||p.id||'_rule_'||r.ordinal,rule_name,r.doc->>'type',constraint_params,r.doc->'source',p.created_by,p.updated_by,p.created_at,p.updated_at)
   RETURNING id INTO rule_id;
   INSERT INTO quality.rule_revisions(tenant_id,rule_id,revision_no,name,type,params,source,created_at,created_by)
   VALUES(p.tenant_id,rule_id,1,rule_name,r.doc->>'type',constraint_params,r.doc->'source',p.updated_at,p.updated_by);
   INSERT INTO quality.plan_check_items(tenant_id,plan_id,rule_key,position,rule_id,revision_no,bindings,severity,disabled)
   VALUES(p.tenant_id,p.id,(r.doc->>'rule_key')::uuid,r.ordinal-1,rule_id,1,target,r.doc->>'severity',COALESCE((r.doc->>'disabled')::boolean,false));
  END LOOP;
 END LOOP;
END $$;
ALTER TABLE quality.plans DROP COLUMN rules;
