package exportartifact

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
	commonModels "github.com/addp/common/models"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"
)

const (
	StatusPending = "pending"
	StatusRunning = "running"
	StatusSuccess = "success"
	StatusFailed  = "failed"

	manifestVersion = "addp.export_artifact.v1"
)

var (
	ErrSessionNotFound = errors.New("export session not found")
	ErrNotReady        = errors.New("export file is not ready")
)

// Session is the shared control-plane record for a short-lived export artifact.
// Repositories choose the owning module table explicitly; Session has no global table.
type Session struct {
	ID                  uint                 `json:"id" gorm:"primaryKey"`
	TenantID            uint                 `json:"tenant_id" gorm:"not null;index:idx_export_sessions_scope_status_created,priority:1"`
	UserID              uint                 `json:"user_id" gorm:"not null;index:idx_export_sessions_scope_status_created,priority:2"`
	SourceRef           string               `json:"source_ref" gorm:"column:source_item_locator;type:text;not null"`
	Format              string               `json:"format" gorm:"size:64;not null"`
	FileName            string               `json:"file_name" gorm:"size:512;not null"`
	TargetParentLocator string               `json:"target_parent_locator" gorm:"type:text;not null"`
	TargetLocator       string               `json:"target_locator" gorm:"type:text;not null"`
	ArtifactManifest    commonModels.JSONMap `json:"artifact_manifest,omitempty" gorm:"type:jsonb"`
	TransferExecutionID string               `json:"transfer_execution_id" gorm:"size:64;not null;index"`
	Status              string               `json:"status" gorm:"size:32;not null;index:idx_export_sessions_scope_status_created,priority:3"`
	ErrorMessage        string               `json:"error_message,omitempty" gorm:"type:text"`
	CreatedAt           time.Time            `json:"created_at" gorm:"index:idx_export_sessions_scope_status_created,priority:4,sort:desc"`
	UpdatedAt           time.Time            `json:"updated_at"`
}

type Store interface {
	Create(context.Context, *Session) error
	Get(context.Context, uint, uint, uint) (*Session, error)
	UpdateStatus(context.Context, *Session) error
	MarkRunningExpired(context.Context, time.Time) (int64, error)
	ListExpiredFinalSessions(context.Context, time.Time, time.Time, int) ([]*Session, error)
	Delete(context.Context, uint, uint, uint) error
}

type CleanupOptions struct {
	SuccessRetention time.Duration
	FailedRetention  time.Duration
	MaxRunningAge    time.Duration
	Interval         time.Duration
}

type CleanupResult struct {
	MarkedExpired   int64
	DeletedSessions int
	DeletedObjects  int
}

func NormalizeCleanupOptions(opts CleanupOptions) CleanupOptions {
	if opts.SuccessRetention <= 0 {
		opts.SuccessRetention = 24 * time.Hour
	}
	if opts.FailedRetention <= 0 {
		opts.FailedRetention = 6 * time.Hour
	}
	if opts.MaxRunningAge <= 0 {
		opts.MaxRunningAge = 6 * time.Hour
	}
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Minute
	}
	return opts
}

// CleanupExpiredOnce removes expired staged objects and their control-plane
// sessions. Both Manager and Develop call this implementation so retention and
// deletion semantics cannot drift between export entry points.
func CleanupExpiredOnce(ctx context.Context, store Store, minioClient *minio.Client, bucket string, opts CleanupOptions, now time.Time) (CleanupResult, error) {
	result := CleanupResult{}
	if store == nil || minioClient == nil || strings.Trim(bucket, "/") == "" {
		return result, errors.New("export artifact cleanup is not configured")
	}
	opts = NormalizeCleanupOptions(opts)
	marked, err := store.MarkRunningExpired(ctx, now.Add(-opts.MaxRunningAge))
	if err != nil {
		return result, fmt.Errorf("mark expired export sessions: %w", err)
	}
	result.MarkedExpired = marked
	for {
		sessions, err := store.ListExpiredFinalSessions(ctx, now.Add(-opts.SuccessRetention), now.Add(-opts.FailedRetention), 100)
		if err != nil {
			return result, fmt.Errorf("list expired export sessions: %w", err)
		}
		if len(sessions) == 0 {
			return result, nil
		}
		var cleanupErrors []error
		for _, session := range sessions {
			if session == nil {
				continue
			}
			prefix, err := SessionObjectPrefix(session, bucket)
			if err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("resolve export session %d prefix: %w", session.ID, err))
				continue
			}
			deleted, err := deleteObjectPrefix(ctx, minioClient, strings.Trim(bucket, "/"), strings.Trim(prefix, "/")+"/")
			if err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("delete export session %d objects: %w", session.ID, err))
				continue
			}
			if err := store.Delete(ctx, session.ID, session.TenantID, session.UserID); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("delete export session %d: %w", session.ID, err))
				continue
			}
			result.DeletedObjects += deleted
			result.DeletedSessions++
		}
		if len(cleanupErrors) > 0 {
			return result, errors.Join(cleanupErrors...)
		}
		if len(sessions) < 100 {
			return result, nil
		}
	}
}

func deleteObjectPrefix(ctx context.Context, minioClient *minio.Client, bucket, prefix string) (int, error) {
	prefix = strings.TrimPrefix(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return 0, nil
	}
	deleted := 0
	objects := minioClient.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true})
	for object := range objects {
		if object.Err != nil {
			return deleted, object.Err
		}
		if strings.TrimSpace(object.Key) == "" {
			continue
		}
		if err := minioClient.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

type GormStore struct {
	db    *gorm.DB
	table string
}

func NewGormStore(db *gorm.DB, table string) *GormStore {
	return &GormStore{db: db, table: strings.TrimSpace(table)}
}

func EnsureStore(db *gorm.DB, table string) error {
	if db == nil || strings.TrimSpace(table) == "" {
		return errors.New("export session database and table are required")
	}
	return db.Table(strings.TrimSpace(table)).AutoMigrate(&Session{})
}

func (s *GormStore) Create(ctx context.Context, session *Session) error {
	return s.db.WithContext(ctx).Table(s.table).Create(session).Error
}

func (s *GormStore) Get(ctx context.Context, id, tenantID, userID uint) (*Session, error) {
	var session Session
	err := s.db.WithContext(ctx).Table(s.table).
		Where("id = ? AND tenant_id = ? AND user_id = ?", id, tenantID, userID).
		First(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &session, err
}

func (s *GormStore) UpdateStatus(ctx context.Context, session *Session) error {
	return s.db.WithContext(ctx).Table(s.table).
		Where("id = ? AND tenant_id = ? AND user_id = ?", session.ID, session.TenantID, session.UserID).
		Updates(map[string]interface{}{
			"transfer_execution_id": session.TransferExecutionID,
			"status":                session.Status, "error_message": session.ErrorMessage,
			"artifact_manifest": session.ArtifactManifest, "updated_at": time.Now(),
		}).Error
}

func (s *GormStore) MarkRunningExpired(ctx context.Context, before time.Time) (int64, error) {
	result := s.db.WithContext(ctx).Table(s.table).
		Where("status IN ? AND created_at < ?", []string{StatusPending, StatusRunning}, before).
		Updates(map[string]interface{}{
			"status": StatusFailed, "error_message": "export session expired before completion", "updated_at": time.Now(),
		})
	return result.RowsAffected, result.Error
}

func (s *GormStore) ListExpiredFinalSessions(ctx context.Context, successBefore, failedBefore time.Time, limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = 100
	}
	var sessions []*Session
	err := s.db.WithContext(ctx).Table(s.table).
		Where("(status = ? AND updated_at < ?) OR (status = ? AND updated_at < ?)", StatusSuccess, successBefore, StatusFailed, failedBefore).
		Order("updated_at ASC").Limit(limit).Find(&sessions).Error
	return sessions, err
}

func (s *GormStore) Delete(ctx context.Context, id, tenantID, userID uint) error {
	return s.db.WithContext(ctx).Table(s.table).
		Where("id = ? AND tenant_id = ? AND user_id = ?", id, tenantID, userID).
		Delete(&Session{}).Error
}

type TransferClient interface {
	CreateExecution(context.Context, *commonClient.CreateTransferExecutionRequest) (*commonClient.CreateTransferExecutionResponse, error)
	GetExecution(string, uint) (*commonClient.TransferExecutionResponse, error)
}

type Service struct {
	transfer     TransferClient
	store        Store
	minio        *minio.Client
	bucket       string
	owner        string
	downloadBase string
}

type CreateRequest struct {
	TenantID        uint
	UserID          uint
	SourceRef       string
	Format          format.FormatType
	FileName        string
	ExecutionName   string
	ExecutionConfig commonClient.TransferExecutionConfig
}

type SessionResponse struct {
	ID                  uint   `json:"id"`
	SourceRef           string `json:"source_ref"`
	Format              string `json:"format"`
	FileName            string `json:"file_name"`
	TransferExecutionID string `json:"transfer_execution_id"`
	Status              string `json:"status"`
	ErrorMessage        string `json:"error_message,omitempty"`
	DownloadURL         string `json:"download_url,omitempty"`
	CreatedAt           string `json:"created_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

type File struct {
	Reader      io.ReadCloser
	FileName    string
	ContentType string
}

func NewService(transfer TransferClient, store Store, minioClient *minio.Client, bucket, owner, downloadBase string) *Service {
	return &Service{
		transfer: transfer, store: store, minio: minioClient,
		bucket: strings.Trim(bucket, "/"), owner: strings.Trim(strings.TrimSpace(owner), "/"),
		downloadBase: strings.TrimRight(strings.TrimSpace(downloadBase), "/"),
	}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*SessionResponse, error) {
	if s == nil || s.transfer == nil || s.store == nil || s.minio == nil || s.bucket == "" || s.owner == "" {
		return nil, errors.New("export artifact service is not configured")
	}
	if req.TenantID == 0 || req.UserID == 0 || strings.TrimSpace(req.SourceRef) == "" {
		return nil, errors.New("export tenant, user and source are required")
	}
	formatType := format.FormatType(strings.ToLower(strings.TrimSpace(string(req.Format))))
	if formatType == "" {
		formatType = format.FormatCSV
	}
	now := time.Now()
	exportID := uuid.NewString()
	prefix := fmt.Sprintf("tenant_%d/export/%s/%s/%s/", req.TenantID, s.owner, now.Format("20060102"), exportID)
	baseName := sanitizeBaseName(req.FileName)
	targetName := withExtension(baseName, formatType)
	downloadName := downloadFileName(baseName, formatType)
	objectPath := strings.Trim(prefix, "/") + "/" + targetName
	parentLocator := InfraPrefixLocator(s.bucket, prefix)
	targetLocator := InfraObjectLocator(s.bucket, objectPath)

	config := req.ExecutionConfig
	config.Target.ParentLocator = parentLocator
	config.Target.Locator = ""
	config.Target.Name = targetName
	executionName := strings.TrimSpace(req.ExecutionName)
	if executionName == "" {
		executionName = s.owner + "_export_" + strings.TrimSuffix(targetName, path.Ext(targetName))
	}
	session := &Session{
		TenantID: req.TenantID, UserID: req.UserID, SourceRef: strings.TrimSpace(req.SourceRef),
		Format: string(formatType), FileName: downloadName, TargetParentLocator: parentLocator,
		TargetLocator: targetLocator, Status: StatusPending,
	}
	if err := s.store.Create(ctx, session); err != nil {
		return nil, fmt.Errorf("create export session: %w", err)
	}
	created, err := s.transfer.CreateExecution(ctx, &commonClient.CreateTransferExecutionRequest{
		Name: executionName, Config: config, AutoScanMetadata: false, BatchSize: 1000, TenantID: req.TenantID,
	})
	if err != nil {
		session.Status = StatusFailed
		session.ErrorMessage = "failed to create transfer export execution"
		_ = s.store.UpdateStatus(ctx, session)
		return nil, fmt.Errorf("create transfer export execution: %w", err)
	}
	if created == nil || strings.TrimSpace(created.ExecutionID) == "" {
		session.Status = StatusFailed
		session.ErrorMessage = "transfer export execution id is empty"
		_ = s.store.UpdateStatus(ctx, session)
		return nil, errors.New("transfer export execution id is empty")
	}
	session.TransferExecutionID = strings.TrimSpace(created.ExecutionID)
	if err := s.store.UpdateStatus(ctx, session); err != nil {
		return nil, fmt.Errorf("attach transfer execution to export session: %w", err)
	}
	return s.response(session), nil
}

func (s *Service) Get(ctx context.Context, id, tenantID, userID uint) (*SessionResponse, error) {
	session, err := s.getSession(ctx, id, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.refresh(ctx, session); err != nil {
		return nil, err
	}
	return s.response(session), nil
}

func (s *Service) Open(ctx context.Context, id, tenantID, userID uint) (*File, error) {
	session, err := s.getSession(ctx, id, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.refresh(ctx, session); err != nil {
		return nil, err
	}
	if session.Status != StatusSuccess {
		return nil, ErrNotReady
	}
	manifest := manifestFromJSON(session.ArtifactManifest)
	if manifest.Layout == format.LayoutMulti || manifest.Download.Kind == "zip" || isMultiFormat(format.FormatType(session.Format)) {
		return s.openZip(ctx, session, manifest)
	}
	objectPath, err := InfraObjectPath(session.TargetLocator, s.bucket)
	if err != nil {
		return nil, err
	}
	object, err := s.minio.GetObject(ctx, s.bucket, objectPath, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	if _, err := object.Stat(); err != nil {
		_ = object.Close()
		return nil, err
	}
	contentType := mime.TypeByExtension(path.Ext(session.FileName))
	if contentType == "" {
		contentType = format.FormatToMIME(format.FormatType(session.Format))
	}
	return &File{Reader: object, FileName: session.FileName, ContentType: contentType}, nil
}

func (s *Service) getSession(ctx context.Context, id, tenantID, userID uint) (*Session, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("export artifact store is not configured")
	}
	session, err := s.store.Get(ctx, id, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrSessionNotFound
	}
	return session, nil
}

func (s *Service) refresh(ctx context.Context, session *Session) error {
	if session == nil || session.Status == StatusSuccess || session.Status == StatusFailed {
		return nil
	}
	execution, err := s.transfer.GetExecution(session.TransferExecutionID, session.TenantID)
	if err != nil {
		return err
	}
	nextStatus := transferStatus(execution.Status)
	nextManifest := session.ArtifactManifest
	if nextStatus == StatusSuccess {
		nextManifest = buildManifest(session, execution.Metadata)
	}
	if nextStatus == session.Status && strings.TrimSpace(execution.ErrorMessage) == strings.TrimSpace(session.ErrorMessage) && jsonEqual(nextManifest, session.ArtifactManifest) {
		return nil
	}
	session.Status = nextStatus
	session.ErrorMessage = strings.TrimSpace(execution.ErrorMessage)
	session.ArtifactManifest = nextManifest
	return s.store.UpdateStatus(ctx, session)
}

func (s *Service) response(session *Session) *SessionResponse {
	response := &SessionResponse{
		ID: session.ID, SourceRef: session.SourceRef, Format: session.Format, FileName: session.FileName,
		TransferExecutionID: session.TransferExecutionID, Status: session.Status, ErrorMessage: session.ErrorMessage,
		CreatedAt: session.CreatedAt.Format(time.RFC3339), UpdatedAt: session.UpdatedAt.Format(time.RFC3339),
	}
	if session.Status == StatusSuccess {
		response.DownloadURL = fmt.Sprintf("%s/%d/file", s.downloadBase, session.ID)
	}
	return response
}

type artifactRef struct {
	Path      string `json:"path"`
	Role      string `json:"role,omitempty"`
	Required  bool   `json:"required,omitempty"`
	Primary   bool   `json:"primary,omitempty"`
	Extension string `json:"extension,omitempty"`
	Entry     string `json:"entry,omitempty"`
}

type artifactManifest struct {
	SchemaVersion string        `json:"schema_version"`
	DataType      string        `json:"data_type"`
	Format        string        `json:"format"`
	Layout        string        `json:"layout"`
	BaseName      string        `json:"base_name"`
	PrimaryRef    string        `json:"primary_ref,omitempty"`
	Refs          []artifactRef `json:"refs,omitempty"`
	Download      struct {
		Kind     string `json:"kind"`
		FileName string `json:"file_name"`
	} `json:"download"`
}

func buildManifest(session *Session, metadata map[string]interface{}) commonModels.JSONMap {
	refs := targetRefs(metadata)
	formatType := format.FormatType(session.Format)
	if len(refs) == 0 && !isMultiFormat(formatType) {
		objectPath, err := InfraObjectPath(session.TargetLocator, "")
		if err == nil {
			refs = []artifactRef{{Path: objectPath, Role: "main", Required: true, Primary: true, Extension: format.NormalizeExtension(path.Ext(objectPath)), Entry: path.Base(session.FileName)}}
		}
	}
	if len(refs) == 0 {
		return commonModels.JSONMap{}
	}
	multi := isMultiFormat(formatType)
	manifest := artifactManifest{SchemaVersion: manifestVersion, DataType: "table", Format: session.Format, Layout: format.LayoutSingle, BaseName: strings.TrimSuffix(path.Base(session.FileName), path.Ext(session.FileName)), Refs: refs}
	manifest.Download.Kind = "stream"
	manifest.Download.FileName = session.FileName
	if multi {
		manifest.Layout = format.LayoutMulti
		manifest.Download.Kind = "zip"
	}
	used := map[string]int{}
	for i := range manifest.Refs {
		if manifest.Refs[i].Extension == "" {
			manifest.Refs[i].Extension = format.NormalizeExtension(path.Ext(manifest.Refs[i].Path))
		}
		if manifest.Refs[i].Entry == "" {
			manifest.Refs[i].Entry = path.Base(manifest.Refs[i].Path)
		}
		manifest.Refs[i].Entry = uniqueEntry(cleanEntry(manifest.Refs[i].Entry), used)
		if manifest.Refs[i].Primary {
			manifest.PrimaryRef = manifest.Refs[i].Path
		}
	}
	bytes, _ := json.Marshal(manifest)
	var result commonModels.JSONMap
	_ = json.Unmarshal(bytes, &result)
	return result
}

func targetRefs(metadata map[string]interface{}) []artifactRef {
	bytes, err := json.Marshal(metadata["target_refs"])
	if err != nil {
		return nil
	}
	var refs []artifactRef
	if json.Unmarshal(bytes, &refs) != nil {
		return nil
	}
	result := refs[:0]
	for _, ref := range refs {
		ref.Path = strings.Trim(ref.Path, "/")
		if ref.Path != "" {
			result = append(result, ref)
		}
	}
	return result
}

func manifestFromJSON(raw commonModels.JSONMap) artifactManifest {
	bytes, _ := json.Marshal(raw)
	var result artifactManifest
	_ = json.Unmarshal(bytes, &result)
	return result
}

func (s *Service) openZip(ctx context.Context, session *Session, manifest artifactManifest) (*File, error) {
	if len(manifest.Refs) == 0 {
		return nil, ErrNotReady
	}
	prefix, err := SessionObjectPrefix(session, s.bucket)
	if err != nil {
		return nil, err
	}
	for i := range manifest.Refs {
		manifest.Refs[i].Path = strings.Trim(manifest.Refs[i].Path, "/")
		manifest.Refs[i].Entry = cleanEntry(manifest.Refs[i].Entry)
		if manifest.Refs[i].Path == "" || manifest.Refs[i].Entry == "" || !insidePrefix(manifest.Refs[i].Path, prefix) {
			return nil, errors.New("export artifact ref is outside session prefix")
		}
	}
	availableRefs := make([]artifactRef, 0, len(manifest.Refs))
	for _, ref := range manifest.Refs {
		if _, err := s.minio.StatObject(ctx, s.bucket, ref.Path, minio.StatObjectOptions{}); err != nil {
			if ref.Required {
				return nil, err
			}
			continue
		}
		availableRefs = append(availableRefs, ref)
	}
	if len(availableRefs) == 0 {
		return nil, ErrNotReady
	}
	reader, writer := io.Pipe()
	go func() { _ = writer.CloseWithError(s.writeZip(ctx, writer, availableRefs)) }()
	fileName := manifest.Download.FileName
	if fileName == "" {
		fileName = session.FileName
	}
	return &File{Reader: reader, FileName: fileName, ContentType: "application/zip"}, nil
}

func (s *Service) writeZip(ctx context.Context, writer io.Writer, refs []artifactRef) error {
	zipWriter := zip.NewWriter(writer)
	for _, ref := range refs {
		object, err := s.minio.GetObject(ctx, s.bucket, ref.Path, minio.GetObjectOptions{})
		if err != nil {
			_ = zipWriter.Close()
			return err
		}
		if _, err := object.Stat(); err != nil {
			_ = object.Close()
			_ = zipWriter.Close()
			return err
		}
		entry, err := zipWriter.Create(ref.Entry)
		if err != nil {
			_ = object.Close()
			_ = zipWriter.Close()
			return err
		}
		_, copyErr := io.Copy(entry, object)
		closeErr := object.Close()
		if copyErr != nil {
			_ = zipWriter.Close()
			return copyErr
		}
		if closeErr != nil {
			_ = zipWriter.Close()
			return closeErr
		}
	}
	return zipWriter.Close()
}

func InfraPrefixLocator(bucket, prefix string) string {
	return fmt.Sprintf("addp-infra://minio/%s/%s?type=prefix", strings.Trim(bucket, "/"), strings.Trim(prefix, "/"))
}

func InfraObjectLocator(bucket, objectPath string) string {
	return fmt.Sprintf("addp-infra://minio/%s/%s?type=object", strings.Trim(bucket, "/"), strings.Trim(objectPath, "/"))
}

func InfraObjectPath(locator, expectedBucket string) (string, error) {
	const prefix = "addp-infra://minio/"
	if !strings.HasPrefix(strings.TrimSpace(locator), prefix) {
		return "", errors.New("invalid infra minio locator")
	}
	withoutQuery := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(locator), prefix), "?", 2)[0]
	parts := strings.SplitN(withoutQuery, "/", 2)
	if len(parts) != 2 || strings.Trim(parts[0], "/") == "" || strings.Trim(parts[1], "/") == "" {
		return "", errors.New("invalid infra minio locator")
	}
	if expected := strings.Trim(expectedBucket, "/"); expected != "" && expected != strings.Trim(parts[0], "/") {
		return "", errors.New("export artifact bucket mismatch")
	}
	return strings.Trim(parts[1], "/"), nil
}

func SessionObjectPrefix(session *Session, expectedBucket string) (string, error) {
	if session == nil {
		return "", errors.New("export session is required")
	}
	if parent := strings.TrimSpace(session.TargetParentLocator); parent != "" {
		objectPath, err := InfraObjectPath(parent, expectedBucket)
		if err != nil {
			return "", err
		}
		return strings.Trim(objectPath, "/"), nil
	}
	objectPath, err := InfraObjectPath(session.TargetLocator, expectedBucket)
	if err != nil {
		return "", err
	}
	return strings.Trim(path.Dir(objectPath), "/"), nil
}

func sanitizeBaseName(fileName string) string {
	name := strings.TrimSpace(path.Base(strings.ReplaceAll(fileName, "\\", "/")))
	name = strings.TrimSuffix(name, path.Ext(name))
	if name == "" || name == "." {
		return "export"
	}
	return name
}

func withExtension(base string, formatType format.FormatType) string {
	ext := format.NormalizeExtension(format.DefaultWriteExtension(formatType, nil))
	if ext == "" {
		ext = "." + string(formatType)
	}
	return strings.TrimSuffix(base, path.Ext(base)) + ext
}

func downloadFileName(base string, formatType format.FormatType) string {
	if isMultiFormat(formatType) {
		return strings.TrimSuffix(base, path.Ext(base)) + ".zip"
	}
	return withExtension(base, formatType)
}

func isMultiFormat(formatType format.FormatType) bool {
	_, err := format.GetMultiTableWriterProvider(formatType)
	return err == nil
}

func transferStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success":
		return StatusSuccess
	case "failed", "timeout", "cancelled":
		return StatusFailed
	case "running":
		return StatusRunning
	default:
		return StatusPending
	}
}

func jsonEqual(left, right commonModels.JSONMap) bool {
	l, le := json.Marshal(left)
	r, re := json.Marshal(right)
	return le == nil && re == nil && string(l) == string(r)
}

func cleanEntry(name string) string {
	cleaned := path.Clean(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return ""
	}
	return cleaned
}

func uniqueEntry(name string, used map[string]int) string {
	if used[name] == 0 {
		used[name] = 1
		return name
	}
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for index := used[name] + 1; ; index++ {
		candidate := fmt.Sprintf("%s_%d%s", base, index, ext)
		if used[candidate] == 0 {
			used[name] = index
			used[candidate] = 1
			return candidate
		}
	}
}

func insidePrefix(objectPath, prefix string) bool {
	objectPath = strings.Trim(objectPath, "/")
	prefix = strings.Trim(prefix, "/")
	return objectPath == prefix || strings.HasPrefix(objectPath, prefix+"/")
}
