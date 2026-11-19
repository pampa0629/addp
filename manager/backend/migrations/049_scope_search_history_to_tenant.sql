CREATE TABLE IF NOT EXISTS manager.search_histories (
    id bigserial PRIMARY KEY,
    tenant_id bigint,
    user_id bigint NOT NULL,
    query varchar(512) NOT NULL,
    created_at timestamptz,
    updated_at timestamptz
);

LOCK TABLE manager.search_histories IN ACCESS EXCLUSIVE MODE;

DELETE FROM manager.search_histories
WHERE tenant_id IS NULL OR tenant_id <= 0 OR user_id IS NULL OR user_id <= 0;

ALTER TABLE manager.search_histories
    ALTER COLUMN tenant_id SET NOT NULL,
    ALTER COLUMN user_id SET NOT NULL,
    ADD CONSTRAINT search_histories_positive_owner CHECK (tenant_id > 0 AND user_id > 0);

DROP INDEX IF EXISTS manager.idx_search_history_user_query;
DROP INDEX IF EXISTS manager.idx_search_histories_tenant_id;
CREATE UNIQUE INDEX idx_search_history_tenant_user_query
    ON manager.search_histories (tenant_id, user_id, query);
