CREATE TABLE IF NOT EXISTS engine_access_target_locks (
    target_key BINARY(32) PRIMARY KEY,
    lock_marker TINYINT NOT NULL DEFAULT 0 CHECK (lock_marker = 0)
) ENGINE=InnoDB;
