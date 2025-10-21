package vectorstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/addp/common/config"
	"github.com/addp/common/embedding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"
)

var (
	ErrInvalidVector = errors.New("vectorstore: invalid vector")
	ErrNotFound      = errors.New("vectorstore: record not found")
)

type Record struct {
	ID         string
	ObjectID   string
	AssetID    string
	TenantID   *uint
	ResourceID *uint
	Modality   embedding.Modality
	Model      string
	Vector     []float32
	Metadata   map[string]any
	Dimension  int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type QueryOptions struct {
	Modality    embedding.Modality
	Modalities  []embedding.Modality
	Model       string
	TenantID    *uint
	ResourceID  *uint
	TopK        int
	MaxDistance float64
	IncludeVec  bool
	IncludeMeta bool
}

type SearchResult struct {
	Record   Record
	Distance float64
}

type PgVectorStore struct {
	cfg  config.VectorDBConfig
	pool *pgxpool.Pool
}

func NewPgVectorStore(ctx context.Context, cfg config.VectorDBConfig) (*PgVectorStore, error) {
	if cfg.Host == "" {
		return nil, errors.New("vectorstore: host missing")
	}
	if cfg.Port == "" {
		cfg.Port = "5432"
	}
	if cfg.Name == "" {
		return nil, errors.New("vectorstore: database name missing")
	}
	if cfg.Schema == "" {
		cfg.Schema = "vector_store"
	}
	if cfg.Table == "" {
		cfg.Table = "document_embeddings"
	}
	if cfg.Dimension <= 0 {
		return nil, fmt.Errorf("vectorstore: dimension must be positive (got %d)", cfg.Dimension)
	}

	dsn := buildDSN(cfg)
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("vectorstore: parse config: %w", err)
	}
	store := &PgVectorStore{
		cfg: cfg,
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("vectorstore: create pool: %w", err)
	}
	store.pool = pool

	if err := store.initSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return store, nil
}

func (s *PgVectorStore) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

func (s *PgVectorStore) initSchema(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("vectorstore: acquire connection: %w", err)
	}
	defer conn.Release()

	schemaIdent := pgx.Identifier{s.cfg.Schema}
	if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaIdent.Sanitize())); err != nil {
		return fmt.Errorf("vectorstore: create schema: %w", err)
	}

	if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("vectorstore: ensure extension: %w", err)
	}

	tableIdent := pgx.Identifier{s.cfg.Schema, s.cfg.Table}
	createTableSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    id TEXT PRIMARY KEY,
    object_id TEXT NOT NULL,
    asset_id TEXT,
    tenant_id BIGINT,
    resource_id BIGINT,
    modality TEXT NOT NULL,
    model TEXT NOT NULL,
    embedding vector(%d) NOT NULL,
    dimension INT NOT NULL,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (object_id, modality, model)
)`, tableIdent.Sanitize(), s.cfg.Dimension)

	if _, err := conn.Exec(ctx, createTableSQL); err != nil {
		return fmt.Errorf("vectorstore: create table: %w", err)
	}

	uniqueIdxName := sanitizeIdent(fmt.Sprintf("%s_%s_object_model_uq", s.cfg.Schema, s.cfg.Table))
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (object_id, modality, model)`, uniqueIdxName, tableIdent.Sanitize())); err != nil {
		return fmt.Errorf("vectorstore: ensure unique index: %w", err)
	}

	tenantIdxName := sanitizeIdent(fmt.Sprintf("%s_%s_tenant_idx", s.cfg.Schema, s.cfg.Table))
	if _, err := conn.Exec(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (tenant_id)`, tenantIdxName, tableIdent.Sanitize())); err != nil {
		return fmt.Errorf("vectorstore: ensure tenant index: %w", err)
	}

	return nil
}

func (s *PgVectorStore) Upsert(ctx context.Context, record Record) (*Record, error) {
	if len(record.Vector) == 0 {
		return nil, ErrInvalidVector
	}

	padded, err := s.prepareVector(record.Vector)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if record.ID == "" {
		record.ID = uuid.NewString()
	}
	if record.Dimension == 0 {
		record.Dimension = len(record.Vector)
	}
	record.CreatedAt = now
	record.UpdatedAt = now

	metadataJSON, err := json.Marshal(record.Metadata)
	if err != nil {
		return nil, fmt.Errorf("vectorstore: marshal metadata: %w", err)
	}

	tableIdent := pgx.Identifier{s.cfg.Schema, s.cfg.Table}
	sql := fmt.Sprintf(`INSERT INTO %s (id, object_id, asset_id, tenant_id, resource_id, modality, model, embedding, dimension, metadata, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb, $11, $12)
ON CONFLICT (object_id, modality, model) DO UPDATE
SET embedding = EXCLUDED.embedding,
    dimension = EXCLUDED.dimension,
    metadata = EXCLUDED.metadata,
    asset_id = EXCLUDED.asset_id,
    tenant_id = EXCLUDED.tenant_id,
    resource_id = EXCLUDED.resource_id,
    updated_at = EXCLUDED.updated_at
RETURNING id, created_at, updated_at`, tableIdent.Sanitize())

	vectorParam := pgvector.NewVector(padded)

	row := s.pool.QueryRow(ctx, sql,
		record.ID,
		record.ObjectID,
		nullString(record.AssetID),
		toNullableInt(record.TenantID),
		toNullableInt(record.ResourceID),
		string(record.Modality),
		record.Model,
		vectorParam,
		record.Dimension,
		string(metadataJSON),
		record.CreatedAt,
		record.UpdatedAt,
	)

	if err := row.Scan(&record.ID, &record.CreatedAt, &record.UpdatedAt); err != nil {
		return nil, fmt.Errorf("vectorstore: upsert scan: %w", err)
	}

	return &record, nil
}

func (s *PgVectorStore) QuerySimilar(ctx context.Context, vector []float32, opts QueryOptions) ([]SearchResult, error) {
	if len(vector) == 0 {
		return nil, ErrInvalidVector
	}

	if opts.Model == "" {
		return nil, errors.New("vectorstore: model required")
	}
	if opts.TopK <= 0 {
		opts.TopK = 10
	}

	padded, err := s.prepareVector(vector)
	if err != nil {
		return nil, err
	}

	tableIdent := pgx.Identifier{s.cfg.Schema, s.cfg.Table}
	args := []any{pgvector.NewVector(padded)}
	whereClauses := make([]string, 0, 4)
	argIndex := 2

	modalitySet := make(map[string]struct{})
	if trimmed := strings.TrimSpace(string(opts.Modality)); trimmed != "" {
		modalitySet[trimmed] = struct{}{}
	}
	for _, m := range opts.Modalities {
		if trimmed := strings.TrimSpace(string(m)); trimmed != "" {
			modalitySet[trimmed] = struct{}{}
		}
	}

	if len(modalitySet) == 1 {
		for m := range modalitySet {
			whereClauses = append(whereClauses, fmt.Sprintf("modality = $%d", argIndex))
			args = append(args, m)
			argIndex++
		}
	} else if len(modalitySet) > 1 {
		placeholders := make([]string, 0, len(modalitySet))
		for m := range modalitySet {
			placeholders = append(placeholders, fmt.Sprintf("$%d", argIndex))
			args = append(args, m)
			argIndex++
		}
		whereClauses = append(whereClauses, fmt.Sprintf("modality IN (%s)", strings.Join(placeholders, ",")))
	}

	whereClauses = append(whereClauses, fmt.Sprintf("model = $%d", argIndex))
	args = append(args, opts.Model)
	argIndex++

	if opts.TenantID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("tenant_id = $%d", argIndex))
		args = append(args, *opts.TenantID)
		argIndex++
	}
	if opts.ResourceID != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("resource_id = $%d", argIndex))
		args = append(args, *opts.ResourceID)
		argIndex++
	}

	query := fmt.Sprintf(`SELECT id, object_id, asset_id, tenant_id, resource_id, modality, model, metadata, dimension,
       embedding <=> $1::vector AS distance,
       created_at, updated_at
FROM %s
WHERE %s
ORDER BY embedding <=> $1::vector
LIMIT $%d`, tableIdent.Sanitize(), strings.Join(whereClauses, " AND "), argIndex)

	args = append(args, opts.TopK)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("vectorstore: query similar: %w", err)
	}
	defer rows.Close()

	results := make([]SearchResult, 0)
	for rows.Next() {
		var (
			id         string
			objectID   string
			assetID    *string
			tenantID   *int64
			resourceID *int64
			modality   string
			model      string
			metadata   []byte
			dimension  int
			distance   float64
			createdAt  time.Time
			updatedAt  time.Time
		)

		if err := rows.Scan(&id, &objectID, &assetID, &tenantID, &resourceID, &modality, &model, &metadata, &dimension, &distance, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("vectorstore: query scan: %w", err)
		}

		if opts.MaxDistance > 0 && distance > opts.MaxDistance {
			continue
		}

		rec := Record{
			ID:        id,
			ObjectID:  objectID,
			AssetID:   derefString(assetID),
			Modality:  embedding.Modality(modality),
			Model:     model,
			Dimension: dimension,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}
		if tenantID != nil {
			val := uint(*tenantID)
			rec.TenantID = &val
		}
		if resourceID != nil {
			val := uint(*resourceID)
			rec.ResourceID = &val
		}
		if opts.IncludeMeta && len(metadata) > 0 {
			var meta map[string]any
			if err := json.Unmarshal(metadata, &meta); err == nil {
				rec.Metadata = meta
			}
		}

		results = append(results, SearchResult{
			Record:   rec,
			Distance: distance,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("vectorstore: iterate rows: %w", err)
	}

	return results, nil
}

func (s *PgVectorStore) prepareVector(vec []float32) ([]float32, error) {
	if len(vec) == 0 {
		return nil, ErrInvalidVector
	}
	if s.cfg.Dimension <= 0 {
		return vec, nil
	}
	if len(vec) > s.cfg.Dimension {
		return nil, fmt.Errorf("vectorstore: vector dimension %d exceeds configured %d", len(vec), s.cfg.Dimension)
	}
	if len(vec) == s.cfg.Dimension {
		return vec, nil
	}
	padded := make([]float32, s.cfg.Dimension)
	copy(padded, vec)
	return padded, nil
}

func buildDSN(cfg config.VectorDBConfig) string {
	u := &url.URL{
		Scheme: "postgres",
		Host:   fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
		Path:   cfg.Name,
	}
	if cfg.User != "" {
		if cfg.Password != "" {
			u.User = url.UserPassword(cfg.User, cfg.Password)
		} else {
			u.User = url.User(cfg.User)
		}
	}

	query := url.Values{}
	if cfg.SSLMode != "" {
		query.Set("sslmode", cfg.SSLMode)
	}
	u.RawQuery = query.Encode()
	return u.String()
}

func sanitizeIdent(name string) string {
	return strings.ReplaceAll(name, "-", "_")
}

func nullString(val string) any {
	if strings.TrimSpace(val) == "" {
		return nil
	}
	return val
}

func toNullableInt(val *uint) any {
	if val == nil {
		return nil
	}
	return int64(*val)
}

func derefString(val *string) string {
	if val == nil {
		return ""
	}
	return *val
}
