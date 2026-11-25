package repository

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/addp/common/config"
	"github.com/addp/quality/internal/models"
	"gorm.io/gorm"
	"time"
)

type OverviewRepository struct{ db *gorm.DB }

func NewOverviewRepository(db *gorm.DB) *OverviewRepository { return &OverviewRepository{db: db} }

// Complete observations only: partial results, timeouts and infrastructure errors
// must never be presented as a new quality score.
type overviewExpressions struct {
	planID, target, bindings, version, observation, ruleCount, passed, domainFilter, utcDay string
	invalidTarget                                                                           string
}

func qualityOverviewExpressions(mysql bool) overviewExpressions {
	if mysql {
		return overviewExpressions{
			planID: "CAST(p.id AS CHAR)", target: "e.execution_config->>'$.target_key'",
			bindings: "l.execution_config->'$.table_bindings'", version: "CAST(o.execution_config->>'$.task_version' AS SIGNED)",
			observation: `JSON_TYPE(metadata->'$.rules')='ARRAY' AND JSON_LENGTH(metadata->'$.rules')>0
                AND (status='success' OR (status='failed' AND error_details->>'$.code'='quality.plan.rule_failed'))
                AND metadata->>'$.schema_version'='addp.quality.plan-result/v1'`,
			ruleCount:     "JSON_LENGTH(%smetadata->'$.rules')",
			passed:        `(SELECT COUNT(*) FROM JSON_TABLE(COALESCE(%smetadata,JSON_OBJECT()), '$.rules[*]' COLUMNS (passed VARCHAR(8) PATH '$.passed' NULL ON ERROR)) AS rule_rows WHERE passed='true')`,
			domainFilter:  `JSON_CONTAINS_PATH(execution_config,'one','$.owner_domain_id') AND COALESCE(CAST(NULLIF(execution_config->>'$.owner_domain_id','null') AS SIGNED),0)=@domain`,
			utcDay:        "DATE_FORMAT(CONVERT_TZ(created_at,@zone,'+00:00'),'%Y-%m-%d')",
			invalidTarget: "NOT REGEXP_LIKE(COALESCE(e.execution_config->>'$.target_key',''),'^[0-9a-f]{64}$','c')",
		}
	}
	return overviewExpressions{
		planID: "CAST(p.id AS TEXT)", target: "e.execution_config->>'target_key'", bindings: "l.execution_config->'table_bindings'",
		version: "CAST(o.execution_config->>'task_version' AS BIGINT)",
		observation: `CASE WHEN jsonb_typeof(metadata->'rules')='array' THEN
            (status='success' OR (status='failed' AND error_details->>'code'='quality.plan.rule_failed'))
            AND metadata->>'schema_version'='addp.quality.plan-result/v1' AND jsonb_array_length(metadata->'rules')>0 ELSE false END`,
		ruleCount:     "jsonb_array_length(%smetadata->'rules')",
		passed:        `(SELECT COUNT(*) FROM jsonb_array_elements(%smetadata->'rules') r WHERE r->>'passed'='true')`,
		domainFilter:  `jsonb_exists(execution_config,'owner_domain_id') AND COALESCE((execution_config->>'owner_domain_id')::bigint,0)=@domain`,
		utcDay:        "to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD')",
		invalidTarget: "COALESCE(e.execution_config->>'target_key','') !~ '^[0-9a-f]{64}$'",
	}
}

func (e overviewExpressions) scopeQuery() string {
	return `WITH owned AS (
        SELECT id,name,version,owner_domain_id FROM quality.plans WHERE tenant_id=@tenant
        AND (@domain < 0 OR COALESCE(owner_domain_id,0)=@domain)
    ), runs AS (
        SELECT e.*, ` + e.target + ` AS target_key FROM common.task_executions e JOIN owned p ON e.source_task_id=` + e.planID + `
        WHERE e.tenant_id=@tenant AND e.module='quality' AND e.task_type='quality_plan' AND NOT (` + e.invalidTarget + `)
    ), latest AS (
        SELECT runs.*,ROW_NUMBER() OVER (PARTITION BY source_task_id,target_key ORDER BY created_at DESC,id DESC) AS seq FROM runs
    ), observed AS (
        SELECT runs.*,ROW_NUMBER() OVER (PARTITION BY source_task_id,target_key ORDER BY created_at DESC,id DESC) AS seq FROM runs WHERE ` + e.observation + `
    ), scopes AS (
        SELECT p.id AS plan_id,p.name AS plan_name,p.version AS plan_version,p.owner_domain_id,
        l.target_key,` + e.bindings + ` AS table_bindings,l.execution_id,l.status,l.created_at,
        o.execution_id AS observed_execution_id,o.completed_at AS observed_at,` + e.version + ` AS observed_version,
        CASE WHEN o.id IS NOT NULL THEN ` + fmt.Sprintf(e.passed, "o.") + ` END AS passed_rules,
        CASE WHEN o.id IS NOT NULL THEN ` + fmt.Sprintf(e.ruleCount, "o.") + ` END AS total_rules
        FROM owned p LEFT JOIN latest l ON l.source_task_id=` + e.planID + ` AND l.seq=1
        LEFT JOIN observed o ON o.source_task_id=l.source_task_id AND o.target_key=l.target_key AND o.seq=1
    ) `
}

func (r *OverviewRepository) Get(ctx context.Context, tenantID int64, domainID *int64, days, page, pageSize int) (*models.QualityOverview, error) {
	domain := int64(-1)
	if domainID != nil {
		domain = *domainID
	}
	page, pageSize = normalizePage(page, pageSize)
	result := &models.QualityOverview{Page: page, PageSize: pageSize, Data: []models.QualityScopeSummary{}, Trend: []models.QualityDailySummary{}}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		expr := qualityOverviewExpressions(tx.Dialector.Name() == "mysql")
		scopes := expr.scopeQuery()
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
		if err := tx.Raw(`SELECT count(*) AS plan_count, COUNT(CASE WHEN NOT EXISTS (
		 SELECT 1 FROM common.task_executions e WHERE e.tenant_id=p.tenant_id AND e.module='quality' AND e.task_type='quality_plan' AND e.source_task_id=`+expr.planID+`
        ) THEN 1 END) AS never_run_plans FROM quality.plans p WHERE tenant_id=@tenant AND (@domain < 0 OR COALESCE(owner_domain_id,0)=@domain)`, args).Scan(&planCounts).Error; err != nil {
			return err
		}
		result.PlanCount, result.NeverRunPlans = planCounts.PlanCount, planCounts.NeverRunPlans
		if err := tx.Raw(`SELECT COUNT(CASE WHEN status='open' THEN 1 END) AS open_issues, COUNT(CASE WHEN status='accepted' THEN 1 END) AS accepted_issues, COUNT(CASE WHEN target_key IS NULL THEN 1 END) AS unscoped_issues
		 FROM quality.issues WHERE tenant_id=@tenant AND (@domain < 0 OR COALESCE(owner_domain_id,0)=@domain)`, args).Scan(&issueCounts).Error; err != nil {
			return err
		}
		result.OpenIssues, result.UnscopedIssues = issueCounts.OpenIssues, issueCounts.UnscopedIssues
		result.AcceptedIssues = issueCounts.AcceptedIssues
		if err := tx.Raw(scopes+`SELECT count(*) FROM scopes`, args).Scan(&result.Total).Error; err != nil {
			return err
		}
		if err := tx.Raw(scopes+`SELECT *,100.0*passed_rules/NULLIF(total_rules,0) AS pass_rate FROM scopes ORDER BY CASE WHEN created_at IS NULL THEN 1 ELSE 0 END,created_at DESC,plan_id,target_key LIMIT @limit OFFSET @offset`, args).Scan(&result.Data).Error; err != nil {
			return err
		}
		args["since"] = time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -days+1)
		historyFilter := `tenant_id=@tenant AND module='quality' AND task_type='quality_plan' AND created_at>=@since
		 AND (@domain < 0 OR (` + expr.domainFilter + `))`
		if err := tx.Raw(`SELECT count(*) FROM common.task_executions e WHERE `+historyFilter+` AND `+expr.invalidTarget, args).Scan(&result.UnscopedExecutions).Error; err != nil {
			return err
		}
		args["zone"] = config.GetTimezone()
		return tx.Raw(`WITH daily_runs AS (
		 SELECT *, (`+expr.observation+`) AS observed FROM common.task_executions WHERE `+historyFilter+`
		), daily AS (
		 SELECT `+expr.utcDay+` AS day,count(*) AS executions,
		 COUNT(CASE WHEN status IN ('failed','timeout') AND NOT COALESCE(observed,false) THEN 1 END) AS runtime_errors,
		 COALESCE(sum(CASE WHEN observed THEN `+fmt.Sprintf(expr.ruleCount, "")+` ELSE 0 END),0) AS total_rules,
		 COALESCE(sum(CASE WHEN observed THEN `+fmt.Sprintf(expr.passed, "daily_runs.")+` ELSE 0 END),0) AS passed_rules
		 FROM daily_runs GROUP BY 1
		) SELECT *,100.0*passed_rules/NULLIF(total_rules,0) AS pass_rate FROM daily ORDER BY day`, args).Scan(&result.Trend).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	result.TotalPages = int((result.Total + int64(pageSize) - 1) / int64(pageSize))
	return result, err
}
