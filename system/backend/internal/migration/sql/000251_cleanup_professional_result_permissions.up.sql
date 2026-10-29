-- Explicitly approved Owner result permissions. No default role grants.
INSERT INTO system.permissions(permission_key,owner_module,action,risk_level,delegable,allowed_scope_types,tenant_customizable,name_i18n_key,description_i18n_key,status)
VALUES
 ('asset.cleanup_result.read','asset','read','medium',false,ARRAY['tenant']::text[],true,'permissions.asset.cleanup_result.read.name','permissions.asset.cleanup_result.read.description','active'),
 ('develop.cleanup_result.read','develop','read','medium',false,ARRAY['tenant']::text[],true,'permissions.develop.cleanup_result.read.name','permissions.develop.cleanup_result.read.description','active'),
 ('graph.cleanup_result.read','graph','read','medium',false,ARRAY['tenant']::text[],true,'permissions.graph.cleanup_result.read.name','permissions.graph.cleanup_result.read.description','active'),
 ('manager.cleanup_result.read','manager','read','medium',false,ARRAY['tenant']::text[],true,'permissions.manager.cleanup_result.read.name','permissions.manager.cleanup_result.read.description','active'),
 ('meta.cleanup_result.read','meta','read','medium',false,ARRAY['tenant']::text[],true,'permissions.meta.cleanup_result.read.name','permissions.meta.cleanup_result.read.description','active'),
 ('model.cleanup_result.read','model','read','medium',false,ARRAY['tenant']::text[],true,'permissions.model.cleanup_result.read.name','permissions.model.cleanup_result.read.description','active'),
 ('quality.cleanup_result.read','quality','read','medium',false,ARRAY['tenant']::text[],true,'permissions.quality.cleanup_result.read.name','permissions.quality.cleanup_result.read.description','active'),
 ('service.cleanup_result.read','service','read','medium',false,ARRAY['tenant']::text[],true,'permissions.service.cleanup_result.read.name','permissions.service.cleanup_result.read.description','active'),
 ('standard.cleanup_result.read','standard','read','medium',false,ARRAY['tenant']::text[],true,'permissions.standard.cleanup_result.read.name','permissions.standard.cleanup_result.read.description','active'),
 ('transfer.cleanup_result.read','transfer','read','medium',false,ARRAY['tenant']::text[],true,'permissions.transfer.cleanup_result.read.name','permissions.transfer.cleanup_result.read.description','active');
