CREATE TABLE IF NOT EXISTS ontologies (
    `tenant_id` bigint NOT NULL,
    `ontology_id` VARCHAR(64) NOT NULL,
    `last_revision` bigint NOT NULL,
    `activation_version` bigint DEFAULT 1 NOT NULL,
    `active_revision` bigint,
    `active_generation` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci,
    PRIMARY KEY (tenant_id, ontology_id),
    CHECK (active_generation IS NULL OR (IS_UUID(active_generation) AND LENGTH(active_generation)=36)),
    CONSTRAINT active_pair CHECK ((((active_revision IS NOT NULL) + (active_generation IS NOT NULL)) IN (0, 2))),
    CONSTRAINT ontologies_activation_version_check CHECK ((activation_version > 0)),
    CONSTRAINT ontologies_last_revision_check CHECK ((last_revision >= 0)),
    CONSTRAINT ontologies_ontology_id_check CHECK (((ontology_id) REGEXP '^[a-z][a-z0-9_]{0,63}$')),
    CONSTRAINT ontologies_tenant_id_check CHECK ((tenant_id > 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS platform_capabilities (
    `capability` VARCHAR(128) NOT NULL,
    `last_revision` bigint DEFAULT 0 NOT NULL,
    `publish_generation` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci,
    `active_revision` bigint,
    `active_generation` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci,
    `activation_version` bigint DEFAULT 0 NOT NULL,
    PRIMARY KEY (capability),
    CHECK (publish_generation IS NULL OR (IS_UUID(publish_generation) AND LENGTH(publish_generation)=36)),
    CHECK (active_generation IS NULL OR (IS_UUID(active_generation) AND LENGTH(active_generation)=36)),
    CONSTRAINT platform_active_pair CHECK ((((active_revision IS NULL) AND (active_generation IS NULL) AND (activation_version = 0)) OR ((active_revision IS NOT NULL) AND (active_generation IS NOT NULL) AND (activation_version > 0)))),
    CONSTRAINT platform_active_revision_order CHECK ((active_revision <= last_revision)),
    CONSTRAINT platform_capabilities_activation_version_check CHECK ((activation_version >= 0)),
    CONSTRAINT platform_capabilities_capability_check CHECK (((octet_length(capability) <= 128) AND (capability REGEXP '^[a-z][a-z0-9_]*([.][a-z][a-z0-9_]*)+$'))),
    CONSTRAINT platform_capabilities_last_revision_check CHECK ((last_revision >= 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS revisions (
    `tenant_id` bigint NOT NULL,
    `ontology_id` VARCHAR(64) NOT NULL,
    `revision` bigint NOT NULL,
    `version` bigint NOT NULL,
    `status` VARCHAR(16) NOT NULL,
    `payload` LONGTEXT NOT NULL,
    `digest` VARCHAR(64) NOT NULL,
    `build_execution_id` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci,
    `generation` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci,
    `published_at` DATETIME(6),
    `created_at` DATETIME(6) NOT NULL,
    `updated_at` DATETIME(6) NOT NULL,
    UNIQUE KEY revisions_build_execution_id_key (build_execution_id),
    UNIQUE KEY revisions_generation_key (generation),
    PRIMARY KEY (tenant_id, ontology_id, revision),
    working TINYINT GENERATED ALWAYS AS (IF(((status) IN ('draft', 'in_review')),1,NULL)) STORED,
    UNIQUE KEY one_working_revision (tenant_id, ontology_id, working),
    CONSTRAINT revisions_tenant_id_ontology_id_fkey FOREIGN KEY (tenant_id, ontology_id) REFERENCES ontologies(tenant_id, ontology_id),
    CHECK (build_execution_id IS NULL OR (IS_UUID(build_execution_id) AND LENGTH(build_execution_id)=36)),
    CHECK (generation IS NULL OR (IS_UUID(generation) AND LENGTH(generation)=36)),
    CONSTRAINT revisions_check CHECK (((((status) IN ('published', 'withdrawn')) AND (((build_execution_id IS NOT NULL) + (generation IS NOT NULL) + (published_at IS NOT NULL)) = 3)) OR (((status) IN ('draft', 'in_review')) AND (((build_execution_id IS NOT NULL) + (generation IS NOT NULL) + (published_at IS NOT NULL)) = 0)))),
    CONSTRAINT revisions_digest_check CHECK (((digest) REGEXP '^[0-9a-f]{64}$')),
    CONSTRAINT revisions_payload_check CHECK ((octet_length(payload) <= 2097152)),
    CONSTRAINT revisions_revision_check CHECK ((revision > 0)),
    CONSTRAINT revisions_status_check CHECK (((status) IN ('draft', 'in_review', 'published', 'withdrawn'))),
    CONSTRAINT revisions_version_check CHECK ((version > 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS platform_revisions (
    `capability` VARCHAR(128) NOT NULL,
    `revision` bigint NOT NULL,
    `payload` LONGTEXT NOT NULL,
    `digest` VARCHAR(64) NOT NULL,
    `created_at` DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6) NOT NULL,
    UNIQUE KEY platform_revisions_capability_revision_digest_key (capability, revision, digest),
    PRIMARY KEY (capability, revision),
    CONSTRAINT platform_revisions_capability_fkey FOREIGN KEY (capability) REFERENCES platform_capabilities(capability),
    CONSTRAINT platform_payload_digest CHECK (SHA2(payload,256)=digest),
    CONSTRAINT platform_revisions_digest_check CHECK ((digest REGEXP '^[0-9a-f]{64}$')),
    CONSTRAINT platform_revisions_payload_check CHECK ((octet_length(payload) <= 65536)),
    CONSTRAINT platform_revisions_revision_check CHECK ((revision > 0)),
    CONSTRAINT platform_payload_identity CHECK ((
        JSON_TYPE(payload)='OBJECT' AND
        JSON_TYPE(JSON_EXTRACT(payload,'$.definition.revision')) IN ('INTEGER','UNSIGNED INTEGER','DOUBLE','DECIMAL') AND
        JSON_TYPE(JSON_EXTRACT(payload,'$.review'))='OBJECT' AND
        JSON_CONTAINS_PATH(payload,'one','$.definition.tenant_id')=0 AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.contract'))='addp.platform-definition/v2' AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.compiler'))='addp.platform-compiler/v2' AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.definition.schema_version'))='addp.platform-capability-context/v1' AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.definition.knowledge_kind'))='platform_definition' AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.definition.capability'))=capability AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.definition.revision'))=CAST(revision AS CHAR) AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.definition.digest'))='' AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.definition.availability'))='not_observed' AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.review.method'))='curated' AND
        JSON_UNQUOTE(JSON_EXTRACT(payload,'$.review.scope'))='selected_capability') IS TRUE)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS projections (
    `generation` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci NOT NULL,
    `tenant_id` bigint NOT NULL,
    `ontology_id` VARCHAR(64) NOT NULL,
    `revision` bigint NOT NULL,
    `digest` VARCHAR(64) NOT NULL,
    `execution_id` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci NOT NULL,
    `baseline_version` bigint NOT NULL,
    `status` VARCHAR(16) NOT NULL,
    `created_at` DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6) NOT NULL,
    `updated_at` DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6) NOT NULL,
    `predecessor_generation` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci,
    UNIQUE KEY projections_execution_id_key (execution_id),
    PRIMARY KEY (generation),
    UNIQUE KEY projections_tenant_id_ontology_id_revision_generation_key (tenant_id, ontology_id, revision, generation),
    pending TINYINT GENERATED ALWAYS AS (IF(((status) IN ('pending', 'building')),1,NULL)) STORED,
    UNIQUE KEY one_pending_projection (tenant_id, ontology_id, revision, pending),
    UNIQUE KEY one_projection_successor (predecessor_generation),
    KEY projection_revision (tenant_id, ontology_id, revision),
    CONSTRAINT projection_predecessor FOREIGN KEY (tenant_id, ontology_id, revision, predecessor_generation) REFERENCES projections(tenant_id, ontology_id, revision, generation),
    CONSTRAINT projections_tenant_id_ontology_id_revision_fkey FOREIGN KEY (tenant_id, ontology_id, revision) REFERENCES revisions(tenant_id, ontology_id, revision),
    CHECK (generation IS NULL OR (IS_UUID(generation) AND LENGTH(generation)=36)),
    CHECK (execution_id IS NULL OR (IS_UUID(execution_id) AND LENGTH(execution_id)=36)),
    CHECK (predecessor_generation IS NULL OR (IS_UUID(predecessor_generation) AND LENGTH(predecessor_generation)=36)),
    CONSTRAINT projections_baseline_version_check CHECK ((baseline_version > 0)),
    CONSTRAINT projections_digest_check CHECK (((digest) REGEXP '^[0-9a-f]{64}$')),
    CONSTRAINT projections_status_check CHECK (((status) IN ('pending', 'building', 'ready', 'failed')))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS revision_events (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `tenant_id` bigint NOT NULL,
    `ontology_id` VARCHAR(64) NOT NULL,
    `revision` bigint NOT NULL,
    `version` bigint NOT NULL,
    `action` VARCHAR(16) NOT NULL,
    `from_state` VARCHAR(16) NOT NULL,
    `to_state` VARCHAR(16) NOT NULL,
    `digest` VARCHAR(64) NOT NULL,
    `actor_principal_id` bigint NOT NULL,
    `actor_membership_id` bigint NOT NULL,
    `authorization_version` bigint NOT NULL,
    `created_at` DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY revision_events_tenant_id_ontology_id_revision_version_key (tenant_id, ontology_id, revision, version),
    CONSTRAINT revision_events_tenant_id_ontology_id_revision_fkey FOREIGN KEY (tenant_id, ontology_id, revision) REFERENCES revisions(tenant_id, ontology_id, revision),
    CONSTRAINT revision_events_action_check CHECK (((action) IN ('create', 'save', 'submit', 'return', 'publish', 'withdraw'))),
    CONSTRAINT revision_events_actor_membership_id_check CHECK ((actor_membership_id > 0)),
    CONSTRAINT revision_events_actor_principal_id_check CHECK ((actor_principal_id > 0)),
    CONSTRAINT revision_events_authorization_version_check CHECK ((authorization_version > 0)),
    CONSTRAINT revision_events_digest_check CHECK (((digest) REGEXP '^[0-9a-f]{64}$')),
    CONSTRAINT revision_events_version_check CHECK ((version > 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS platform_projections (
    `generation` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci NOT NULL,
    `capability` VARCHAR(128) NOT NULL,
    `revision` bigint NOT NULL,
    `digest` VARCHAR(64) NOT NULL,
    `baseline_activation_version` bigint NOT NULL,
    `status` VARCHAR(16) NOT NULL,
    `created_at` DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6) NOT NULL,
    UNIQUE KEY platform_projections_capability_generation_key (capability, generation),
    UNIQUE KEY platform_projections_capability_revision_generation_key (capability, revision, generation),
    PRIMARY KEY (generation),
    CONSTRAINT platform_projections_capability_revision_digest_fkey FOREIGN KEY (capability, revision, digest) REFERENCES platform_revisions(capability, revision, digest),
    CHECK (generation IS NULL OR (IS_UUID(generation) AND LENGTH(generation)=36)),
    CONSTRAINT platform_projections_baseline_activation_version_check CHECK ((baseline_activation_version >= 0)),
    CONSTRAINT platform_projections_generation_check CHECK ((generation <> '00000000-0000-0000-0000-000000000000')),
    CONSTRAINT platform_projections_status_check CHECK ((status IN ('building', 'ready', 'failed', 'superseded')))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS platform_revision_events (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `capability` VARCHAR(128) NOT NULL,
    `revision` bigint NOT NULL,
    `action` VARCHAR(16) NOT NULL,
    `digest` VARCHAR(64) NOT NULL,
    `actor_principal_id` bigint NOT NULL,
    `actor_principal_type` VARCHAR(32) NOT NULL,
    `authorization_version` bigint NOT NULL,
    `created_at` DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6) NOT NULL,
    UNIQUE KEY platform_revision_events_capability_revision_action_key (capability, revision, action),
    PRIMARY KEY (id),
    CONSTRAINT platform_revision_events_capability_revision_digest_fkey FOREIGN KEY (capability, revision, digest) REFERENCES platform_revisions(capability, revision, digest),
    CONSTRAINT platform_revision_events_action_check CHECK ((action = 'record')),
    CONSTRAINT platform_revision_events_actor_principal_id_check CHECK ((actor_principal_id > 0)),
    CONSTRAINT platform_revision_events_actor_principal_type_check CHECK ((actor_principal_type IN ('user', 'service_principal'))),
    CONSTRAINT platform_revision_events_authorization_version_check CHECK ((authorization_version > 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS projection_events (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `generation` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci NOT NULL,
    `action` VARCHAR(32) NOT NULL,
    `attempt` integer NOT NULL,
    `actor_principal_id` bigint NOT NULL,
    `actor_membership_id` bigint NOT NULL,
    `authorization_version` bigint NOT NULL,
    `created_at` DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6) NOT NULL,
    UNIQUE KEY projection_events_generation_action_key (generation, action),
    PRIMARY KEY (id),
    CONSTRAINT projection_events_generation_fkey FOREIGN KEY (generation) REFERENCES projections(generation),
    CHECK (generation IS NULL OR (IS_UUID(generation) AND LENGTH(generation)=36)),
    CONSTRAINT projection_events_action_check CHECK (((action) IN ('rebuild_requested', 'building', 'activated', 'failed'))),
    CONSTRAINT projection_events_actor_membership_id_check CHECK ((actor_membership_id > 0)),
    CONSTRAINT projection_events_actor_principal_id_check CHECK ((actor_principal_id > 0)),
    CONSTRAINT projection_events_attempt_check CHECK ((attempt >= 0)),
    CONSTRAINT projection_events_authorization_version_check CHECK ((authorization_version > 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;

CREATE TABLE IF NOT EXISTS platform_projection_events (
    `id` bigint NOT NULL AUTO_INCREMENT,
    `generation` CHAR(36) CHARACTER SET ascii COLLATE ascii_general_ci NOT NULL,
    `action` VARCHAR(16) NOT NULL,
    `actor_principal_id` bigint NOT NULL,
    `authorization_version` bigint NOT NULL,
    `created_at` DATETIME(6) DEFAULT CURRENT_TIMESTAMP(6) NOT NULL,
    UNIQUE KEY platform_projection_events_generation_action_key (generation, action),
    PRIMARY KEY (id),
    CONSTRAINT platform_projection_events_generation_fkey FOREIGN KEY (generation) REFERENCES platform_projections(generation),
    CHECK (generation IS NULL OR (IS_UUID(generation) AND LENGTH(generation)=36)),
    CONSTRAINT platform_projection_events_action_check CHECK ((action IN ('begin', 'ready', 'failed', 'superseded'))),
    CONSTRAINT platform_projection_events_actor_principal_id_check CHECK ((actor_principal_id > 0)),
    CONSTRAINT platform_projection_events_authorization_version_check CHECK ((authorization_version > 0))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_bin;
