CREATE TABLE IF NOT EXISTS meta.authorization_basis_generation (
    singleton BOOLEAN NOT NULL PRIMARY KEY CHECK (singleton=1),
    generation CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL
) ENGINE=InnoDB
