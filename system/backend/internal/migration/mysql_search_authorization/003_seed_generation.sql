INSERT INTO search_authorization_generation(singleton,generation) VALUES (TRUE,UUID())
ON DUPLICATE KEY UPDATE singleton=search_authorization_generation.singleton
