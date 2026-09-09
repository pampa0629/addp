import { readFileSync } from 'node:fs'

import { describe, expect, it } from 'vitest'

import en from '../src/i18n/en.json'
import zhCn from '../src/i18n/zh-cn.json'

const documentDetailSource = readFileSync(new URL('../src/views/DocumentDetail.vue', import.meta.url), 'utf8')
const standardApiSource = readFileSync(new URL('../src/api/standard.js', import.meta.url), 'utf8')

describe('document candidate contract', () => {
  it('shows an enumeration candidate code set reference with bilingual labels', () => {
    expect(documentDetailSource).toContain('candidate.payload?.code_set_code')
    expect(documentDetailSource).toContain('candidate.payload.code_set_code')
    expect(zhCn.standard.document.codeSetReference).toBe('引用码值集候选')
    expect(en.standard.document.codeSetReference).toBe('Referenced Code Set Candidate')
    expect(zhCn.standard.document.comparisonField.code_set_code).toBe('码值集编码')
    expect(en.standard.document.comparisonField.code_set_code).toBe('Code Set Code')
  })

  it('does not translate an empty comparison field during table slot initialization', () => {
    expect(documentDetailSource).toContain("const comparisonFieldLabel = field => field ? t(`standard.document.comparisonField.${field}`) : ''")
  })

  it('keeps retention separate from permission-aware formalization', () => {
    expect(documentDetailSource).toContain("group.state === 'retained' && canFormalizeCandidate(group.candidate)")
    expect(documentDetailSource).toContain('buildStandardPermission(candidate.candidate_type, action)')
    expect(documentDetailSource).toContain('documentAPI.formalizeCandidate')
    expect(documentDetailSource).toContain('candidate.formalization?.standard_id')
    expect(zhCn.standard.document.formalizationAction.created_identity).toBe('已创建 R1 草稿')
    expect(en.standard.document.formalizationAction.linked_existing).toBe('Existing revision linked')
  })

  it('uses the single paginated candidate family route without merging semantic variants', () => {
    expect(documentDetailSource).toContain('documentAPI.listCandidateFamilies')
    expect(documentDetailSource).not.toContain('documentAPI.listExtractions')
    expect(documentDetailSource).not.toContain('documentAPI.listCandidateGroups')
    expect(standardApiSource).toContain('/extraction-candidate-families')
    expect(standardApiSource).not.toContain('/extraction-candidate-groups')
    expect(standardApiSource).not.toContain('listExtractions')
    expect(documentDetailSource).toContain('family.family_key')
    expect(documentDetailSource).toContain('family.variants')
    expect(documentDetailSource).toContain('group.semantic_fingerprint')
    expect(documentDetailSource).toContain('group.occurrences')
    expect(documentDetailSource).toContain('candidateFamilyResponse.variant_total')
    expect(documentDetailSource).toContain('candidateFamilyResponse.variant_status_counts')
    expect(documentDetailSource).toContain('createLatestRequestCoordinator')
    expect(documentDetailSource).toContain('candidateQuery.page > result.total_pages')
    expect(zhCn.standard.document.candidateGroupState.formalized).toBe('已正式化')
    expect(zhCn.standard.document.candidateFamilyTotal).toContain('语义变体')
    expect(en.standard.document.candidateFamilyVariantCount).toContain('semantic variants')
    expect(en.standard.document.candidateEvidenceHistory).toContain('Evidence')
  })

  it('filters candidate families by the live family comparison facet', () => {
    expect(documentDetailSource).toContain('candidateQuery.comparison_result')
    expect(documentDetailSource).toContain("const comparisonResults = ['new', 'exact', 'content_conflict', 'scope_conflict']")
    expect(documentDetailSource).toContain("standard.document.allComparisonResults")
    expect(documentDetailSource).toContain('candidateFamilyResponse.family_comparison_counts')
    expect(documentDetailSource).toContain('candidateFamilyResponse.family_comparison_counts?.all')
    expect(documentDetailSource).toContain('<el-radio-group v-model="candidateQuery.comparison_result"')
    expect(zhCn.standard.document.allComparisonResults).toBe('全部比对结果')
    expect(en.standard.document.allComparisonResults).toBe('All Comparison Results')
    expect(zhCn.standard.document.comparisonFacetLabel).toBe('比对结果')
    expect(en.standard.document.comparisonFacetLabel).toBe('Comparison Result')
    expect(documentDetailSource).toContain("standard.document.comparisonFacetHint")
    expect(documentDetailSource).toContain("standard.document.comparisonFacetHelpLabel")
    expect(documentDetailSource).toContain('<InfoFilled />')
    expect(zhCn.standard.document.comparisonFacetHint).toContain('各分类数量之和不一定等于候选族总数')
    expect(en.standard.document.comparisonFacetHint).toContain('do not necessarily add up')
  })

  it('searches candidate groups by representative code or name', () => {
    expect(documentDetailSource).toContain('candidateQuery.keyword')
    expect(documentDetailSource).toContain('standard.document.candidateSearchPlaceholder')
    expect(documentDetailSource).toContain('@change="applyCandidateFilters"')
    expect(zhCn.standard.document.candidateSearchPlaceholder).toBe('搜索候选编码或名称')
    expect(en.standard.document.candidateSearchPlaceholder).toBe('Search candidate code or name')
  })
})
