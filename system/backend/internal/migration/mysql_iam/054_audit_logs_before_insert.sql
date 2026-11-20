CREATE TRIGGER IF NOT EXISTS trg_audit_logs_before_insert
BEFORE INSERT ON audit_logs FOR EACH ROW
BEGIN
    DECLARE contains_sensitive_key BOOLEAN DEFAULT FALSE;
    IF NEW.principal_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM principals WHERE id = NEW.principal_id AND principal_type = NEW.principal_type
        FOR SHARE
    ) THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit principal type must match the recorded principal';
    END IF;

    WITH RECURSIVE nodes(node_value) AS (
        SELECT NEW.details
        UNION ALL
        SELECT CASE JSON_TYPE(nodes.node_value)
            WHEN 'ARRAY' THEN JSON_EXTRACT(nodes.node_value, CONCAT('$[', child.position - 1, ']'))
            ELSE JSON_EXTRACT(nodes.node_value, CONCAT('$.', JSON_QUOTE(child.member_key)))
        END
          FROM nodes
          JOIN JSON_TABLE(
              CASE JSON_TYPE(nodes.node_value)
                  WHEN 'OBJECT' THEN JSON_KEYS(nodes.node_value)
                  WHEN 'ARRAY' THEN nodes.node_value
                  ELSE JSON_ARRAY()
              END,
              '$[*]' COLUMNS (
                  position FOR ORDINALITY,
                  member_key TEXT PATH '$' NULL ON EMPTY NULL ON ERROR
              )
          ) AS child
         WHERE JSON_TYPE(nodes.node_value) IN ('ARRAY', 'OBJECT')
    )
    SELECT EXISTS (
        SELECT 1 FROM nodes
        JOIN JSON_TABLE(JSON_KEYS(nodes.node_value), '$[*]' COLUMNS (member_key TEXT PATH '$')) AS member
        WHERE LOWER(REGEXP_REPLACE(member.member_key, '[^a-zA-Z0-9]', '', 1, 0, 'c')) IN (
            'password', 'passwordhash', 'mfasecret', 'clientsecret', 'accesstoken',
            'refreshtoken', 'delegatedaccesstoken', 'resourceaccessticket', 'authorizationcode',
            'devicecode', 'usercode', 'codeverifier', 'codechallenge', 'pkceverifier',
            'pkcechallenge', 'state', 'cookie', 'setcookie', 'authorization', 'authorizationheader',
            'requestbody', 'query', 'queryparams', 'rawquery', 'errordetail', 'errorstack'
        )
    ) INTO contains_sensitive_key;

    IF contains_sensitive_key THEN
        SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'audit details contain a prohibited field';
    END IF;
END
