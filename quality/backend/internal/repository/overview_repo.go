package repository

import (
	"context"
	"database/sql"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
	"time"
)

type OverviewRepository struct{ db *gorm.DB }

func NewOverviewRepository(db *gorm.DB) *OverviewRepository { return &OverviewRepository{db: db} }

// Complete observations only: partial results, timeouts and infrastructure errors
// must never be presented as a new quality score.
const observationPredicate = `CASE WHEN jsonb_typeof(metadata->'rules')='array' THEN
 (status='success' OR (status='failed' AND error_details->>'code'='quality.plan.rule_failed'))
 AND metadata->>'schema_version'='addp.quality.plan-result/v1' AND jsonb_array_length(metadata->'rules')>0 ELSE false END`

const overviewScopeCTE = `WITH owned AS (
 SELECT id, name, version, owner_domain_id FROM quality.plans WHERE tenant_id=@tenant
 AND (@domain < 0 OR COALESCE(owner_domain_id,0)=@domain)
), runs AS (
 SELECT e.*, e.execution_config->>'target_key' AS target_key
 FROM common.task_executions e JOIN owned p ON e.source_task_id=p.id::text
 WHERE e.tenant_id=@tenant AND e.module='quality' AND e.task_type='quality_plan'
 AND e.execution_config->>'target_key' ~ '^[0-9a-f]{64}$'
), latest AS (
 SELECT DISTINCT ON (source_task_id,target_key) * FROM runs ORDER BY source_task_id,target_key,created_at DESC,id DESC
), observed AS (
 SELECT DISTINCT ON (source_task_id,target_key) * FROM runs WHERE ` + observationPredicate + `
 ORDER BY source_task_id,target_key,created_at DESC,id DESC
), scopes AS (
 SELECT p.id AS plan_id,p.name AS plan_name,p.version AS plan_version,p.owner_domain_id,
 l.target_key,l.execution_config->'table_bindings' AS table_bindings,l.execution_id,l.status,l.created_at,
 o.execution_id AS observed_execution_id,o.completed_at AS observed_at,
 (o.execution_config->>'task_version')::bigint AS observed_version,
 CASE WHEN o.id IS NOT NULL THEN (SELECT count(*) FROM jsonb_array_elements(o.metadata->'rules') r WHERE r->>'passed'='true') END AS passed_rules,
 CASE WHEN o.id IS NOT NULL THEN jsonb_array_length(o.metadata->'rules') END AS total_rules
 FROM owned p LEFT JOIN latest l ON l.source_task_id=p.id::text
 LEFT JOIN observed o ON o.source_task_id=l.source_task_id AND o.target_key=l.target_key
) `

func (r *OverviewRepository) Get(ctx context.Context, tenantID int64, domainID *int64, days, page, pageSize int) (*models.QualityOverview, error) {
	domain := int64(-1)
	if domainID != nil {
		domain = *domainID
	}
	page, pageSize = normalizePage(page, pageSize)
	result := &models.QualityOverview{Page: page, PageSize: pageSize, Data: []models.QualityScopeSummary{}, Trend: []models.QualityDailySummary{}}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var planCounts struct {
			PlanCount     int64
			NeverRunPlans int64
		}
		var issueCounts struct {
			OpenIssues     int64
			AcceptedIssues int64
			UnscopedIssues int64
		}
		args := map[string]interface{}{"tenant": tenantID, "domain": domain, "limit": pageSize, "offset": (page - 1) * pageSize}
		if err := tx.Raw(`SELECT count(*) AS plan_count, count(*) FILTER (WHERE NOT EXISTS (
		 SELECT 1 FROM common.task_executions e WHERE e.tenant_id=p.tenant_id AND e.module='quality' AND e.task_type='quality_plan' AND e.source_task_id=p.id::text
		)) AS never_run_plans FROM quality.plans p WHERE tenant_id=@tenant AND (@domain < 0 OR COALESCE(owner_domain_id,0)=@domain)`, args).Scan(&planCounts).Error; err != nil {
			return err
		}
		result.PlanCount, result.NeverRunPlans = planCounts.PlanCount, planCounts.NeverRunPlans
		if err := tx.Raw(`SELECT count(*) FILTER (WHERE status='open') AS open_issues, count(*) FILTER (WHERE status='accepted') AS accepted_issues, count(*) FILTER (WHERE target_key IS NULL) AS unscoped_issues
		 FROM quality.issues WHERE tenant_id=@tenant AND (@domain < 0 OR COALESCE(owner_domain_id,0)=@domain)`, args).Scan(&issueCounts).Error; err != nil {
			return err
		}
		result.OpenIssues, result.UnscopedIssues = issueCounts.OpenIssues, issueCounts.UnscopedIssues
		result.AcceptedIssues = issueCounts.AcceptedIssues
		if err := tx.Raw(overviewScopeCTE+`SELECT count(*) FROM scopes`, args).Scan(&result.Total).Error; err != nil {
			return err
		}
		if err := tx.Raw(overviewScopeCTE+`SELECT *,100.0*passed_rules/NULLIF(total_rules,0) AS pass_rate FROM scopes ORDER BY created_at DESC NULLS LAST,plan_id,target_key LIMIT @limit OFFSET @offset`, args).Scan(&result.Data).Error; err != nil {
			return err
		}
		args["since"] = time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -days+1)
		historyFilter := `tenant_id=@tenant AND module='quality' AND task_type='quality_plan' AND created_at>=@since
		 AND (@domain < 0 OR (jsonb_exists(execution_config, 'owner_domain_id') AND COALESCE((execution_config->>'owner_domain_id')::bigint,0)=@domain))`
		if err := tx.Raw(`SELECT count(*) FROM common.task_executions WHERE `+historyFilter+` AND COALESCE(execution_config->>'target_key','') !~ '^[0-9a-f]{64}$'`, args).Scan(&result.UnscopedExecutions).Error; err != nil {
			return err
		}
		return tx.Raw(`WITH daily_runs AS (
		 SELECT *, (`+observationPredicate+`) AS observed FROM common.task_executions WHERE `+historyFilter+`
		), daily AS (
		 SELECT to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD') AS day,count(*) AS executions,
		 count(*) FILTER (WHERE status IN ('failed','timeout') AND NOT COALESCE(observed,false)) AS runtime_errors,
		 COALESCE(sum(CASE WHEN observed THEN jsonb_array_length(metadata->'rules') ELSE 0 END),0) AS total_rules,
		 COALESCE(sum(CASE WHEN observed THEN (SELECT count(*) FROM jsonb_array_elements(metadata->'rules') r WHERE r->>'passed'='true') ELSE 0 END),0) AS passed_rules
		 FROM daily_runs GROUP BY 1
		) SELECT *,100.0*passed_rules/NULLIF(total_rules,0) AS pass_rate FROM daily ORDER BY day`, args).Scan(&result.Trend).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	result.TotalPages = int((result.Total + int64(pageSize) - 1) / int64(pageSize))
	return result, err
}
