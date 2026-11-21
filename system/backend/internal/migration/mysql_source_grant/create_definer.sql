-- Run once as the deployment DBA on a fresh instance. A name collision must
-- stop installation rather than reuse or modify an existing identity.
CREATE USER 'addp_system_source_grant'@'localhost' ACCOUNT LOCK;
