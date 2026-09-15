package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	modeli18n "github.com/addp/model/i18n"
	"github.com/addp/model/internal/apperrors"
	"github.com/addp/model/internal/models"
	"github.com/addp/model/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"strconv"
	"strings"
)

const materializationAuthorizationTTL = int64(3600)
const materializationMarkerPrefix = "addp:model-materialization:v1:"

var errMaterializedTargetOwnershipMismatch = errors.New("materialized target ownership marker mismatch")

type MaterializationService struct {
	systemClient        *commonClient.SystemServiceClient
	authorizationIssuer *commonClient.SystemExecutionAuthorizationClient
	logicalTableRepo    *repository.LogicalTableRepository
	logicalTableSvc     *LogicalTableService
}

func NewMaterializationService(client *commonClient.SystemServiceClient, repo *repository.LogicalTableRepository, svc *LogicalTableService) *MaterializationService {
	return &MaterializationService{systemClient: client, logicalTableRepo: repo, logicalTableSvc: svc}
}
func (s *MaterializationService) SetExecutionAuthorizationIssuer(issuer *commonClient.SystemExecutionAuthorizationClient) {
	s.authorizationIssuer = issuer
}

func (s *MaterializationService) DecommissionMaterializedTarget(
	ctx context.Context,
	logicalTableID, tenantID int64,
	request models.MaterializedTargetDecommissionRequest,
	userAccessToken string,
) error {
	if logicalTableID <= 0 || tenantID <= 0 || request.Version <= 0 || s.authorizationIssuer == nil || s.systemClient == nil {
		return apperrors.Validation("materialized_target_request_invalid", modeli18n.MsgMaterializationInvalid)
	}
	requestedLocator, err := resourcetree.ParseURI(strings.TrimSpace(request.TargetParentLocator))
	if err != nil || requestedLocator.EngineID == 0 || requestedLocator.Type != resourcetree.TypeSchema || len(requestedLocator.Path) == 0 ||
		!identifierPattern.MatchString(strings.TrimSpace(request.TargetName)) {
		return apperrors.Validation("materialized_target_confirmation_invalid", modeli18n.MsgMaterializationInvalid)
	}

	table, err := s.logicalTableRepo.GetByID(logicalTableID, tenantID)
	if err != nil {
		return materializationResourceError(err)
	}
	if err := requireVersion(table.Version, request.Version); err != nil {
		return err
	}
	if !materializedTargetConfirmationMatches(table, request) {
		return apperrors.Conflict("materialized_target_confirmation_mismatch", modeli18n.MsgMaterializedTargetConflict)
	}

	executionID := uuid.NewString()
	issued, err := s.authorizationIssuer.Issue(ctx, strings.TrimSpace(userAccessToken), commonClient.IssueExecutionAuthorizationRequest{
		Audience: commonExecution.AudienceModel, ExecutionID: executionID,
		Accesses: []commonClient.ExecutionEngineAccessScope{{
			EngineID: strconv.FormatUint(uint64(requestedLocator.EngineID), 10), Effects: []string{"read", "ddl"},
		}},
		ExpiresIn: materializationAuthorizationTTL,
	})
	if err != nil {
		return materializedTargetAuthorizationError(err)
	}

	return s.logicalTableRepo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateMaterializedTargetDecommissionState(tx, logicalTableID, tenantID, request); err != nil {
			return err
		}

		access, err := s.systemClient.WithTenantID(uint(tenantID)).GetExecutionEngineAccess(ctx, issued.ID, commonClient.ExecutionEngineAccessRequest{
			ExecutionID: executionID, EngineID: strconv.FormatUint(uint64(requestedLocator.EngineID), 10),
			RequiredEffects: []string{"read", "ddl"},
		})
		if err != nil {
			return materializedTargetAuthorizationError(err)
		}
		engineType := strings.ToLower(strings.TrimSpace(access.Engine.EngineType))
		if engineType != "postgres" && engineType != "postgresql" && engineType != "postgis" {
			return apperrors.Conflict("materialized_target_engine_unsupported", modeli18n.MsgMaterializedTargetConflict)
		}
		pool, err := materializationPool(access.Engine)
		if err != nil {
			return apperrors.Wrap(apperrors.KindUnavailable, "materialized_target_engine_unavailable", modeli18n.MsgMaterializedTargetUnavailable, err)
		}
		schemaName := requestedLocator.Path[len(requestedLocator.Path)-1]
		if err := pool.WithContext(ctx).Transaction(func(physicalTx *gorm.DB) error {
			return dropOwnedMaterializedTarget(physicalTx, schemaName, strings.TrimSpace(request.TargetName), logicalTableID)
		}); err != nil {
			if errors.Is(err, errMaterializedTargetOwnershipMismatch) {
				return apperrors.Conflict("materialized_target_ownership_mismatch", modeli18n.MsgMaterializedTargetConflict)
			}
			return apperrors.Wrap(apperrors.KindUnavailable, "materialized_target_drop_failed", modeli18n.MsgMaterializedTargetUnavailable, err)
		}
		return nil
	})
}

func validateMaterializedTargetDecommissionState(
	tx *gorm.DB,
	logicalTableID, tenantID int64,
	request models.MaterializedTargetDecommissionRequest,
) error {
	var locked models.LogicalTable
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND tenant_id = ?", logicalTableID, tenantID).First(&locked).Error; err != nil {
		return materializationResourceError(err)
	}
	if err := requireVersion(locked.Version, request.Version); err != nil {
		return err
	}
	if !materializedTargetConfirmationMatches(&locked, request) {
		return apperrors.Conflict("materialized_target_confirmation_mismatch", modeli18n.MsgMaterializedTargetConflict)
	}
	return nil
}

func materializedTargetConfirmationMatches(table *models.LogicalTable, request models.MaterializedTargetDecommissionRequest) bool {
	if table == nil {
		return false
	}
	locator, locatorOK := materializationString(table.Materialization, "target_parent_locator")
	targetName, nameOK := materializationString(table.Materialization, "target_name")
	return locatorOK && nameOK && locator == strings.TrimSpace(request.TargetParentLocator) &&
		targetName == strings.TrimSpace(request.TargetName)
}

func dropOwnedMaterializedTarget(tx *gorm.DB, schemaName, tableName string, logicalTableID int64) error {
	if err := lockMaterializedTarget(tx, schemaName, tableName); err != nil {
		return err
	}
	comment, exists, err := physicalTableComment(tx, schemaName, tableName)
	if err != nil || !exists {
		return err
	}
	if !materializationMarkerOwnedBy(comment, logicalTableID) {
		return errMaterializedTargetOwnershipMismatch
	}
	return tx.Exec("DROP TABLE " + qualifiedIdentifier(schemaName, tableName)).Error
}

func materializedTargetAuthorizationError(err error) error {
	if status, ok := commonClient.SystemAPIStatusCode(err); ok && (status == http.StatusUnauthorized || status == http.StatusForbidden) {
		return apperrors.Wrap(apperrors.KindForbidden, "materialized_target_engine_access_denied", modeli18n.MsgMaterializedTargetForbidden, err)
	}
	return apperrors.Wrap(apperrors.KindUnavailable, "materialized_target_authorization_unavailable", modeli18n.MsgMaterializedTargetUnavailable, err)
}

func (s *MaterializationService) loadApprovedDefinition(
	logicalTableID, tenantID int64,
) (*models.LogicalTable, []models.LogicalField, *resourcetree.ResourceLocator, string, string, error) {
	table, err := s.logicalTableRepo.GetByID(logicalTableID, tenantID)
	if err != nil {
		return nil, nil, nil, "", "", materializationResourceError(err)
	}
	if table.Status != "approved" {
		return nil, nil, nil, "", "", apperrors.Conflict("materialization_table_not_approved", modeli18n.MsgMaterializationConflict)
	}
	fields, err := s.logicalTableRepo.GetFields(logicalTableID)
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	if len(fields) == 0 || validateMaterialization(table, fields) != nil {
		return nil, nil, nil, "", "", apperrors.Validation("materialization_definition_invalid", modeli18n.MsgMaterializationInvalid)
	}
	locatorText, ok := materializationString(table.Materialization, "target_parent_locator")
	if !ok || locatorText == "" {
		return nil, nil, nil, "", "", apperrors.Validation("materialization_target_missing", modeli18n.MsgMaterializationInvalid)
	}
	targetName, ok := materializationString(table.Materialization, "target_name")
	if !ok || targetName == "" {
		return nil, nil, nil, "", "", apperrors.Validation("materialization_target_missing", modeli18n.MsgMaterializationInvalid)
	}
	locator, err := resourcetree.ParseURI(locatorText)
	if err != nil || locator.EngineID == 0 || locator.Type != resourcetree.TypeSchema || len(locator.Path) == 0 {
		return nil, nil, nil, "", "", apperrors.Validation("materialization_target_invalid", modeli18n.MsgMaterializationInvalid)
	}
	fingerprint, err := materializationSchemaFingerprint(table, fields)
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	return table, fields, locator, targetName, fingerprint, nil
}

func materializationSchemaFingerprint(table *models.LogicalTable, fields []models.LogicalField) (string, error) {
	type fieldShape struct {
		ColumnName   string `json:"column_name"`
		DataType     string `json:"data_type"`
		Length       *int   `json:"length,omitempty"`
		Nullable     bool   `json:"nullable"`
		PrimaryKey   bool   `json:"primary_key"`
		DefaultValue string `json:"default_value,omitempty"`
	}
	shape := struct {
		Fields        []fieldShape `json:"fields"`
		PartitionBy   string       `json:"partition_by,omitempty"`
		PartitionType string       `json:"partition_type,omitempty"`
	}{Fields: make([]fieldShape, 0, len(fields))}
	for _, field := range fields {
		shape.Fields = append(shape.Fields, fieldShape{
			ColumnName: field.ColumnName, DataType: field.DataType, Length: field.Length,
			Nullable: field.Nullable, PrimaryKey: field.IsPK, DefaultValue: field.DefaultValue,
		})
	}
	shape.PartitionBy, _ = materializationString(table.Materialization, "partition_by")
	shape.PartitionType, _ = materializationString(table.Materialization, "partition_type")
	encoded, err := json.Marshal(shape)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func normalizePostgreSQLType(value string) string {
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	value = strings.ReplaceAll(value, "varchar(", "character varying(")
	switch value {
	case "timestamp":
		return "timestamp without time zone"
	case "double precision":
		return value
	}
	return value
}

func materializationPool(engine *commonModels.Engine) (*gorm.DB, error) {
	if engine == nil {
		return nil, errors.New("materialization engine is missing")
	}
	return plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
		ID: engine.ID, EngineType: engine.EngineType, ConnectionInfo: plugin.ConnectionInfo(engine.ConnectionInfo),
	}, plugin.DefaultPoolConfig())
}

func physicalTableComment(tx *gorm.DB, schemaName, tableName string) (string, bool, error) {
	qualified := qualifiedIdentifier(schemaName, tableName)
	var relation sql.NullString
	if err := tx.Raw("SELECT to_regclass(?)::text", qualified).Scan(&relation).Error; err != nil {
		return "", false, err
	}
	if !relation.Valid || relation.String == "" {
		return "", false, nil
	}
	var comment sql.NullString
	if err := tx.Raw("SELECT obj_description(to_regclass(?), 'pg_class')", qualified).Scan(&comment).Error; err != nil {
		return "", false, err
	}
	return comment.String, true, nil
}

func materializationMarker(logicalTableID int64, fingerprint, operationID string) string {
	return materializationMarkerPrefix + strconv.FormatInt(logicalTableID, 10) + ":" + fingerprint + ":" + operationID
}

type materializationOwnershipMarker struct {
	LogicalTableID    int64
	SchemaFingerprint string
	OperationID       string
}

func parseMaterializationMarker(marker string) (materializationOwnershipMarker, bool) {
	if !strings.HasPrefix(marker, materializationMarkerPrefix) {
		return materializationOwnershipMarker{}, false
	}
	remainder := strings.TrimPrefix(marker, materializationMarkerPrefix)
	parts := strings.SplitN(remainder, ":", 3)
	if len(parts) != 3 || len(parts[1]) != 64 || strings.TrimSpace(parts[2]) == "" {
		return materializationOwnershipMarker{}, false
	}
	logicalTableID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || logicalTableID <= 0 {
		return materializationOwnershipMarker{}, false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return materializationOwnershipMarker{}, false
	}
	return materializationOwnershipMarker{
		LogicalTableID: logicalTableID, SchemaFingerprint: parts[1], OperationID: parts[2],
	}, true
}

func materializationMarkerOwnedBy(marker string, logicalTableID int64) bool {
	parsed, ok := parseMaterializationMarker(marker)
	return ok && parsed.LogicalTableID == logicalTableID
}

func qualifiedIdentifier(schemaName, tableName string) string {
	return quoteIdentifier(schemaName) + "." + quoteIdentifier(tableName)
}

func quoteSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func materializationResourceError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, commonAPI.ErrNotFound) {
		return apperrors.NotFound("materialization_resource_not_found", modeli18n.MsgMaterializationNotFound)
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) || errors.Is(err, commonAPI.ErrConflict) {
		return apperrors.Conflict("materialization_state_conflict", modeli18n.MsgMaterializationConflict)
	}
	return err
}

// CreateMaterializedTarget creates the approved structure without changing any existing records.
func (s *MaterializationService) createMaterializedTarget(ctx context.Context, id, tenantID, version int64, authorizationID, operationID string) (string, error) {
	if id <= 0 || tenantID <= 0 || version <= 0 || s.authorizationIssuer == nil || s.systemClient == nil {
		return "", apperrors.Validation("materialized_target_request_invalid", modeli18n.MsgMaterializationInvalid)
	}
	table, _, locator, name, _, err := s.loadApprovedDefinition(id, tenantID)
	if err != nil {
		return "", err
	}
	if err = requireVersion(table.Version, version); err != nil {
		return "", err
	}
	err = s.logicalTableRepo.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		locked, err := repository.LockLogicalTable(tx, id, tenantID)
		if err != nil {
			return materializationResourceError(err)
		}
		if err := requireVersion(locked.Version, version); err != nil {
			return err
		}
		if locked.Status != "approved" {
			return apperrors.Conflict("materialization_table_not_approved", modeli18n.MsgMaterializationConflict)
		}
		fields, err := repository.NewLogicalTableRepository(tx).GetFields(id)
		if err != nil {
			return err
		}
		fingerprint, err := materializationSchemaFingerprint(locked, fields)
		if err != nil {
			return err
		}
		access, err := s.systemClient.WithTenantID(uint(tenantID)).GetExecutionEngineAccess(ctx, authorizationID, commonClient.ExecutionEngineAccessRequest{ExecutionID: operationID, EngineID: strconv.FormatUint(uint64(locator.EngineID), 10), RequiredEffects: []string{"read", "ddl"}})
		if err != nil {
			return materializedTargetAuthorizationError(err)
		}
		if access.Engine == nil || (access.Engine.EngineType != "postgresql" && access.Engine.EngineType != "postgres" && access.Engine.EngineType != "postgis") {
			return apperrors.Conflict("materialized_target_engine_unsupported", modeli18n.MsgMaterializedTargetConflict)
		}
		pool, err := materializationPool(access.Engine)
		if err != nil {
			return err
		}
		return pool.WithContext(ctx).Transaction(func(physical *gorm.DB) error {
			return s.ensureMaterializedTable(physical, locked, fields, locator.Path[len(locator.Path)-1], name, fingerprint, operationID)
		})
	})
	if err != nil {
		return "", err
	}
	return (&resourcetree.ResourceLocator{EngineID: locator.EngineID, Path: append(append([]string{}, locator.Path...), name), Type: resourcetree.TypeTable}).ToURI(), nil
}

func (s *MaterializationService) ensureMaterializedTable(tx *gorm.DB, table *models.LogicalTable, fields []models.LogicalField, schema, name, fingerprint, operationID string) error {
	if len(fields) == 0 {
		return apperrors.Validation("materialization_definition_invalid", modeli18n.MsgMaterializationInvalid)
	}
	if partition, _ := materializationString(table.Materialization, "partition_by"); partition != "" {
		return apperrors.Validation("materialization_partition_unsupported", modeli18n.MsgMaterializationInvalid)
	}
	if err := lockMaterializedTarget(tx, schema, name); err != nil {
		return err
	}
	comment, exists, err := physicalTableComment(tx, schema, name)
	if err != nil {
		return err
	}
	if exists {
		marker, ok := parseMaterializationMarker(comment)
		if !ok || marker.LogicalTableID != table.ID {
			return apperrors.Conflict("materialized_target_ownership_mismatch", modeli18n.MsgMaterializedTargetConflict)
		}
		if marker.SchemaFingerprint != fingerprint {
			return apperrors.Conflict("materialized_target_schema_mismatch", modeli18n.MsgMaterializedTargetConflict)
		}
		return s.validateMaterializedColumns(tx, fields, schema, name)
	}
	if err := tx.Exec(s.logicalTableSvc.generatePostgreSQLDDL(table, fields)).Error; err != nil {
		return err
	}
	return tx.Exec("COMMENT ON TABLE " + qualifiedIdentifier(schema, name) + " IS " + quoteSQLLiteral(materializationMarker(table.ID, fingerprint, operationID))).Error
}
func lockMaterializedTarget(tx *gorm.DB, schema, name string) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", qualifiedIdentifier(schema, name)).Error
}
func (s *MaterializationService) validateMaterializedColumns(tx *gorm.DB, fields []models.LogicalField, schema, name string) error {
	var physical []struct {
		ColumnName   string
		DataType     string
		Nullable     bool
		IsPrimaryKey bool
	}
	if err := tx.Raw(`SELECT a.attname AS column_name, pg_catalog.format_type(a.atttypid,a.atttypmod) AS data_type, NOT a.attnotnull AS nullable,
 EXISTS(SELECT 1 FROM pg_catalog.pg_index i WHERE i.indrelid=c.oid AND i.indisprimary AND a.attnum=ANY(i.indkey)) AS is_primary_key
 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace JOIN pg_catalog.pg_attribute a ON a.attrelid=c.oid
 WHERE n.nspname=? AND c.relname=? AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped ORDER BY a.attnum`, schema, name).Scan(&physical).Error; err != nil {
		return err
	}
	if len(physical) != len(fields) {
		return apperrors.Conflict("materialized_target_schema_mismatch", modeli18n.MsgMaterializedTargetConflict)
	}
	for i, f := range fields {
		p := physical[i]
		if p.ColumnName != f.ColumnName || normalizePostgreSQLType(p.DataType) != normalizePostgreSQLType(s.logicalTableSvc.mapDataTypeToPostgreSQL(f.DataType, f.Length)) || p.Nullable != (f.Nullable && !f.IsPK) || p.IsPrimaryKey != f.IsPK {
			return apperrors.Conflict("materialized_target_schema_mismatch", modeli18n.MsgMaterializedTargetConflict)
		}
	}
	return nil
}
