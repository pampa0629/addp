package service

import (
    "context"
    "database/sql"
    "fmt"
    "os"

    "github.com/addp/common/logger"
    "github.com/addp/common/spatial"
    "github.com/addp/manager/internal/repository"
)

// MVTService generates Mapbox Vector Tiles (MVT) from PostGIS tables for preview.
type MVTService struct {
    metadataRepo *repository.MetadataRepository
    resourceRepo *repository.ResourceRepository
}

func NewMVTService(meta *repository.MetadataRepository, res *repository.ResourceRepository) *MVTService {
    return &MVTService{metadataRepo: meta, resourceRepo: res}
}

// GetTile produces a single MVT tile for given z/x/y and table.
// Validates tenant access against the resource.
func (s *MVTService) GetTile(ctx context.Context, tenantID *uint, resourceID uint, schema, table, geomCol string, cols []string, z, x, y int, srid int) ([]byte, error) {
    // Fetch resource and validate tenant access
    res, err := s.resourceRepo.GetByID(resourceID)
    if err != nil {
        return nil, err
    }
    if !resourceAccessible(res, tenantID) {
        return nil, ErrResourceAccessDenied
    }

    // Decrypt connection info (password, etc.)
    connInfo, err := s.metadataRepo.DecryptConnectionInfo(res.ConnectionInfo)
    if err != nil {
        return nil, fmt.Errorf("decrypt connection info failed: %w", err)
    }

    // Build DSN (postgres)
    host, _ := connInfo["host"].(string)
    if host == "localhost" || host == "127.0.0.1" {
        if alias := os.Getenv("RESOURCE_LOCALHOST_ALIAS"); alias != "" {
            host = alias
        } else {
            host = "127.0.0.1"
        }
    }
    database, _ := connInfo["database"].(string)
    password, _ := connInfo["password"].(string)
    username, _ := connInfo["username"].(string)
    if username == "" {
        username, _ = connInfo["user"].(string)
    }
    var port string
    switch v := connInfo["port"].(type) {
    case float64:
        port = fmt.Sprintf("%.0f", v)
    case int:
        port = fmt.Sprintf("%d", v)
    case string:
        port = v
    }

    dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
        host, port, username, password, database,
    )

    db, err := sql.Open("postgres", dsn)
    if err != nil {
        return nil, err
    }
    defer db.Close()

    // Build query
    opt := spatial.MVTOptions{Layer: table, Extent: 4096, Buffer: 64, SRID: srid, Simplify: true}
    sqlStr, args := spatial.BuildMVTQuery(schema, table, geomCol, cols, z, x, y, opt, "id")

    // Execute
    var mvt []byte
    scanErr := db.QueryRowContext(ctx, sqlStr, args...).Scan(&mvt)
    if scanErr != nil {
        logger.L().Error("MVT query failed", "error", scanErr, "resource_id", resourceID, "schema", schema, "table", table, "z", z, "x", x, "y", y)
        return nil, scanErr
    }
    if mvt == nil {
        // Return empty tile (empty buffer) to avoid client errors
        return []byte{}, nil
    }
    return mvt, nil
}

// Note: access policy and ErrResourceAccessDenied are defined in metadata_service.go
