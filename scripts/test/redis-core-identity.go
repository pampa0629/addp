// Copied only into the pinned identity checkout, never the product checkout.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	consumerRepository "github.com/addp/system/internal/repository"
	consumerService "github.com/addp/system/internal/service"
	"gorm.io/gorm"
)

const redisCoreSuite = "infra-redis-core"

var redisCorePermissions = []string{"service.definition.create", "service.definition.read", "service.definition.update", "service.definition.delete", "service.data_read.execute", "meta.catalog.read", "security.enrollment.create", "security.enrollment.read"}

func prepareRedisCoreIdentity(ctx context.Context, db *gorm.DB, tenantID, actorID int64, values map[string]string) error {
	credential, err := iam.NewTenantServiceAccountService(iam.NewRepository(db)).Create(ctx, iam.CreateTenantServiceAccountInput{
		TenantID: tenantID, ActorPrincipalID: actorID, Name: "redis-core-oauth-probe",
		Description: "Disposable zero-permission R1 authentication probe", Audit: audit("redis-core-service-account"),
	})
	if err != nil {
		return err
	}
	values["ADDP_REDIS_HTTP_CLIENT_ID"] = credential.Account.ClientID
	values["ADDP_REDIS_HTTP_CLIENT_SECRET"] = credential.ClientSecret
	values["ADDP_REDIS_HTTP_ACCOUNT_ID"] = fmt.Sprint(credential.Account.ID)
	values["ADDP_REDIS_HTTP_MEMBERSHIP_ID"] = fmt.Sprint(credential.Account.MembershipID)
	values["ADDP_REDIS_HTTP_PREPARER_ID"] = fmt.Sprint(actorID)
	return nil
}

// This dedicated owner helper has no HTTP route and is not compiled into any
// product server. Only its previously written private identity file is accepted.
func redisCoreConsumerCommand(ctx context.Context, db *gorm.DB, mode, privatePath string, queryID uint) error {
	info, err := os.Stat(privatePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("owner-only Redis identity file required")
	}
	raw, err := os.ReadFile(privatePath)
	if err != nil {
		return err
	}
	var values map[string]string
	// The wrapper converts the shell identity environment to this private JSON.
	if err := json.Unmarshal(raw, &values); err != nil {
		return errors.New("invalid private Redis identity")
	}
	tenantID, err := strconv.ParseUint(values["ADDP_ONLINE_TEST_TENANT_ID"], 10, 32)
	if err != nil || tenantID <= 1 {
		return errors.New("nondefault fixture tenant required")
	}
	actorID, err := strconv.ParseUint(values["ADDP_REDIS_HTTP_PREPARER_ID"], 10, 32)
	if err != nil || actorID == 0 {
		return errors.New("fixture actor required")
	}
	var valid bool
	if err := db.Raw(`SELECT EXISTS(SELECT 1 FROM system.tenant_memberships m JOIN system.principals p ON p.id=m.principal_id
      WHERE m.tenant_id=? AND m.principal_id=? AND m.status='active' AND p.principal_type='user' AND p.status='active')`, tenantID, actorID).Scan(&valid).Error; err != nil || !valid {
		return errors.New("fixture actor binding differs")
	}
	owner := consumerService.NewAPIConsumerService(consumerRepository.NewAPIConsumerRepository(db))
	switch mode {
	case "prepare":
		if queryID == 0 || values["ADDP_REDIS_HTTP_CONSUMER_ID"] != "" {
			return errors.New("fresh exact Query binding required")
		}
		consumer, err := owner.Create(&models.CreateAPIConsumerRequest{Name: "redis-core-" + values["ADDP_ONLINE_TEST_RUN_ID"],
			RateLimitPerMinute: 2, ServiceGrants: []models.APIConsumerServiceReference{{ServiceType: "query", ServiceID: queryID}}}, uint(tenantID), uint(actorID))
		if err != nil {
			return err
		}
		// Persist the cleanup binding before creating the credential.
		values["ADDP_REDIS_HTTP_CONSUMER_ID"] = fmt.Sprint(consumer.ID)
		values["ADDP_REDIS_HTTP_QUERY_ID"] = fmt.Sprint(queryID)
		if err := writeRedisPrivateJSON(privatePath, values); err != nil {
			return err
		}
		credential, err := owner.CreateCredential(consumer.ID, uint(tenantID), uint(actorID), &models.CreateAPIConsumerCredentialRequest{Name: "redis-core"})
		if err != nil {
			return err
		}
		values["ADDP_REDIS_HTTP_CONSUMER_KEY"] = credential.PlainTextCredential
	case "retire":
		id, err := strconv.ParseInt(values["ADDP_REDIS_HTTP_ACCOUNT_ID"], 10, 64)
		if err != nil || id <= 0 {
			return errors.New("owned OAuth account required")
		}
		service := iam.NewTenantServiceAccountService(iam.NewRepository(db))
		account, err := service.Get(ctx, int64(tenantID), id)
		if err != nil {
			return err
		}
		if account.ClientID != values["ADDP_REDIS_HTTP_CLIENT_ID"] || account.Name != "redis-core-oauth-probe" {
			return errors.New("OAuth account ownership differs")
		}
		if _, err := service.Suspend(ctx, iam.ChangeTenantServiceAccountStatusInput{TenantID: int64(tenantID), AccountID: id, Version: account.Version, ActorPrincipalID: int64(actorID), Reason: "Redis R1 fixture cleanup", Audit: audit("redis-core-retire")}); err != nil {
			return err
		}
		values["ADDP_REDIS_HTTP_ACCOUNT_RETIRED"] = "true"
	case "cleanup":
		id, err := strconv.ParseUint(values["ADDP_REDIS_HTTP_CONSUMER_ID"], 10, 32)
		if err != nil || id == 0 {
			return errors.New("exact owned Consumer required")
		}
		consumer, err := owner.Get(uint(id), uint(tenantID))
		if err != nil {
			return err
		}
		if !strings.HasPrefix(consumer.Name, "redis-core-") || consumer.Name != "redis-core-"+values["ADDP_ONLINE_TEST_RUN_ID"] || consumer.CreatedByPrincipalID != uint(actorID) {
			return errors.New("Consumer ownership differs")
		}
		queryID, err := strconv.ParseUint(values["ADDP_REDIS_HTTP_QUERY_ID"], 10, 32)
		if err != nil || queryID == 0 || len(consumer.ServiceGrants) != 1 || consumer.ServiceGrants[0].ServiceType != "query" || consumer.ServiceGrants[0].ServiceID != uint(queryID) {
			return errors.New("Consumer Query binding differs")
		}
		if err := owner.Delete(uint(id), uint(tenantID)); err != nil {
			return err
		}
		values["ADDP_REDIS_HTTP_CONSUMER_CLEANED"] = "true"
		delete(values, "ADDP_REDIS_HTTP_CONSUMER_KEY")
	default:
		return errors.New("unsupported Consumer fixture operation")
	}
	return writeRedisPrivateJSON(privatePath, values)
}

func writeRedisPrivateJSON(path string, values map[string]string) error {
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0600)
}
