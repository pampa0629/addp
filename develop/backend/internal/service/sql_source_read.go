package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/client"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
)

func (s *SQLEngineService) observeSQLSourceRead(ctx context.Context, tenantID uint, executionID uuid.UUID, engine *commonModels.Engine, authorization *IssuedSQLExecutionAuthorization, readSet *plugin.QueryReadSet, sampleCredential string) (*client.DevelopQueryReadObservation, error) {
	if s.systemService == nil || authorization == nil || engine == nil || readSet == nil {
		return nil, client.ErrDevelopQueryReadDenied
	}
	request := client.DevelopQueryReadRequest{AuthorizationID: strconv.FormatInt(authorization.AuthorizationID, 10), ExecutionID: executionID.String(),
		EngineID: strconv.FormatUint(uint64(engine.ID), 10), EngineVersion: strconv.FormatInt(engine.Version, 10), ReadSet: readSet.Clone(), SampleCredential: sampleCredential}
	if sampleCredential == "" {
		lease, ok := execution.LeaseFromContext(ctx)
		if !ok || lease.ExecutionID != executionID.String() || lease.TenantID != int(tenantID) {
			return nil, client.ErrDevelopQueryReadDenied
		}
		request.Attempt, request.LeaseToken = lease.Attempt, lease.Token
	}
	return s.systemService.WithTenantID(tenantID).CheckDevelopQueryRead(ctx, request)
}

// Both async execution and synchronous sample trial reads use this single
// source-authorized path. Unsupported proof never falls back to Execute.
func (s *SQLEngineService) executeSourceAuthorizedSQL(ctx context.Context, tenantID uint, executionID uuid.UUID, engine *commonModels.Engine, authorization *IssuedSQLExecutionAuthorization,
	enginePlugin plugin.EnginePlugin, prepared plugin.PreparedQuery, limit int, sampleCredential string, protect func(*plugin.QueryResult) error) (*plugin.QueryResult, error) {
	return executeGuardedSQL(ctx, enginePlugin, prepared, limit, protect, func(ctx context.Context, set *plugin.QueryReadSet) (*client.DevelopQueryReadObservation, error) {
		return s.observeSQLSourceRead(ctx, tenantID, executionID, engine, authorization, set, sampleCredential)
	})
}

type queryReadObserver func(context.Context, *plugin.QueryReadSet) (*client.DevelopQueryReadObservation, error)

func executeGuardedSQL(ctx context.Context, enginePlugin plugin.EnginePlugin, prepared plugin.PreparedQuery, limit int, protect func(*plugin.QueryResult) error, observe queryReadObserver) (*plugin.QueryResult, error) {
	provider, ok := enginePlugin.(plugin.GuardedQueryReadSessionProvider)
	if !ok || limit <= 0 || protect == nil {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	var observed *client.DevelopQueryReadObservation
	session, err := provider.OpenGuardedQueryReadSession(ctx, prepared, func(ctx context.Context, set *plugin.QueryReadSet) error {
		var err error
		observed, err = observe(ctx, set)
		if errors.Is(err, client.ErrDevelopQueryReadDenied) {
			return plugin.NewQueryError("develop.query.source_read_denied", err)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	defer session.Close(context.Background())
	if observed == nil {
		return nil, client.ErrDevelopQueryReadDenied
	}
	readSet, err := session.ReadSet(ctx)
	if err != nil || readSet == nil {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	readCtx, cancel := context.WithDeadline(ctx, observed.ExpiresAt)
	defer cancel()
	batch, err := session.ReadBatch(readCtx, limit)
	if err != nil {
		return nil, err
	}
	if batch == nil {
		return nil, plugin.ErrQueryReadSetUnresolved
	}
	if err := session.Close(ctx); err != nil {
		return nil, err
	}
	result := &plugin.QueryResult{Fields: batch.Fields, Rows: batch.Rows, Columns: make([]string, len(batch.Fields))}
	for i, field := range batch.Fields {
		result.Columns[i] = field.Name
	}
	if err := protect(result); err != nil {
		return nil, err
	}
	if _, err := observe(ctx, readSet); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *SQLEngineService) generateGuardedExecutableSampleQuery(ctx context.Context, tenantID uint, userCredential string, executionID uuid.UUID, engine *commonModels.Engine,
	enginePlugin plugin.EnginePlugin, authorization *IssuedSQLExecutionAuthorization, selectedPath *plugin.EngineCatalogPath) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	provider, ok := enginePlugin.(plugin.GuardedQueryReadSessionProvider)
	if !ok {
		return "", "", plugin.ErrQueryReadSetUnresolved
	}
	if selectedPath == nil {
		catalog, hasCatalog := enginePlugin.(plugin.EngineCatalogProvider)
		model, hasModel := enginePlugin.(plugin.EngineCatalogModelProvider)
		if !hasCatalog || !hasModel {
			return "", "", ErrSampleQueryUnavailable
		}
		namespaces, err := catalog.ListChildren(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.EngineCatalogRootPath(model.EngineCatalogModel(), engine.ID), plugin.ListOptions{})
		if err != nil {
			return "", "", fmt.Errorf("%w: %v", ErrSampleQueryUnavailable, err)
		}
		for _, namespace := range namespaces {
			if namespace.Role != plugin.EngineCatalogRoleBranch {
				continue
			}
			items, err := catalog.ListChildren(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), namespace.Path, plugin.ListOptions{})
			if err != nil {
				return "", "", fmt.Errorf("%w: %v", ErrSampleQueryUnavailable, err)
			}
			for _, item := range items {
				if item.Role != plugin.EngineCatalogRoleLeaf {
					continue
				}
				// Catalog names are candidates only. Never Count rows before the
				// same source-authorized bounded trial used for an explicit leaf.
				query, language, err := s.generateGuardedExecutableSampleQuery(ctx, tenantID, userCredential, executionID, engine, enginePlugin, authorization, &item.Path)
				if err == nil {
					return query, language, nil
				}
				if !errors.Is(err, client.ErrDevelopQueryReadDenied) && !errors.Is(err, plugin.ErrQueryReadSetUnresolved) && !errors.Is(err, ErrSampleQueryResourceEmpty) {
					return "", "", err
				}
				if err := ctx.Err(); err != nil {
					return "", "", err
				}
			}
		}
		return "", "", ErrSampleQueryUnavailable
	}
	opts := plugin.SampleQueryOptions{}
	if selectedPath != nil {
		opts.Path = *selectedPath
	}
	query, language := provider.GenerateSampleQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), opts)
	query, language = strings.TrimSpace(query), strings.ToLower(strings.TrimSpace(language))
	if query == "" || language != "sql" {
		return "", "", ErrSampleQueryUnavailable
	}
	prepared, err := provider.PrepareQuery(ctx, plugin.ConnectionInfo(engine.ConnectionInfo), plugin.QueryRequest{EngineID: engine.ID, Language: language, Query: query, TargetPath: selectedPath,
		Options: plugin.QueryOptions{EngineID: engine.ID, EngineType: engine.EngineType, ReadOnly: true}})
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrSampleQueryUnavailable, err)
	}
	protect, end, err := s.protectionGate.BeginPreparedQuery(ctx, tenantID, enginePlugin, prepared)
	if err != nil {
		return "", "", err
	}
	defer end()
	result, err := s.executeSourceAuthorizedSQL(ctx, tenantID, executionID, engine, authorization, enginePlugin, prepared, 10, userCredential, protect)
	if err != nil {
		return "", "", err
	}
	if len(result.Rows) == 0 {
		return "", "", ErrSampleQueryResourceEmpty
	}
	return query, language, nil
}
