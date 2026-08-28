-- Standard 数据元发布模型改为不可变修订后，旧规则应用无法证明所绑定的业务修订。
-- ADDP 当前不保留无修订身份的兼容记录；历史执行快照仍由 execution 自身保存。
TRUNCATE TABLE quality.rule_applications CASCADE;

ALTER TABLE quality.rule_applications
    ADD COLUMN element_revision_id BIGINT NOT NULL;

ALTER TABLE quality.rule_applications
    ADD CONSTRAINT ck_quality_rule_application_element_revision
    CHECK (element_revision_id > 0);

CREATE INDEX idx_quality_rule_applications_element_revision
    ON quality.rule_applications (tenant_id, element_revision_id);
