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

  it('uses the single paginated cross-extraction candidate group route', () => {
    expect(documentDetailSource).toContain('documentAPI.listCandidateGroups')
    expect(documentDetailSource).not.toContain('documentAPI.listExtractions')
    expect(standardApiSource).toContain('/extraction-candidate-groups')
    expect(standardApiSource).not.toContain('listExtractions')
    expect(documentDetailSource).toContain('group.semantic_fingerprint')
    expect(documentDetailSource).toContain('group.occurrences')
    expect(documentDetailSource).toContain('createLatestRequestCoordinator')
    expect(documentDetailSource).toContain('candidateQuery.page > result.total_pages')
    expect(zhCn.standard.document.candidateGroupState.formalized).toBe('已正式化')
    expect(en.standard.document.candidateEvidenceHistory).toContain('Evidence')
  })
})
