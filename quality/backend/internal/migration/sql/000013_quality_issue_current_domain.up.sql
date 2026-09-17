-- Current issues follow the owning plan, not the last execution's frozen owner.
UPDATE quality.issues AS issue
SET owner_domain_id = plan.owner_domain_id
FROM quality.plans AS plan
WHERE issue.tenant_id = plan.tenant_id
  AND issue.plan_id = plan.id
  AND issue.owner_domain_id IS DISTINCT FROM plan.owner_domain_id;
