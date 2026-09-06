package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonclient "github.com/addp/common/client"
	"github.com/addp/standard/internal/models"
	"github.com/addp/standard/internal/repository"
	minio "github.com/minio/minio-go/v7"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeDocumentObjectStore struct {
	objects     map[string][]byte
	putKeys     []string
	removedKeys []string
	putErr      error
	removeErr   error
}

func (f *fakeDocumentObjectStore) BucketExists(context.Context, string) (bool, error) {
	return true, nil
}
func (f *fakeDocumentObjectStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	return nil
}
func (f *fakeDocumentObjectStore) PutObject(_ context.Context, _ string, key string, reader io.Reader, _ int64, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	if f.putErr != nil {
		return minio.UploadInfo{}, f.putErr
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		return minio.UploadInfo{}, err
	}
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[key] = content
	f.putKeys = append(f.putKeys, key)
	return minio.UploadInfo{Key: key, Size: int64(len(content))}, nil
}
func (f *fakeDocumentObjectStore) RemoveObject(_ context.Context, _ string, key string, _ minio.RemoveObjectOptions) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	delete(f.objects, key)
	f.removedKeys = append(f.removedKeys, key)
	return nil
}
func (f *fakeDocumentObjectStore) StatObject(_ context.Context, _ string, key string, _ minio.StatObjectOptions) (minio.ObjectInfo, error) {
	content, ok := f.objects[key]
	if !ok {
		return minio.ObjectInfo{}, errors.New("object not found")
	}
	return minio.ObjectInfo{Key: key, Size: int64(len(content))}, nil
}
func (f *fakeDocumentObjectStore) GetObject(_ context.Context, _ string, key string, _ minio.GetObjectOptions) (io.ReadCloser, error) {
	content, ok := f.objects[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func TestSanitizeDocumentFileName(t *testing.T) {
	name, err := sanitizeDocumentFileName(`../nested\report.md`)
	if err != nil || name != "report.md" {
		t.Fatalf("sanitizeDocumentFileName() = %q, %v", name, err)
	}
	if _, err := sanitizeDocumentFileName("line\nbreak.md"); !errors.Is(err, ErrDocumentFileNameInvalid) {
		t.Fatalf("unsafe file name error = %v", err)
	}
}

func TestSplitMarkdownSectionsCarriesAbsoluteLineNumbers(t *testing.T) {
	sections := splitMarkdownSections("# Outdoor\n\n说明\n## 指标\n实际参加活动数")
	if len(sections) != 2 {
		t.Fatalf("sections = %#v, want 2", sections)
	}
	if sections[1].SectionPath != "Outdoor / 指标" || sections[1].StartLine != 4 || sections[1].EndLine != 5 {
		t.Fatalf("metric section metadata = %#v", sections[1])
	}
	if sections[1].Text != "L4: ## 指标\nL5: 实际参加活动数" {
		t.Fatalf("metric numbered text = %q", sections[1].Text)
	}
}

func TestUploadFileReplacesOnlyDraftRevisionObject(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	doc, revision := seedDocumentDraft(t, repo, 7, "tenant_7/documents/1/old.md")
	store := &fakeDocumentObjectStore{objects: map[string][]byte{revision.FileKey: []byte("old")}}
	svc := &DocumentService{repo: repo, objectStore: store, maxFileSize: 10, timeout: time.Second}
	if _, err := svc.UploadFile(doc.ID, revision.ID, doc.TenantID, 1, doc.Version, "new.md", strings.NewReader("new content"), 11, "text/markdown"); !errors.Is(err, ErrDocumentFileTooLarge) {
		t.Fatalf("oversized upload error = %v", err)
	}
	updated, err := svc.UploadFile(doc.ID, revision.ID, doc.TenantID, 1, doc.Version, "new.md", strings.NewReader("new"), 3, "text/markdown")
	if err != nil {
		t.Fatalf("replacement upload: %v", err)
	}
	if updated.DraftRevision == nil || updated.DraftRevision.FileName != "new.md" || updated.DraftRevision.FileKey == revision.FileKey || updated.DraftRevision.ContentSHA256 == "" {
		t.Fatalf("updated aggregate = %+v", updated)
	}
	if len(store.removedKeys) != 1 || store.removedKeys[0] != revision.FileKey {
		t.Fatalf("removed keys = %v", store.removedKeys)
	}
}

func TestExtractCandidatesPersistsCanonicalOutdoorEvidence(t *testing.T) {
	var gotAuthorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"candidate_type":"metric","code":"outdoor_participation_count","name":"实际参加活动数","definition":"人员实际参加的有效活动去重数","payload":{"aggregation":"count"},"evidences":[{"section_path":"Outdoor / 指标","start_line":3,"end_line":4}]},{"candidate_type":"glossary","code":"wrong_section","name":"错误章节","definition":"证据路径与行号不匹配的候选必须被丢弃","payload":{},"evidences":[{"section_path":"Outdoor","start_line":3,"end_line":4}]}]}`))
	}))
	defer server.Close()
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	doc, revision := seedDocumentDraft(t, repo, 8, "outdoor.md")
	if err := db.Exec(`INSERT INTO standard.metric_definitions (id, tenant_id, scope_type, code, draft_revision_id, lifecycle_state) VALUES (41, 8, 'tenant_common', 'outdoor_participation_count', 51, 'active')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO standard.metric_definition_revisions (id, metric_definition_id, revision_no, status, name, definition, statistical_caliber, semantic_formula) VALUES (51, 41, 1, 'draft', '实际参加活动数', '人员实际参加的有效活动去重数', '', '')`).Error; err != nil {
		t.Fatal(err)
	}
	content := "# Outdoor\n\n## 指标\n实际参加活动数只统计有效活动。\n"
	store := &fakeDocumentObjectStore{objects: map[string][]byte{revision.FileKey: []byte(content)}}
	svc := &DocumentService{repo: repo, objectStore: store, maxFileSize: 1024, timeout: time.Second, copilotURL: server.URL, serviceTokenSource: commonclient.ServiceTokenProviderFunc(func(_ context.Context, tenantID uint) (string, error) {
		if tenantID != 8 {
			t.Fatalf("tenantID=%d", tenantID)
		}
		return "service-token", nil
	}), httpClient: server.Client()}
	extraction, err := svc.ExtractCandidates(context.Background(), doc.ID, revision.ID, doc.TenantID, 9, doc.Version)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if gotAuthorization != "Bearer service-token" {
		t.Fatalf("Authorization=%q", gotAuthorization)
	}
	if len(extraction.Candidates) != 1 || len(extraction.Candidates[0].Evidences) != 1 {
		t.Fatalf("extraction=%+v", extraction)
	}
	if extraction.Candidates[0].Payload.Aggregation == nil || *extraction.Candidates[0].Payload.Aggregation != "count" {
		t.Fatalf("candidate payload=%+v", extraction.Candidates[0].Payload)
	}
	if comparison := extraction.Candidates[0].Comparison; comparison == nil || comparison.Result != models.CandidateComparisonExact || comparison.StandardID != 41 || comparison.RevisionID != 51 {
		t.Fatalf("candidate comparison=%+v", comparison)
	}
	evidence := extraction.Candidates[0].Evidences[0]
	if evidence.Excerpt != "## 指标\n实际参加活动数只统计有效活动。" || evidence.ExcerptHash == "" {
		t.Fatalf("evidence=%+v", evidence)
	}
	loaded, err := repo.ListExtractions(doc.ID, doc.TenantID)
	if err != nil || len(loaded) != 1 || len(loaded[0].Candidates) != 1 || loaded[0].Candidates[0].Payload.Aggregation == nil || *loaded[0].Candidates[0].Payload.Aggregation != "count" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	listed, err := svc.ListCandidateGroups(doc.ID, doc.TenantID, DocumentCandidateGroupListOptions{})
	if err != nil || len(listed.Data) != 1 || listed.Data[0].Candidate.Comparison == nil || listed.Data[0].Candidate.Comparison.Result != models.CandidateComparisonExact {
		t.Fatalf("listed comparison=%+v err=%v", listed, err)
	}
	updated, err := repo.GetByID(doc.ID, doc.TenantID)
	if err != nil || updated.Version != 2 {
		t.Fatalf("document version=%+v err=%v", updated, err)
	}
}

func TestCompareDocumentCandidateClassifiesDeterministicMatches(t *testing.T) {
	domainID, otherDomainID := int64(2), int64(3)
	document := &models.Document{ScopeType: models.StandardScopeDomain, OwnerDomainID: &domainID}
	target := repository.DocumentCandidateComparisonTarget{
		CandidateType: "glossary", StandardID: 21, Code: "outdoor_activity", ScopeType: models.StandardScopeDomain,
		OwnerDomainID: &domainID, RevisionID: 31, RevisionNo: 2, RevisionStatus: models.RevisionStatusInReview,
		Name: "户外活动", Definition: "在户外 开展的活动",
	}

	tests := []struct {
		name      string
		candidate models.DocumentExtractionCandidate
		target    repository.DocumentCandidateComparisonTarget
		matched   bool
		want      string
		wantDiff  string
	}{
		{name: "new", candidate: models.DocumentExtractionCandidate{CandidateType: "glossary", Code: "new", Name: "新术语", Definition: "新定义"}, matched: false, want: models.CandidateComparisonNew},
		{name: "exact normalizes whitespace", candidate: models.DocumentExtractionCandidate{CandidateType: "glossary", Code: target.Code, Name: " 户外活动 ", Definition: "在户外  \n 开展的活动"}, target: target, matched: true, want: models.CandidateComparisonExact},
		{name: "content conflict", candidate: models.DocumentExtractionCandidate{CandidateType: "glossary", Code: target.Code, Name: target.Name, Definition: "另一业务定义"}, target: target, matched: true, want: models.CandidateComparisonContentConflict, wantDiff: "definition"},
		{name: "scope conflict takes precedence", candidate: models.DocumentExtractionCandidate{CandidateType: "glossary", Code: target.Code, Name: target.Name, Definition: target.Definition}, target: func() repository.DocumentCandidateComparisonTarget {
			changed := target
			changed.OwnerDomainID = &otherDomainID
			return changed
		}(), matched: true, want: models.CandidateComparisonScopeConflict, wantDiff: "owner_domain_id"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := test.candidate
			comparison := compareDocumentCandidate(document, &candidate, test.target, test.matched)
			if comparison.Result != test.want {
				t.Fatalf("result=%q comparison=%+v", comparison.Result, comparison)
			}
			if test.wantDiff != "" {
				difference, ok := findCandidateDifference(comparison.Differences, test.wantDiff)
				if !ok {
					t.Fatalf("differences=%+v, want field %q", comparison.Differences, test.wantDiff)
				}
				if difference.CandidateValue.Kind == "" || difference.StandardValue.Kind == "" {
					t.Fatalf("difference values must be explicit: %+v", difference)
				}
			}
		})
	}
}

func TestBuildDocumentExtractionRejectsNonstandardValueDomainKind(t *testing.T) {
	invalidKind := "identifier"
	content := "# 数据元\n人员标识是户外参与人员的稳定标识。"
	sections := []documentExtractionSection{{SectionPath: "数据元", StartLine: 1, EndLine: 2, Text: content}}
	response := &copilotDocumentExtractResponse{Candidates: []copilotCandidate{{
		CandidateType: "element",
		Code:          "outdoor_person_id",
		Name:          "人员标识",
		Definition:    "户外参与人员的稳定标识。",
		Payload:       copilotCandidatePayload{ValueDomainKind: &invalidKind},
		Evidences:     []copilotEvidence{{SectionPath: "数据元", StartLine: 1, EndLine: 2}},
	}}}

	if _, err := buildDocumentExtraction(1, 2, 3, content, sections, response); !errors.Is(err, ErrDocumentExtractionInvalid) {
		t.Fatalf("err=%v, want ErrDocumentExtractionInvalid", err)
	}
}

func TestBuildDocumentExtractionRejectsValueDomainKindOnNonElementCandidate(t *testing.T) {
	kind := models.ValueDomainUnrestricted
	content := "# 指标\n活动次数表示人员参加活动的次数。"
	sections := []documentExtractionSection{{SectionPath: "指标", StartLine: 1, EndLine: 2, Text: content}}
	response := &copilotDocumentExtractResponse{Candidates: []copilotCandidate{{
		CandidateType: "metric",
		Code:          "outdoor_activity_count",
		Name:          "活动次数",
		Definition:    "人员参加活动的次数。",
		Payload:       copilotCandidatePayload{ValueDomainKind: &kind},
		Evidences:     []copilotEvidence{{SectionPath: "指标", StartLine: 1, EndLine: 2}},
	}}}

	if _, err := buildDocumentExtraction(1, 2, 3, content, sections, response); !errors.Is(err, ErrDocumentExtractionInvalid) {
		t.Fatalf("err=%v, want ErrDocumentExtractionInvalid", err)
	}
}

func TestBuildDocumentExtractionRejectsInvalidCandidateDataType(t *testing.T) {
	tests := []struct {
		name          string
		candidateType string
		dataType      string
	}{
		{name: "noncanonical element type", candidateType: "element", dataType: "date_or_datetime"},
		{name: "unsupported code set type", candidateType: "code_set", dataType: "decimal"},
		{name: "metric cannot own type", candidateType: "metric", dataType: "integer"},
		{name: "glossary cannot own type", candidateType: "glossary", dataType: "string"},
	}
	content := "# 候选\n户外业务候选定义。"
	sections := []documentExtractionSection{{SectionPath: "候选", StartLine: 1, EndLine: 2, Text: content}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := &copilotDocumentExtractResponse{Candidates: []copilotCandidate{{
				CandidateType: test.candidateType,
				Code:          "outdoor_candidate",
				Name:          "户外候选",
				Definition:    "户外业务候选定义。",
				Payload:       copilotCandidatePayload{DataType: &test.dataType},
				Evidences:     []copilotEvidence{{SectionPath: "候选", StartLine: 1, EndLine: 2}},
			}}}

			if _, err := buildDocumentExtraction(1, 2, 3, content, sections, response); !errors.Is(err, ErrDocumentExtractionInvalid) {
				t.Fatalf("err=%v, want ErrDocumentExtractionInvalid", err)
			}
		})
	}
}

func TestBuildDocumentExtractionAcceptsTypeSpecificCanonicalDataType(t *testing.T) {
	tests := []struct {
		candidateType string
		dataType      string
	}{
		{candidateType: "element", dataType: "decimal"},
		{candidateType: "code_set", dataType: "bigint"},
	}
	content := "# 候选\n户外业务候选定义。"
	sections := []documentExtractionSection{{SectionPath: "候选", StartLine: 1, EndLine: 2, Text: content}}
	for _, test := range tests {
		t.Run(test.candidateType, func(t *testing.T) {
			response := &copilotDocumentExtractResponse{Candidates: []copilotCandidate{{
				CandidateType: test.candidateType,
				Code:          "outdoor_candidate",
				Name:          "户外候选",
				Definition:    "户外业务候选定义。",
				Payload:       copilotCandidatePayload{DataType: &test.dataType},
				Evidences:     []copilotEvidence{{SectionPath: "候选", StartLine: 1, EndLine: 2}},
			}}}

			extraction, err := buildDocumentExtraction(1, 2, 3, content, sections, response)
			if err != nil || len(extraction.Candidates) != 1 {
				t.Fatalf("extraction=%+v err=%v", extraction, err)
			}
		})
	}
}

func TestBuildDocumentExtractionRequiresClosedEnumerationCodeSetReference(t *testing.T) {
	enumeration, unrestricted := models.ValueDomainEnumeration, models.ValueDomainUnrestricted
	codeSetCode, missingCodeSetCode := "outdoor_activity_status_codes", "missing_status_codes"
	invalidCodeSetCode := "Outdoor Status"
	content := "# 候选\n户外活动状态及其码值。"
	sections := []documentExtractionSection{{SectionPath: "候选", StartLine: 1, EndLine: 2, Text: content}}
	evidence := []copilotEvidence{{SectionPath: "候选", StartLine: 1, EndLine: 2}}

	tests := []struct {
		name       string
		candidates []copilotCandidate
		wantErr    bool
	}{
		{
			name: "enumeration without code set code",
			candidates: []copilotCandidate{{
				CandidateType: "element", Code: "outdoor_activity_status", Name: "活动状态", Definition: "户外活动状态。",
				Payload: copilotCandidatePayload{ValueDomainKind: &enumeration}, Evidences: evidence,
			}},
			wantErr: true,
		},
		{
			name: "non enumeration with code set code",
			candidates: []copilotCandidate{{
				CandidateType: "element", Code: "outdoor_activity_status", Name: "活动状态", Definition: "户外活动状态。",
				Payload: copilotCandidatePayload{ValueDomainKind: &unrestricted, CodeSetCode: &codeSetCode}, Evidences: evidence,
			}},
			wantErr: true,
		},
		{
			name: "non element with code set code",
			candidates: []copilotCandidate{{
				CandidateType: "metric", Code: "outdoor_activity_count", Name: "活动次数", Definition: "户外活动次数。",
				Payload: copilotCandidatePayload{CodeSetCode: &codeSetCode}, Evidences: evidence,
			}},
			wantErr: true,
		},
		{
			name: "enumeration with invalid code set code",
			candidates: []copilotCandidate{{
				CandidateType: "element", Code: "outdoor_activity_status", Name: "活动状态", Definition: "户外活动状态。",
				Payload: copilotCandidatePayload{ValueDomainKind: &enumeration, CodeSetCode: &invalidCodeSetCode}, Evidences: evidence,
			}},
			wantErr: true,
		},
		{
			name: "enumeration references absent code set candidate",
			candidates: []copilotCandidate{{
				CandidateType: "element", Code: "outdoor_activity_status", Name: "活动状态", Definition: "户外活动状态。",
				Payload: copilotCandidatePayload{ValueDomainKind: &enumeration, CodeSetCode: &missingCodeSetCode}, Evidences: evidence,
			}},
			wantErr: true,
		},
		{
			name: "enumeration references same batch code set candidate",
			candidates: []copilotCandidate{
				{
					CandidateType: "element", Code: "outdoor_activity_status", Name: "活动状态", Definition: "户外活动状态。",
					Payload: copilotCandidatePayload{ValueDomainKind: &enumeration, CodeSetCode: &codeSetCode}, Evidences: evidence,
				},
				{
					CandidateType: "code_set", Code: codeSetCode, Name: "活动状态码值集", Definition: "户外活动允许使用的状态。",
					Evidences: evidence,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			extraction, err := buildDocumentExtraction(1, 2, 3, content, sections, &copilotDocumentExtractResponse{Candidates: test.candidates})
			if test.wantErr {
				if !errors.Is(err, ErrDocumentExtractionInvalid) {
					t.Fatalf("err=%v, want ErrDocumentExtractionInvalid", err)
				}
				return
			}
			if err != nil || len(extraction.Candidates) != 2 || extraction.Candidates[1].Payload.CodeSetCode == nil || *extraction.Candidates[1].Payload.CodeSetCode != codeSetCode {
				t.Fatalf("extraction=%+v err=%v", extraction, err)
			}
		})
	}
}

func TestCompareDocumentCandidateUsesOnlyStandardOwnedAssertedFields(t *testing.T) {
	document := &models.Document{ScopeType: models.StandardScopeTenantCommon}
	target := repository.DocumentCandidateComparisonTarget{
		CandidateType: "metric", StandardID: 41, Code: "outdoor_participation_count", ScopeType: models.StandardScopeTenantCommon,
		RevisionID: 51, RevisionNo: 1, RevisionStatus: models.RevisionStatusDraft, Name: "实际参加活动数", Definition: "人员实际参加的有效活动去重数",
		StatisticalCaliber: "只统计有效活动", SemanticFormula: "count(distinct activity_id)", UnitName: "次", UnitSymbol: "次",
	}
	candidate := models.DocumentExtractionCandidate{
		CandidateType: "metric", Code: target.Code, Name: target.Name, Definition: target.Definition,
		Payload: models.DocumentExtractionCandidatePayload{Aggregation: stringPointer("count_distinct"), Dimensions: []string{"member_id"}},
	}
	comparison := compareDocumentCandidate(document, &candidate, target, true)
	if comparison.Result != models.CandidateComparisonExact || len(comparison.Differences) != 0 {
		t.Fatalf("unasserted and execution fields must not differ: %+v", comparison)
	}
	candidate.Payload.StatisticalScope = stringPointer("统计所有活动")
	comparison = compareDocumentCandidate(document, &candidate, target, true)
	if comparison.Result != models.CandidateComparisonContentConflict {
		t.Fatalf("asserted standard field mismatch=%+v", comparison)
	}
	difference, ok := findCandidateDifference(comparison.Differences, "statistical_caliber")
	if !ok || difference.CandidateValue.Text == nil || *difference.CandidateValue.Text != "统计所有活动" || difference.StandardValue.Text == nil || *difference.StandardValue.Text != target.StatisticalCaliber {
		t.Fatalf("asserted field values=%+v", difference)
	}
}

func TestCompareDocumentCandidateComparesEnumerationCodeSetCode(t *testing.T) {
	document := &models.Document{ScopeType: models.StandardScopeTenantCommon}
	target := repository.DocumentCandidateComparisonTarget{
		CandidateType: "element", StandardID: 41, Code: "outdoor_activity_status", ScopeType: models.StandardScopeTenantCommon,
		RevisionID: 51, RevisionNo: 2, RevisionStatus: models.RevisionStatusPublished, Name: "活动状态", Definition: "户外活动状态。",
		DataType: "string", ValueDomainKind: models.ValueDomainEnumeration, CodeSetCode: "outdoor_activity_status_codes",
	}
	candidateCode := "other_activity_status_codes"
	candidate := models.DocumentExtractionCandidate{
		CandidateType: "element", Code: target.Code, Name: target.Name, Definition: target.Definition,
		Payload: models.DocumentExtractionCandidatePayload{DataType: stringPointer("string"), ValueDomainKind: stringPointer(models.ValueDomainEnumeration), CodeSetCode: &candidateCode},
	}

	comparison := compareDocumentCandidate(document, &candidate, target, true)
	difference, ok := findCandidateDifference(comparison.Differences, "code_set_code")
	if comparison.Result != models.CandidateComparisonContentConflict || !ok || difference.CandidateValue.Text == nil || *difference.CandidateValue.Text != candidateCode || difference.StandardValue.Text == nil || *difference.StandardValue.Text != target.CodeSetCode {
		t.Fatalf("comparison=%+v difference=%+v", comparison, difference)
	}
}

func stringPointer(value string) *string { return &value }

func TestCompareDocumentCandidateComparesCodeItemsByCode(t *testing.T) {
	document := &models.Document{ScopeType: models.StandardScopeTenantCommon}
	target := repository.DocumentCandidateComparisonTarget{
		CandidateType: "code_set", StandardID: 61, Code: "outdoor_status", ScopeType: models.StandardScopeTenantCommon,
		RevisionID: 71, RevisionNo: 1, RevisionStatus: models.RevisionStatusPublished, Name: "活动状态", Definition: "户外活动状态", DataType: "string",
		Items: []models.DocumentExtractionCandidateComparisonItem{{Code: "closed", Name: "已结束"}, {Code: "open", Name: "进行中"}},
	}
	candidate := models.DocumentExtractionCandidate{
		CandidateType: "code_set", Code: target.Code, Name: target.Name, Definition: target.Definition,
		Payload: models.DocumentExtractionCandidatePayload{DataType: stringPointer("string"), Items: []models.DocumentExtractionCandidatePayloadItem{{Code: "open", Name: "进行中"}, {Code: "closed", Name: "已结束"}}},
	}
	comparison := compareDocumentCandidate(document, &candidate, target, true)
	if comparison.Result != models.CandidateComparisonExact {
		t.Fatalf("same items in different order=%+v", comparison)
	}
}

func TestCompareDocumentCandidateReturnsStructuredDifferenceValues(t *testing.T) {
	domainID, otherDomainID := int64(2), int64(3)
	document := &models.Document{ScopeType: models.StandardScopeDomain, OwnerDomainID: &domainID}
	target := repository.DocumentCandidateComparisonTarget{
		CandidateType: "code_set", StandardID: 61, Code: "outdoor_status", ScopeType: models.StandardScopeDomain, OwnerDomainID: &otherDomainID,
		RevisionID: 71, RevisionNo: 1, RevisionStatus: models.RevisionStatusPublished, Name: "活动状态", Definition: "现行户外活动状态", DataType: "string",
		Items: []models.DocumentExtractionCandidateComparisonItem{{Code: "open", Name: "进行中", Definition: "活动正在进行"}},
	}
	candidate := models.DocumentExtractionCandidate{
		CandidateType: "code_set", Code: target.Code, Name: target.Name, Definition: "候选活动状态",
		Payload: models.DocumentExtractionCandidatePayload{DataType: stringPointer("integer"), Items: []models.DocumentExtractionCandidatePayloadItem{{Code: "draft", Name: "拟定中", Definition: "活动尚未发布"}}},
	}

	comparison := compareDocumentCandidate(document, &candidate, target, true)
	if comparison.Result != models.CandidateComparisonScopeConflict {
		t.Fatalf("comparison=%+v", comparison)
	}
	ownerDifference, ok := findCandidateDifference(comparison.Differences, "owner_domain_id")
	if !ok || ownerDifference.CandidateValue.Integer == nil || *ownerDifference.CandidateValue.Integer != domainID || ownerDifference.StandardValue.Integer == nil || *ownerDifference.StandardValue.Integer != otherDomainID {
		t.Fatalf("owner difference=%+v", ownerDifference)
	}
	definitionDifference, ok := findCandidateDifference(comparison.Differences, "definition")
	if !ok || definitionDifference.CandidateValue.Text == nil || *definitionDifference.CandidateValue.Text != candidate.Definition || definitionDifference.StandardValue.Text == nil || *definitionDifference.StandardValue.Text != target.Definition {
		t.Fatalf("definition difference=%+v", definitionDifference)
	}
	itemsDifference, ok := findCandidateDifference(comparison.Differences, "items")
	if !ok || len(itemsDifference.CandidateValue.Items) != 1 || itemsDifference.CandidateValue.Items[0].Code != "draft" || len(itemsDifference.StandardValue.Items) != 1 || itemsDifference.StandardValue.Items[0].Code != "open" {
		t.Fatalf("items difference=%+v", itemsDifference)
	}
}

func findCandidateDifference(values []models.DocumentExtractionCandidateDifference, field string) (models.DocumentExtractionCandidateDifference, bool) {
	for _, value := range values {
		if value.Field == field {
			return value, true
		}
	}
	return models.DocumentExtractionCandidateDifference{}, false
}

func TestFileCleanupFailureStaysQueuedForRetry(t *testing.T) {
	db := openDocumentServiceTestDB(t)
	repo := repository.NewDocumentRepository(db)
	cleanup, err := repo.EnqueueFileCleanup("stale.md")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeDocumentObjectStore{objects: map[string][]byte{"stale.md": []byte("stale")}, removeErr: errors.New("minio unavailable")}
	svc := &DocumentService{repo: repo, objectStore: store, timeout: time.Second}
	svc.tryFileCleanup(*cleanup)
	var queued models.DocumentFileCleanup
	if err := db.First(&queued, cleanup.ID).Error; err != nil {
		t.Fatal(err)
	}
	if queued.Attempts != 1 || queued.LastError == "" {
		t.Fatalf("queued=%+v", queued)
	}
	store.removeErr = nil
	svc.tryFileCleanup(queued)
	if err := db.First(&queued, cleanup.ID).Error; !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("completed cleanup error=%v", err)
	}
}

func seedDocumentDraft(t *testing.T, repo *repository.DocumentRepository, tenantID int64, fileKey string) (*models.Document, *models.DocumentRevision) {
	t.Helper()
	doc := &models.Document{TenantID: tenantID, ScopeType: models.StandardScopeTenantCommon, Code: "outdoor_" + time.Now().Format("150405.000000000"), DocType: "internal", CreatedBy: 1, LifecycleState: "active"}
	revision := &models.DocumentRevision{Name: "Outdoor", ChangeSummary: "initial", CreatedBy: 1, FileKey: fileKey, FileName: "outdoor.md", FileSize: 7, MediaType: "text/markdown", ContentSHA256: "fixture"}
	if err := repo.Create(doc, revision); err != nil {
		t.Fatalf("create document: %v", err)
	}
	return doc, revision
}

func openDocumentServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ATTACH DATABASE ':memory:' AS standard").Error; err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE standard.documents (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, doc_type TEXT NOT NULL, source_org TEXT, steward_id INTEGER, tags TEXT, draft_revision_id INTEGER, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX standard.uq_test_documents_tenant_code ON documents (tenant_id, code)`,
		`CREATE TABLE standard.document_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, document_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, version_label TEXT, publish_date DATETIME, description TEXT, file_key TEXT, file_name TEXT, file_size INTEGER, media_type TEXT, content_sha256 TEXT, change_summary TEXT NOT NULL, effective_from DATETIME, effective_to DATETIME, submitted_by INTEGER, submitted_at DATETIME, published_by INTEGER, published_at DATETIME, created_by INTEGER NOT NULL, updated_by INTEGER, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.document_extractions (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, document_revision_id INTEGER NOT NULL, status TEXT NOT NULL, requested_by INTEGER NOT NULL, created_at DATETIME)`,
		`CREATE TABLE standard.document_extraction_candidates (id INTEGER PRIMARY KEY AUTOINCREMENT, extraction_id INTEGER NOT NULL, candidate_type TEXT NOT NULL, code TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, payload TEXT, status TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, reviewed_by INTEGER, reviewed_at DATETIME, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.document_candidate_formalizations (candidate_id INTEGER PRIMARY KEY, action TEXT NOT NULL, standard_id INTEGER NOT NULL, standard_code TEXT NOT NULL, revision_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, target_revision_status TEXT NOT NULL, change_summary TEXT NOT NULL, created_by INTEGER NOT NULL, created_at DATETIME)`,
		`CREATE TABLE standard.document_extraction_evidences (id INTEGER PRIMARY KEY AUTOINCREMENT, candidate_id INTEGER NOT NULL, document_revision_id INTEGER NOT NULL, section_path TEXT NOT NULL, start_line INTEGER NOT NULL, end_line INTEGER NOT NULL, excerpt TEXT NOT NULL, excerpt_hash TEXT NOT NULL, created_at DATETIME)`,
		`CREATE TABLE standard.document_file_cleanups (id INTEGER PRIMARY KEY AUTOINCREMENT, object_key TEXT NOT NULL UNIQUE, attempts INTEGER NOT NULL DEFAULT 0, next_attempt_at DATETIME NOT NULL, last_error TEXT, created_at DATETIME, updated_at DATETIME)`,
		`CREATE TABLE standard.metric_definitions (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id INTEGER NOT NULL, scope_type TEXT NOT NULL, owner_domain_id INTEGER, code TEXT NOT NULL, draft_revision_id INTEGER, version INTEGER NOT NULL DEFAULT 1, lifecycle_state TEXT NOT NULL)`,
		`CREATE TABLE standard.metric_definition_revisions (id INTEGER PRIMARY KEY AUTOINCREMENT, metric_definition_id INTEGER NOT NULL, revision_no INTEGER NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL, definition TEXT NOT NULL, statistical_caliber TEXT NOT NULL, semantic_formula TEXT, unit_id INTEGER, effective_from DATETIME, effective_to DATETIME)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create document service test schema: %v", err)
		}
	}
	return db
}
