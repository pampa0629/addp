CREATE TABLE IF NOT EXISTS execution_authorization_engine_accesses (
    authorization_id BIGINT NOT NULL,
    engine_id BIGINT NOT NULL,
    effects JSON NOT NULL,
    PRIMARY KEY (authorization_id, engine_id),
    KEY idx_execution_authorization_engine_accesses_engine (engine_id, authorization_id),
    FOREIGN KEY (authorization_id) REFERENCES execution_authorizations (id),
    CHECK (engine_id>0),
    CHECK (JSON_TYPE(effects)='ARRAY' AND JSON_LENGTH(effects) BETWEEN 1 AND 4
        AND JSON_CONTAINS(JSON_ARRAY('read','write','ddl','external_effect'), effects))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin
