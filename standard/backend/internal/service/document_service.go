package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	commonapi "github.com/addp/common/api"
	commonclient "github.com/addp/common/client"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	"github.com/google/uuid"
	minio "github.com/minio/minio-go/v7"
)

const (
	minioBucket                   = "standard"
	defaultDocumentMaxFileSize    = 100 * 1024 * 1024
	defaultDocumentStorageTimeout = 30 * time.Second
	maxDocumentExtractionBytes    = 2 * 1024 * 1024
	copilotDocumentExtractPath    = "/api/v1/copilot/standard-documents/extract"
)

var (
	ErrDocumentStorageUnavailable            = errors.New("document storage unavailable")
	ErrDocumentFileTooLarge                  = errors.New("document file too large")
	ErrDocumentFileUpload                    = errors.New("document file upload failed")
	ErrDocumentFileDownload                  = errors.New("document file download failed")
	ErrDocumentFileCleanup                   = errors.New("document file cleanup failed")
	ErrDocumentFileNameInvalid               = errors.New("document file name invalid")
	ErrDocumentFileRequired                  = errors.New("document revision file required")
	ErrDocumentExtractionUnsupported         = errors.New("document extraction only supports markdown")
	ErrDocumentExtractionInvalid             = errors.New("document extraction result invalid")
	ErrDocumentCopilotUnavailable            = errors.New("document copilot unavailable")
	ErrDocumentPublicationHistory            = errors.New("document publication history exists")
	ErrDocumentCandidateFormalizationHistory = errors.New("document candidate formalization history exists")
	documentCandidateCodePattern             = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

type DocumentStorageOptions struct {
	MaxFileSize        int64
	Timeout            time.Duration
	CopilotURL         string
	ServiceTokenSource commonclient.ServiceTokenProvider
	HTTPClient         *http.Client
}

type documentContextReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *documentContextReadCloser) Close() error { r.cancel(); return r.ReadCloser.Close() }

type DocumentService struct {
	repo               *repository.DocumentRepository
	refs               *repository.TenantReferenceRepository
	objectStore        documentObjectStore
	maxFileSize        int64
	timeout            time.Duration
	copilotURL         string
	serviceTokenSource commonclient.ServiceTokenProvider
	httpClient         *http.Client
	stopCh             chan struct{}
}

func NewDocumentService(repo *repository.DocumentRepository, refs *repository.TenantReferenceRepository, minioClient *minio.Client, options DocumentStorageOptions) *DocumentService {
	if options.MaxFileSize <= 0 {
		options.MaxFileSize = defaultDocumentMaxFileSize
	}
	if options.Timeout <= 0 {
		options.Timeout = defaultDocumentStorageTimeout
	}
	if options.CopilotURL == "" {
		options.CopilotURL = "http://localhost:8087"
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	}
	svc := &DocumentService{repo: repo, refs: refs, objectStore: newMinioDocumentObjectStore(minioClient), maxFileSize: options.MaxFileSize, timeout: options.Timeout, copilotURL: strings.TrimRight(options.CopilotURL, "/"), serviceTokenSource: options.ServiceTokenSource, httpClient: options.HTTPClient, stopCh: make(chan struct{})}
	if svc.objectStore != nil {
		ctx, cancel := context.WithTimeout(context.Background(), svc.timeout)
		defer cancel()
		exists, err := svc.objectStore.BucketExists(ctx, minioBucket)
		if err != nil {
			log.Printf("standard document bucket check failed: %v", err)
		} else if !exists {
			if err := svc.objectStore.MakeBucket(ctx, minioBucket, minio.MakeBucketOptions{}); err != nil {
				log.Printf("standard document bucket creation failed: %v", err)
			}
		}
	}
	go svc.runFileCleanupWorker()
	return svc
}

func (s *DocumentService) Stop() {
	if s == nil || s.stopCh == nil {
		return
	}
	close(s.stopCh)
}
func (s *DocumentService) MaxFileSize() int64 { return s.maxFileSize }

func (s *DocumentService) ListDocuments(tenantID int64, opts repository.ListDocumentOptions) ([]models.DocumentAggregate, int64, error) {
	return s.repo.List(tenantID, opts)
}
func (s *DocumentService) GetDocument(id, tenantID int64) (*models.DocumentAggregate, error) {
	return s.repo.GetAggregate(id, tenantID)
}
func (s *DocumentService) GetDocumentAt(id, tenantID int64, asOf time.Time) (*models.DocumentAggregate, error) {
	return s.repo.GetAggregateAt(id, tenantID, asOf)
}

func (s *DocumentService) CreateDocument(req *models.CreateDocumentRequest, tenantID, userID int64) (*models.DocumentAggregate, error) {
	document, revision, err := s.newDocument(req, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Create(document, revision); err != nil {
		return nil, err
	}
	return s.repo.GetAggregate(document.ID, tenantID)
}

func (s *DocumentService) newDocument(req *models.CreateDocumentRequest, tenantID, userID int64) (*models.Document, *models.DocumentRevision, error) {
	scopeType, err := validateTenantStandardScope(s.refs, tenantID, req.ScopeType, req.OwnerDomainID)
	if err != nil {
		return nil, nil, err
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		return nil, nil, ErrInvalidStandardRevision
	}
	exists, err := s.repo.ExistsByCode(code, tenantID)
	if err != nil {
		return nil, nil, err
	}
	if exists {
		return nil, nil, commonapi.ErrConflict
	}
	docType := strings.TrimSpace(req.DocType)
	if !validDocumentType(docType) {
		return nil, nil, ErrInvalidStandardRevision
	}
	revision := &models.DocumentRevision{Name: strings.TrimSpace(req.Name), VersionLabel: strings.TrimSpace(req.VersionLabel), PublishDate: req.PublishDate, Description: req.Description, ChangeSummary: strings.TrimSpace(req.ChangeSummary), EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, CreatedBy: userID}
	if err := validateDocumentRevision(revision, false); err != nil {
		return nil, nil, err
	}
	document := &models.Document{TenantID: tenantID, ScopeType: scopeType, OwnerDomainID: req.OwnerDomainID, Code: code, DocType: docType, SourceOrg: strings.TrimSpace(req.SourceOrg), StewardID: req.StewardID, Tags: req.Tags, CreatedBy: userID, LifecycleState: "active"}
	return document, revision, nil
}

func (s *DocumentService) UpdateDocument(id, tenantID, userID int64, req *models.UpdateDocumentRequest) (*models.DocumentAggregate, error) {
	scopeType, err := validateTenantStandardScope(s.refs, tenantID, req.ScopeType, req.OwnerDomainID)
	if err != nil {
		return nil, err
	}
	if !validDocumentType(req.DocType) {
		return nil, ErrInvalidStandardRevision
	}
	document, err := s.repo.GetByID(id, tenantID)
	if err != nil {
		return nil, err
	}
	document.ScopeType, document.OwnerDomainID, document.DocType, document.SourceOrg = scopeType, req.OwnerDomainID, req.DocType, strings.TrimSpace(req.SourceOrg)
	document.StewardID, document.Tags, document.UpdatedBy = req.StewardID, req.Tags, &userID
	if err := s.repo.UpdateIdentity(document, req.Version); err != nil {
		return nil, err
	}
	return s.repo.GetAggregate(id, tenantID)
}

func (s *DocumentService) DeleteDocument(id, tenantID int64) error {
	cleanups, err := s.repo.DeleteUnpublished(id, tenantID)
	if err != nil {
		if errors.Is(err, repository.ErrDocumentPublicationHistory) {
			return ErrDocumentPublicationHistory
		}
		if errors.Is(err, repository.ErrDocumentCandidateFormalizationHistory) {
			return ErrDocumentCandidateFormalizationHistory
		}
		return err
	}
	for _, cleanup := range cleanups {
		s.tryFileCleanup(cleanup)
	}
	return nil
}

func (s *DocumentService) ListRevisions(id, tenantID int64) ([]models.DocumentRevision, error) {
	return s.repo.ListRevisions(id, tenantID)
}
func (s *DocumentService) GetRevision(id, revisionID, tenantID int64) (*models.DocumentRevision, error) {
	return s.repo.GetRevision(id, revisionID, tenantID)
}
func (s *DocumentService) CreateRevision(id, tenantID, userID int64, req *models.CreateDocumentRevisionRequest) (*models.DocumentAggregate, error) {
	if strings.TrimSpace(req.ChangeSummary) == "" {
		return nil, ErrInvalidStandardRevision
	}
	if _, err := s.repo.CreateDraft(id, tenantID, userID, req.Version, strings.TrimSpace(req.ChangeSummary)); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}
func (s *DocumentService) UpdateRevision(id, revisionID, tenantID, userID int64, req *models.UpdateDocumentRevisionRequest) (*models.DocumentAggregate, error) {
	revision := &models.DocumentRevision{ID: revisionID, DocumentID: id, Name: strings.TrimSpace(req.Name), VersionLabel: strings.TrimSpace(req.VersionLabel), PublishDate: req.PublishDate, Description: req.Description, ChangeSummary: strings.TrimSpace(req.ChangeSummary), EffectiveFrom: req.EffectiveFrom, EffectiveTo: req.EffectiveTo, UpdatedBy: &userID}
	if err := validateDocumentRevision(revision, false); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateDraft(id, revisionID, tenantID, userID, req.Version, revision); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}
func (s *DocumentService) SubmitRevision(id, revisionID, tenantID, userID, version int64) (*models.DocumentAggregate, error) {
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if err := validateDocumentRevision(revision, true); err != nil {
		return nil, err
	}
	if err := s.repo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusDraft, models.RevisionStatusInReview); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}
func (s *DocumentService) ReturnRevision(id, revisionID, tenantID, userID, version int64) (*models.DocumentAggregate, error) {
	if err := s.repo.TransitionRevision(id, revisionID, tenantID, userID, version, models.RevisionStatusInReview, models.RevisionStatusDraft); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}
func (s *DocumentService) PublishRevision(id, revisionID, tenantID, userID, version int64) (*models.DocumentAggregate, error) {
	revision, err := s.repo.GetRevision(id, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if revision.Status != models.RevisionStatusInReview {
		return nil, ErrInvalidRevisionTransition
	}
	if err := validateDocumentRevision(revision, true); err != nil {
		return nil, err
	}
	if err := s.repo.PublishRevision(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}
func (s *DocumentService) WithdrawRevision(id, revisionID, tenantID, userID, version int64) (*models.DocumentAggregate, error) {
	if err := s.repo.WithdrawPublished(id, revisionID, tenantID, userID, version); err != nil {
		return nil, mapRevisionError(err)
	}
	return s.repo.GetAggregate(id, tenantID)
}

func validDocumentType(value string) bool {
	switch value {
	case "national", "industry", "internal", "reference":
		return true
	default:
		return false
	}
}
func validateDocumentRevision(revision *models.DocumentRevision, requireFile bool) error {
	if revision == nil || strings.TrimSpace(revision.Name) == "" || strings.TrimSpace(revision.ChangeSummary) == "" {
		return ErrInvalidStandardRevision
	}
	if revision.EffectiveFrom != nil && revision.EffectiveTo != nil && !revision.EffectiveFrom.Before(*revision.EffectiveTo) {
		return ErrInvalidStandardRevision
	}
	if requireFile && (revision.FileKey == "" || revision.FileName == "" || revision.ContentSHA256 == "") {
		return ErrDocumentFileRequired
	}
	if requireFile && revision.EffectiveFrom == nil {
		return ErrInvalidStandardRevision
	}
	return nil
}

func (s *DocumentService) UploadFile(documentID, revisionID, tenantID, userID, version int64, fileName string, content io.Reader, size int64, contentType string) (*models.DocumentAggregate, error) {
	if s.objectStore == nil {
		return nil, ErrDocumentStorageUnavailable
	}
	if size < 0 || size > s.maxFileSize {
		return nil, ErrDocumentFileTooLarge
	}
	if _, err := s.repo.GetRevision(documentID, revisionID, tenantID); err != nil {
		return nil, commonapi.ErrNotFound
	}
	fileName, err := sanitizeDocumentFileName(fileName)
	if err != nil {
		return nil, err
	}
	extension := strings.ToLower(filepath.Ext(fileName))
	fileKey := fmt.Sprintf("tenant_%d/documents/%d/revisions/%d/%s%s", tenantID, documentID, revisionID, uuid.NewString(), extension)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	hasher := sha256.New()
	reader := io.TeeReader(content, hasher)
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	_, err = s.objectStore.PutObject(ctx, minioBucket, fileKey, reader, size, minio.PutObjectOptions{ContentType: contentType})
	cancel()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDocumentFileUpload, err)
	}
	contentHash := hex.EncodeToString(hasher.Sum(nil))
	cleanup, err := s.repo.ReplaceDraftFile(documentID, revisionID, tenantID, userID, version, fileKey, fileName, size, contentType, contentHash)
	if err != nil {
		if cleanupErr := s.removeObject(fileKey); cleanupErr != nil {
			if _, queueErr := s.repo.EnqueueFileCleanup(fileKey); queueErr != nil {
				log.Printf("standard document new file cleanup failed and enqueue failed, key=%q: cleanup=%v enqueue=%v", fileKey, cleanupErr, queueErr)
			}
		}
		return nil, err
	}
	if cleanup != nil {
		s.tryFileCleanup(*cleanup)
	}
	return s.repo.GetAggregate(documentID, tenantID)
}

func (s *DocumentService) DownloadFile(documentID, revisionID, tenantID int64) (io.ReadCloser, string, string, int64, error) {
	if s.objectStore == nil {
		return nil, "", "", 0, ErrDocumentStorageUnavailable
	}
	revision, err := s.repo.GetRevision(documentID, revisionID, tenantID)
	if err != nil {
		return nil, "", "", 0, commonapi.ErrNotFound
	}
	if revision.FileKey == "" {
		return nil, "", "", 0, commonapi.ErrNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	if _, err := s.objectStore.StatObject(ctx, minioBucket, revision.FileKey, minio.StatObjectOptions{}); err != nil {
		cancel()
		return nil, "", "", 0, fmt.Errorf("%w: %v", ErrDocumentFileDownload, err)
	}
	obj, err := s.objectStore.GetObject(ctx, minioBucket, revision.FileKey, minio.GetObjectOptions{})
	if err != nil {
		cancel()
		return nil, "", "", 0, fmt.Errorf("%w: %v", ErrDocumentFileDownload, err)
	}
	return &documentContextReadCloser{ReadCloser: obj, cancel: cancel}, revision.FileName, revision.MediaType, revision.FileSize, nil
}

type documentExtractionSection struct {
	SectionPath string `json:"section_path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Text        string `json:"text"`
}
type copilotDocumentExtractRequest struct {
	DocumentName string                      `json:"document_name"`
	VersionLabel string                      `json:"version_label"`
	Sections     []documentExtractionSection `json:"sections"`
}
type copilotEvidence struct {
	SectionPath string `json:"section_path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
}
type copilotCodeItem struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
}
type copilotCandidatePayload struct {
	DataType           *string           `json:"data_type"`
	ValueDomainKind    *string           `json:"value_domain_kind"`
	CodeSetCode        *string           `json:"code_set_code"`
	Unit               *string           `json:"unit"`
	CalculationFormula *string           `json:"calculation_formula"`
	StatisticalScope   *string           `json:"statistical_scope"`
	Aggregation        *string           `json:"aggregation"`
	Dimensions         []string          `json:"dimensions"`
	Items              []copilotCodeItem `json:"items"`
}
type copilotCandidate struct {
	CandidateType string                  `json:"candidate_type"`
	Code          string                  `json:"code"`
	Name          string                  `json:"name"`
	Definition    string                  `json:"definition"`
	Payload       copilotCandidatePayload `json:"payload"`
	Evidences     []copilotEvidence       `json:"evidences"`
}
type copilotDocumentExtractResponse struct {
	Candidates []copilotCandidate `json:"candidates"`
}

func (s *DocumentService) ExtractCandidates(ctx context.Context, documentID, revisionID, tenantID, userID, version int64) (*models.DocumentExtraction, error) {
	revision, err := s.repo.GetRevision(documentID, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	if !isMarkdownRevision(revision) {
		return nil, ErrDocumentExtractionUnsupported
	}
	reader, _, _, size, err := s.DownloadFile(documentID, revisionID, tenantID)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if size > maxDocumentExtractionBytes {
		return nil, ErrDocumentFileTooLarge
	}
	content, err := io.ReadAll(io.LimitReader(reader, maxDocumentExtractionBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxDocumentExtractionBytes {
		return nil, ErrDocumentFileTooLarge
	}
	sections := splitMarkdownSections(string(content))
	if len(sections) == 0 {
		return nil, ErrDocumentExtractionInvalid
	}
	response, err := s.callCopilotExtract(ctx, tenantID, copilotDocumentExtractRequest{DocumentName: revision.Name, VersionLabel: revision.VersionLabel, Sections: sections})
	if err != nil {
		return nil, err
	}
	extraction, err := buildDocumentExtraction(tenantID, revisionID, userID, string(content), sections, response)
	if err != nil {
		return nil, err
	}
	document, err := s.repo.GetByID(documentID, tenantID)
	if err != nil {
		return nil, err
	}
	result := []models.DocumentExtraction{*extraction}
	if err := s.attachCandidateComparisons(document, result); err != nil {
		return nil, err
	}
	*extraction = result[0]
	if err := s.repo.CreateExtraction(documentID, tenantID, version, extraction); err != nil {
		return nil, err
	}
	return extraction, nil
}

func (s *DocumentService) callCopilotExtract(ctx context.Context, tenantID int64, request copilotDocumentExtractRequest) (*copilotDocumentExtractResponse, error) {
	if s.serviceTokenSource == nil {
		return nil, ErrDocumentCopilotUnavailable
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := s.serviceTokenSource.Token(ctx, uint(tenantID))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDocumentCopilotUnavailable, err)
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.copilotURL+copilotDocumentExtractPath, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+token)
		resp, err := s.httpClient.Do(httpReq)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDocumentCopilotUnavailable, err)
		}
		if resp.StatusCode == http.StatusUnauthorized && attempt == 0 {
			_ = resp.Body.Close()
			if invalidator, ok := s.serviceTokenSource.(commonclient.ServiceTokenInvalidator); ok {
				invalidator.InvalidateToken(uint(tenantID), token)
				continue
			}
		}
		if resp.StatusCode != http.StatusOK {
			responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("%w: status=%d body=%s", ErrDocumentCopilotUnavailable, resp.StatusCode, string(responseBody))
		}
		var result copilotDocumentExtractResponse
		decoder := json.NewDecoder(resp.Body)
		decoder.DisallowUnknownFields()
		err = decoder.Decode(&result)
		if err == nil {
			var trailing interface{}
			if trailingErr := decoder.Decode(&trailing); trailingErr != io.EOF {
				err = errors.New("copilot response contains trailing JSON")
			}
		}
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrDocumentExtractionInvalid, err)
		}
		return &result, nil
	}
	return nil, ErrDocumentCopilotUnavailable
}

func splitMarkdownSections(content string) []documentExtractionSection {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	type pending struct {
		path  string
		start int
		body  []string
	}
	var sections []documentExtractionSection
	current := pending{path: "文档正文", start: 1}
	headings := make([]string, 0, 6)
	flush := func(end int) {
		if strings.TrimSpace(strings.Join(current.body, "\n")) != "" {
			numbered := make([]string, len(current.body))
			for index, line := range current.body {
				numbered[index] = fmt.Sprintf("L%d: %s", current.start+index, line)
			}
			sections = append(sections, documentExtractionSection{SectionPath: current.path, StartLine: current.start, EndLine: end, Text: strings.Join(numbered, "\n")})
		}
	}
	headingPattern := regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)
	for index, line := range lines {
		match := headingPattern.FindStringSubmatch(line)
		if match == nil {
			current.body = append(current.body, line)
			continue
		}
		flush(index)
		level := len(match[1])
		if len(headings) >= level {
			headings = headings[:level-1]
		}
		for len(headings) < level-1 {
			headings = append(headings, "")
		}
		headings = append(headings, strings.TrimSpace(match[2]))
		pathParts := make([]string, 0, len(headings))
		for _, heading := range headings {
			if heading != "" {
				pathParts = append(pathParts, heading)
			}
		}
		current = pending{path: strings.Join(pathParts, " / "), start: index + 1, body: []string{line}}
	}
	flush(len(lines))
	return sections
}

func buildDocumentExtraction(tenantID, revisionID, userID int64, content string, sections []documentExtractionSection, response *copilotDocumentExtractResponse) (*models.DocumentExtraction, error) {
	if response == nil || len(response.Candidates) > 200 {
		return nil, ErrDocumentExtractionInvalid
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	sectionIntervals := make(map[string][][2]int, len(sections))
	for _, section := range sections {
		sectionIntervals[section.SectionPath] = append(sectionIntervals[section.SectionPath], [2]int{section.StartLine, section.EndLine})
	}
	extraction := &models.DocumentExtraction{TenantID: tenantID, DocumentRevisionID: revisionID, Status: "completed", RequestedBy: userID}
	seen := map[string]struct{}{}
	for _, raw := range response.Candidates {
		candidateType := strings.TrimSpace(raw.CandidateType)
		if candidateType != "glossary" && candidateType != "element" && candidateType != "code_set" && candidateType != "metric" {
			continue
		}
		if !validCopilotCandidateDataType(candidateType, raw.Payload.DataType) {
			return nil, ErrDocumentExtractionInvalid
		}
		if !validCopilotCandidateValueDomainKind(candidateType, raw.Payload.ValueDomainKind) {
			return nil, ErrDocumentExtractionInvalid
		}
		if !validCopilotCandidateCodeSetCode(candidateType, raw.Payload.ValueDomainKind, raw.Payload.CodeSetCode) {
			return nil, ErrDocumentExtractionInvalid
		}
		code, name, definition := strings.TrimSpace(raw.Code), strings.TrimSpace(raw.Name), strings.TrimSpace(raw.Definition)
		if !documentCandidateCodePattern.MatchString(code) || len(code) > 100 || name == "" || utf8.RuneCountInString(name) > 200 || definition == "" || utf8.RuneCountInString(definition) > 4000 {
			continue
		}
		key := candidateType + "\x00" + code
		if _, ok := seen[key]; ok {
			continue
		}
		candidate := models.DocumentExtractionCandidate{CandidateType: candidateType, Code: code, Name: name, Definition: definition, Payload: normalizeCopilotCandidatePayload(raw.Payload), Status: "pending", Version: 1}
		for _, source := range raw.Evidences[:min(len(raw.Evidences), 20)] {
			if source.StartLine < 1 || source.EndLine < source.StartLine || source.EndLine > len(lines) {
				continue
			}
			sectionPath := strings.TrimSpace(source.SectionPath)
			insideSection := false
			for _, interval := range sectionIntervals[sectionPath] {
				if interval[0] <= source.StartLine && source.EndLine <= interval[1] {
					insideSection = true
					break
				}
			}
			if !insideSection {
				continue
			}
			excerpt := strings.TrimSpace(strings.Join(lines[source.StartLine-1:source.EndLine], "\n"))
			if excerpt == "" {
				continue
			}
			sum := sha256.Sum256([]byte(excerpt))
			candidate.Evidences = append(candidate.Evidences, models.DocumentExtractionEvidence{DocumentRevisionID: revisionID, SectionPath: sectionPath, StartLine: source.StartLine, EndLine: source.EndLine, Excerpt: excerpt, ExcerptHash: hex.EncodeToString(sum[:])})
		}
		if len(candidate.Evidences) > 0 {
			seen[key] = struct{}{}
			extraction.Candidates = append(extraction.Candidates, candidate)
		}
	}
	sort.SliceStable(extraction.Candidates, func(i, j int) bool {
		if extraction.Candidates[i].CandidateType == extraction.Candidates[j].CandidateType {
			return extraction.Candidates[i].Code < extraction.Candidates[j].Code
		}
		return extraction.Candidates[i].CandidateType < extraction.Candidates[j].CandidateType
	})
	if len(extraction.Candidates) == 0 {
		return nil, ErrDocumentExtractionInvalid
	}
	if !hasClosedCandidateCodeSetReferences(extraction.Candidates) {
		return nil, ErrDocumentExtractionInvalid
	}
	return extraction, nil
}

func validCopilotCandidateDataType(candidateType string, value *string) bool {
	if value == nil {
		return true
	}
	dataType := strings.TrimSpace(*value)
	switch candidateType {
	case "element":
		switch dataType {
		case "string", "int", "bigint", "float", "decimal", "date", "datetime", "bool", "json", "text":
			return true
		}
	case "code_set":
		switch dataType {
		case "string", "int", "bigint":
			return true
		}
	}
	return false
}

func validCopilotCandidateValueDomainKind(candidateType string, value *string) bool {
	if value == nil {
		return true
	}
	if candidateType != "element" {
		return false
	}
	switch strings.TrimSpace(*value) {
	case models.ValueDomainUnrestricted, models.ValueDomainRange, models.ValueDomainEnumeration:
		return true
	default:
		return false
	}
}

func validCopilotCandidateCodeSetCode(candidateType string, valueDomainKind, codeSetCode *string) bool {
	isEnumeration := candidateType == "element" && valueDomainKind != nil && strings.TrimSpace(*valueDomainKind) == models.ValueDomainEnumeration
	if !isEnumeration {
		return codeSetCode == nil
	}
	if codeSetCode == nil {
		return false
	}
	code := strings.TrimSpace(*codeSetCode)
	return len(code) <= 100 && documentCandidateCodePattern.MatchString(code)
}

func hasClosedCandidateCodeSetReferences(candidates []models.DocumentExtractionCandidate) bool {
	codeSetCounts := map[string]int{}
	for _, candidate := range candidates {
		if candidate.CandidateType == "code_set" {
			codeSetCounts[candidate.Code]++
		}
	}
	for _, candidate := range candidates {
		if candidate.Payload.CodeSetCode != nil && codeSetCounts[*candidate.Payload.CodeSetCode] != 1 {
			return false
		}
	}
	return true
}

func normalizeCopilotCandidatePayload(raw copilotCandidatePayload) models.DocumentExtractionCandidatePayload {
	payload := models.DocumentExtractionCandidatePayload{
		DataType:           normalizedCandidateString(raw.DataType),
		ValueDomainKind:    normalizedCandidateString(raw.ValueDomainKind),
		CodeSetCode:        normalizedCandidateString(raw.CodeSetCode),
		Unit:               normalizedCandidateString(raw.Unit),
		CalculationFormula: normalizedCandidateString(raw.CalculationFormula),
		StatisticalScope:   normalizedCandidateString(raw.StatisticalScope),
		Aggregation:        normalizedCandidateString(raw.Aggregation),
	}
	if len(raw.Dimensions) > 0 {
		payload.Dimensions = append([]string(nil), raw.Dimensions...)
	}
	if len(raw.Items) > 0 {
		payload.Items = make([]models.DocumentExtractionCandidatePayloadItem, 0, len(raw.Items))
		for _, item := range raw.Items {
			payload.Items = append(payload.Items, models.DocumentExtractionCandidatePayloadItem{Code: item.Code, Name: item.Name, Definition: item.Definition})
		}
	}
	return payload
}

func normalizedCandidateString(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func isMarkdownRevision(revision *models.DocumentRevision) bool {
	return revision != nil && (strings.EqualFold(revision.MediaType, "text/markdown") || strings.EqualFold(filepath.Ext(revision.FileName), ".md"))
}
func (s *DocumentService) attachCandidateComparisons(document *models.Document, extractions []models.DocumentExtraction) error {
	codesByType := map[string][]string{}
	for extractionIndex := range extractions {
		for candidateIndex := range extractions[extractionIndex].Candidates {
			candidate := &extractions[extractionIndex].Candidates[candidateIndex]
			codesByType[candidate.CandidateType] = append(codesByType[candidate.CandidateType], candidate.Code)
		}
	}
	targets, err := s.repo.ListCandidateComparisonTargets(document.TenantID, codesByType)
	if err != nil {
		return err
	}
	for extractionIndex := range extractions {
		for candidateIndex := range extractions[extractionIndex].Candidates {
			candidate := &extractions[extractionIndex].Candidates[candidateIndex]
			target, ok := targets[candidateComparisonKey(candidate.CandidateType, candidate.Code)]
			candidate.Comparison = compareDocumentCandidate(document, candidate, target, ok)
		}
	}
	return nil
}

func compareDocumentCandidate(document *models.Document, candidate *models.DocumentExtractionCandidate, target repository.DocumentCandidateComparisonTarget, matched bool) *models.DocumentExtractionCandidateComparison {
	comparison := &models.DocumentExtractionCandidateComparison{Result: models.CandidateComparisonNew, Differences: []models.DocumentExtractionCandidateDifference{}}
	if !matched {
		return comparison
	}
	comparison.StandardID = target.StandardID
	comparison.Code = target.Code
	comparison.Name = target.Name
	comparison.ScopeType = target.ScopeType
	comparison.OwnerDomainID = target.OwnerDomainID
	comparison.RevisionID = target.RevisionID
	comparison.RevisionNo = target.RevisionNo
	comparison.RevisionStatus = target.RevisionStatus

	scopeMatches := document.ScopeType == target.ScopeType && equalOptionalInt64(document.OwnerDomainID, target.OwnerDomainID)
	if document.ScopeType != target.ScopeType {
		appendCandidateDifference(comparison, "scope_type", candidateTextComparisonValue(document.ScopeType), candidateTextComparisonValue(target.ScopeType))
	}
	if !equalOptionalInt64(document.OwnerDomainID, target.OwnerDomainID) {
		appendCandidateDifference(comparison, "owner_domain_id", candidateIntegerComparisonValue(document.OwnerDomainID), candidateIntegerComparisonValue(target.OwnerDomainID))
	}
	if !equalCandidateText(candidate.Name, target.Name) {
		appendCandidateDifference(comparison, "name", candidateTextComparisonValue(candidate.Name), candidateTextComparisonValue(target.Name))
	}
	if !equalCandidateText(candidate.Definition, target.Definition) {
		appendCandidateDifference(comparison, "definition", candidateTextComparisonValue(candidate.Definition), candidateTextComparisonValue(target.Definition))
	}
	appendPayloadDifference := func(candidateValue *string, field, targetValue string) {
		if candidateValue != nil && !equalCandidateText(*candidateValue, targetValue) {
			appendCandidateDifference(comparison, field, candidateTextComparisonValue(*candidateValue), candidateTextComparisonValue(targetValue))
		}
	}
	switch candidate.CandidateType {
	case "element":
		appendPayloadDifference(candidate.Payload.DataType, "data_type", target.DataType)
		appendPayloadDifference(candidate.Payload.ValueDomainKind, "value_domain_kind", target.ValueDomainKind)
		appendPayloadDifference(candidate.Payload.CodeSetCode, "code_set_code", target.CodeSetCode)
		if candidate.Payload.Unit != nil && !matchesCandidateUnit(*candidate.Payload.Unit, target.UnitName, target.UnitSymbol) {
			appendCandidateDifference(comparison, "unit", candidateTextComparisonValue(*candidate.Payload.Unit), candidateTextComparisonValue(comparisonTargetUnit(target.UnitName, target.UnitSymbol)))
		}
	case "code_set":
		appendPayloadDifference(candidate.Payload.DataType, "value_type", target.DataType)
		if len(candidate.Payload.Items) > 0 && !equalCandidateItems(candidate.Payload.Items, target.Items) {
			appendCandidateDifference(comparison, "items", candidateItemsComparisonValue(candidate.Payload.Items), models.DocumentExtractionCandidateComparisonValue{Kind: "code_items", Items: target.Items})
		}
	case "metric":
		appendPayloadDifference(candidate.Payload.StatisticalScope, "statistical_caliber", target.StatisticalCaliber)
		appendPayloadDifference(candidate.Payload.CalculationFormula, "semantic_formula", target.SemanticFormula)
		if candidate.Payload.Unit != nil && !matchesCandidateUnit(*candidate.Payload.Unit, target.UnitName, target.UnitSymbol) {
			appendCandidateDifference(comparison, "unit", candidateTextComparisonValue(*candidate.Payload.Unit), candidateTextComparisonValue(comparisonTargetUnit(target.UnitName, target.UnitSymbol)))
		}
	}
	if !scopeMatches {
		comparison.Result = models.CandidateComparisonScopeConflict
	} else if len(comparison.Differences) > 0 {
		comparison.Result = models.CandidateComparisonContentConflict
	} else {
		comparison.Result = models.CandidateComparisonExact
	}
	return comparison
}

func appendCandidateDifference(comparison *models.DocumentExtractionCandidateComparison, field string, candidateValue, standardValue models.DocumentExtractionCandidateComparisonValue) {
	comparison.Differences = append(comparison.Differences, models.DocumentExtractionCandidateDifference{Field: field, CandidateValue: candidateValue, StandardValue: standardValue})
}

func candidateTextComparisonValue(value string) models.DocumentExtractionCandidateComparisonValue {
	value = strings.TrimSpace(value)
	if value == "" {
		return models.DocumentExtractionCandidateComparisonValue{Kind: "empty"}
	}
	return models.DocumentExtractionCandidateComparisonValue{Kind: "text", Text: &value}
}

func candidateIntegerComparisonValue(value *int64) models.DocumentExtractionCandidateComparisonValue {
	if value == nil {
		return models.DocumentExtractionCandidateComparisonValue{Kind: "empty"}
	}
	return models.DocumentExtractionCandidateComparisonValue{Kind: "integer", Integer: value}
}

func candidateItemsComparisonValue(items []models.DocumentExtractionCandidatePayloadItem) models.DocumentExtractionCandidateComparisonValue {
	values := make([]models.DocumentExtractionCandidateComparisonItem, 0, len(items))
	for _, item := range items {
		values = append(values, models.DocumentExtractionCandidateComparisonItem{Code: item.Code, Name: item.Name, Definition: item.Definition})
	}
	return models.DocumentExtractionCandidateComparisonValue{Kind: "code_items", Items: values}
}

func comparisonTargetUnit(name, symbol string) string {
	name, symbol = strings.TrimSpace(name), strings.TrimSpace(symbol)
	if name == "" {
		return symbol
	}
	if symbol == "" || equalCandidateText(name, symbol) {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, symbol)
}

func equalCandidateItems(candidateItems []models.DocumentExtractionCandidatePayloadItem, targetItems []models.DocumentExtractionCandidateComparisonItem) bool {
	if len(candidateItems) != len(targetItems) {
		return false
	}
	targetByCode := make(map[string]models.DocumentExtractionCandidateComparisonItem, len(targetItems))
	for _, item := range targetItems {
		targetByCode[item.Code] = item
	}
	for _, item := range candidateItems {
		target, ok := targetByCode[item.Code]
		if !ok || !equalCandidateText(item.Name, target.Name) {
			return false
		}
		if strings.TrimSpace(item.Definition) != "" && !equalCandidateText(item.Definition, target.Definition) {
			return false
		}
	}
	return true
}

func matchesCandidateUnit(candidate, name, symbol string) bool {
	return equalCandidateText(candidate, name) || (strings.TrimSpace(symbol) != "" && equalCandidateText(candidate, symbol))
}

func equalCandidateText(left, right string) bool {
	return strings.Join(strings.Fields(left), " ") == strings.Join(strings.Fields(right), " ")
}

func equalOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func candidateComparisonKey(candidateType, code string) string {
	return candidateType + "\x00" + code
}
func (s *DocumentService) UpdateCandidateStatus(candidateID, tenantID, userID int64, req *models.UpdateDocumentExtractionCandidateRequest) (*models.DocumentExtractionCandidate, error) {
	if req.Status != "retained" && req.Status != "rejected" {
		return nil, ErrDocumentExtractionInvalid
	}
	result, err := s.repo.UpdateCandidateStatus(candidateID, tenantID, userID, req.Version, req.Status)
	if err != nil {
		return nil, mapCandidateFormalizationError(err)
	}
	return result, nil
}

type CandidateFormalizationAuthorization struct {
	Create map[string]bool
	Update map[string]bool
}

func (s *DocumentService) FormalizeCandidate(candidateID, tenantID, userID int64, req *models.FormalizeDocumentExtractionCandidateRequest, authorization CandidateFormalizationAuthorization) (*models.DocumentCandidateFormalizationResponse, error) {
	changeSummary := strings.TrimSpace(req.ChangeSummary)
	metricType := strings.TrimSpace(req.MetricType)
	if changeSummary == "" {
		return nil, ErrCandidateFormalizationInvalid
	}
	context, err := s.repo.GetCandidateContext(candidateID, tenantID)
	if err != nil {
		return nil, err
	}
	candidate, document := &context.Candidate, &context.Document
	if candidate.Version != req.Version {
		return nil, ErrCandidateFormalizationStale
	}
	if candidate.Status != "retained" {
		return nil, ErrCandidateNotRetained
	}
	if candidate.Formalization != nil {
		return nil, ErrCandidateAlreadyFormalized
	}
	targets, err := s.repo.ListCandidateComparisonTargets(tenantID, map[string][]string{candidate.CandidateType: {candidate.Code}})
	if err != nil {
		return nil, err
	}
	target, matched := targets[candidateComparisonKey(candidate.CandidateType, candidate.Code)]
	comparison := compareDocumentCandidate(document, candidate, target, matched)
	plan := repository.DocumentCandidateFormalizationPlan{
		CandidateType: candidate.CandidateType, CandidateVersion: candidate.Version,
		SourceDocumentVersion: document.Version, ChangeSummary: changeSummary,
		TargetStandardID: target.StandardID, TargetStandardVersion: target.StandardVersion,
		TargetRevisionID: target.RevisionID,
	}
	switch {
	case !matched:
		plan.Action = models.CandidateFormalizationCreatedIdentity
	case comparison.Result == models.CandidateComparisonScopeConflict:
		return nil, ErrCandidateScopeConflict
	case comparison.Result == models.CandidateComparisonExact && (target.RevisionStatus == models.RevisionStatusDraft || target.RevisionStatus == models.RevisionStatusInReview || target.RevisionStatus == models.RevisionStatusPublished):
		plan.Action = models.CandidateFormalizationLinkedExisting
	case target.DraftRevisionID != nil:
		return nil, ErrCandidateTargetDraftExists
	default:
		plan.Action = models.CandidateFormalizationCreatedRevision
	}
	if plan.Action == models.CandidateFormalizationCreatedIdentity && candidate.CandidateType == "metric" {
		if metricType != "atomic" && metricType != "derived" && metricType != "composite" {
			return nil, ErrCandidateFormalizationInvalid
		}
		plan.MetricType = metricType
	} else if metricType != "" {
		return nil, ErrCandidateFormalizationInvalid
	}
	permissionSet := authorization.Update
	if plan.Action == models.CandidateFormalizationCreatedIdentity {
		permissionSet = authorization.Create
	}
	if !permissionSet[candidate.CandidateType] {
		return nil, ErrCandidateFormalizationDenied
	}
	result, err := s.repo.FormalizeCandidate(candidateID, tenantID, userID, plan)
	if err != nil {
		return nil, mapCandidateFormalizationError(err)
	}
	return result, nil
}

func mapCandidateFormalizationError(err error) error {
	switch {
	case errors.Is(err, repository.ErrCandidateNotRetained):
		return ErrCandidateNotRetained
	case errors.Is(err, repository.ErrCandidateAlreadyFormalized):
		return ErrCandidateAlreadyFormalized
	case errors.Is(err, repository.ErrCandidateFormalizationStale):
		return ErrCandidateFormalizationStale
	case errors.Is(err, repository.ErrCandidateScopeConflict):
		return ErrCandidateScopeConflict
	case errors.Is(err, repository.ErrCandidateTargetDraftExists):
		return ErrCandidateTargetDraftExists
	case errors.Is(err, repository.ErrCandidateReferenceUnavailable):
		return ErrCandidateReferenceUnavailable
	default:
		return err
	}
}

func (s *DocumentService) removeObject(key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()
	return s.objectStore.RemoveObject(ctx, minioBucket, key, minio.RemoveObjectOptions{})
}
func (s *DocumentService) runFileCleanupWorker() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.processDueFileCleanups()
		case <-s.stopCh:
			return
		}
	}
}
func (s *DocumentService) processDueFileCleanups() {
	if s.objectStore == nil {
		return
	}
	rows, err := s.repo.ListDueFileCleanups(time.Now(), 100)
	if err != nil {
		log.Printf("standard document file cleanup list failed: %v", err)
		return
	}
	for _, row := range rows {
		s.tryFileCleanup(row)
	}
}
func (s *DocumentService) tryFileCleanup(cleanup models.DocumentFileCleanup) {
	if s.objectStore == nil || cleanup.ObjectKey == "" {
		return
	}
	if err := s.removeObject(cleanup.ObjectKey); err != nil {
		attempts := cleanup.Attempts + 1
		backoff := time.Duration(1<<min(attempts, 10)) * time.Minute
		if updateErr := s.repo.FailFileCleanup(cleanup.ID, attempts, time.Now().Add(backoff), err.Error()); updateErr != nil {
			log.Printf("standard document file cleanup retry update failed, key=%q: %v", cleanup.ObjectKey, updateErr)
		}
		return
	}
	if err := s.repo.CompleteFileCleanup(cleanup.ID); err != nil {
		log.Printf("standard document file cleanup completion failed, key=%q: %v", cleanup.ObjectKey, err)
	}
}
func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
func sanitizeDocumentFileName(fileName string) (string, error) {
	fileName = strings.TrimSpace(strings.ReplaceAll(fileName, "\\", "/"))
	fileName = filepath.Base(fileName)
	if fileName == "." || fileName == ".." || fileName == "" || strings.ContainsAny(fileName, "\r\n\x00") {
		return "", ErrDocumentFileNameInvalid
	}
	return fileName, nil
}

func (s *DocumentService) GetMappings(docID, tenantID int64) (map[string]interface{}, error) {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return nil, err
	}
	elements, err := s.repo.GetElementMappings(docID, tenantID)
	if err != nil {
		return nil, err
	}
	glossaries, err := s.repo.GetGlossaryMappings(docID, tenantID)
	if err != nil {
		return nil, err
	}
	metrics, err := s.repo.GetMetricMappings(docID, tenantID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"elements": elements, "glossaries": glossaries, "metrics": metrics}, nil
}
func (s *DocumentService) SetMappings(docID, tenantID int64, req *models.SetDocumentMappingsRequest) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return err
	}
	for _, validate := range []func() error{func() error { return s.refs.RequireElements(tenantID, req.ElementIDs) }, func() error { return s.refs.RequireGlossaries(tenantID, req.GlossaryIDs) }, func() error { return s.refs.RequireMetrics(tenantID, req.MetricIDs) }} {
		if err := validate(); err != nil {
			return err
		}
	}
	locations := req.Locations
	if locations == nil {
		locations = map[string]string{}
	}
	return s.repo.SetMappings(docID, tenantID, req.Version, req.ElementIDs, req.GlossaryIDs, req.MetricIDs, locations)
}

func (s *DocumentService) ListByElement(tenantID, elementID int64) ([]models.DocumentAggregate, error) {
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return nil, err
	}
	return s.repo.ListByElementID(tenantID, elementID)
}
func (s *DocumentService) ListByGlossary(tenantID, glossaryID int64) ([]models.DocumentAggregate, error) {
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return nil, err
	}
	return s.repo.ListByGlossaryID(tenantID, glossaryID)
}
func (s *DocumentService) ListByMetric(tenantID, metricID int64) ([]models.DocumentAggregate, error) {
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return nil, err
	}
	return s.repo.ListByMetricID(tenantID, metricID)
}

func (s *DocumentService) createAndLink(req *models.CreateLinkedDocumentRequest, tenantID, userID int64, mapping interface{}, parent interface{}, parentID int64) (*models.LinkedDocumentMutationResponse, error) {
	document, revision, err := s.newDocument(&req.CreateDocumentRequest, tenantID, userID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.CreateWithMappingVersioned(document, revision, mapping, parent, parentID, tenantID, req.Version); err != nil {
		return nil, err
	}
	aggregate, err := s.repo.GetAggregate(document.ID, tenantID)
	if err != nil {
		return nil, err
	}
	return &models.LinkedDocumentMutationResponse{Document: aggregate, Version: req.Version + 1}, nil
}
func (s *DocumentService) CreateAndLinkElement(req *models.CreateLinkedDocumentRequest, tenantID, userID, elementID int64) (*models.LinkedDocumentMutationResponse, error) {
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return nil, err
	}
	return s.createAndLink(req, tenantID, userID, &models.DocumentElementMapping{ElementID: elementID}, &models.Element{}, elementID)
}
func (s *DocumentService) CreateAndLinkGlossary(req *models.CreateLinkedDocumentRequest, tenantID, userID, glossaryID int64) (*models.LinkedDocumentMutationResponse, error) {
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return nil, err
	}
	return s.createAndLink(req, tenantID, userID, &models.DocumentGlossaryMapping{GlossaryID: glossaryID}, &models.Glossary{}, glossaryID)
}
func (s *DocumentService) CreateAndLinkMetric(req *models.CreateLinkedDocumentRequest, tenantID, userID, metricID int64) (*models.LinkedDocumentMutationResponse, error) {
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return nil, err
	}
	return s.createAndLink(req, tenantID, userID, &models.DocumentMetricMapping{MetricID: metricID}, &models.MetricDefinition{}, metricID)
}

func (s *DocumentService) LinkDocToElement(docID, tenantID, elementID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return err
	}
	return s.repo.MutateMappingVersioned(&models.Element{}, elementID, tenantID, version, &models.DocumentElementMapping{DocumentID: docID, ElementID: elementID}, true, "document_id = ? AND element_id = ?", docID, elementID)
}
func (s *DocumentService) LinkDocToGlossary(docID, tenantID, glossaryID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return err
	}
	return s.repo.MutateMappingVersioned(&models.Glossary{}, glossaryID, tenantID, version, &models.DocumentGlossaryMapping{DocumentID: docID, GlossaryID: glossaryID}, true, "document_id = ? AND glossary_id = ?", docID, glossaryID)
}
func (s *DocumentService) LinkDocToMetric(docID, tenantID, metricID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return err
	}
	return s.repo.MutateMappingVersioned(&models.MetricDefinition{}, metricID, tenantID, version, &models.DocumentMetricMapping{DocumentID: docID, MetricID: metricID}, true, "document_id = ? AND metric_id = ?", docID, metricID)
}
func (s *DocumentService) UnlinkDocFromElement(docID, tenantID, elementID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireElement(tenantID, elementID); err != nil {
		return err
	}
	return s.repo.MutateMappingVersioned(&models.Element{}, elementID, tenantID, version, &models.DocumentElementMapping{}, false, "document_id = ? AND element_id = ?", docID, elementID)
}
func (s *DocumentService) UnlinkDocFromGlossary(docID, tenantID, glossaryID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireGlossary(tenantID, glossaryID); err != nil {
		return err
	}
	return s.repo.MutateMappingVersioned(&models.Glossary{}, glossaryID, tenantID, version, &models.DocumentGlossaryMapping{}, false, "document_id = ? AND glossary_id = ?", docID, glossaryID)
}
func (s *DocumentService) UnlinkDocFromMetric(docID, tenantID, metricID, version int64) error {
	if _, err := s.repo.GetByID(docID, tenantID); err != nil {
		return commonapi.ErrNotFound
	}
	if err := s.refs.RequireMetric(tenantID, &metricID); err != nil {
		return err
	}
	return s.repo.MutateMappingVersioned(&models.MetricDefinition{}, metricID, tenantID, version, &models.DocumentMetricMapping{}, false, "document_id = ? AND metric_id = ?", docID, metricID)
}
