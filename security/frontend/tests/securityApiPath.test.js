import { describe, expect, it, vi } from 'vitest'
vi.mock('../src/api/client', () => ({ default: { get: vi.fn(path => path), post: vi.fn((path, data) => ({path,data})), put: vi.fn((path,data)=>({path,data})), delete: vi.fn((path,config)=>config?{path,config}:path) } }))
import client from '../src/api/client'
import { assessmentAPI, classificationAPI, definitionProfileAPI, detectorAPI, detectorCapabilityAPI, discoveryQualityAPI, findingAPI, gradeAPI, sensitiveDataTypeAPI, protectionAccessRequestAPI, protectionBaselineAPI, protectionEnrollmentAPI, protectionExemptionAPI, metaAPI } from '../src/api/security'

describe('Security API paths', () => {
  it('uses the single /security route family', () => {
    expect(classificationAPI.list()).toBe('/security/classifications')
    expect(gradeAPI.get(2)).toBe('/security/grades/2')
    expect(sensitiveDataTypeAPI.delete(3)).toBe('/security/sensitive-data-types/3')
    expect(definitionProfileAPI.list()).toBe('/security/definition-profiles')
    expect(definitionProfileAPI.apply('recommended').path).toBe('/security/definition-profile-applications')
    expect(detectorCapabilityAPI.list()).toBe('/security/detector-capabilities')
    expect(detectorAPI.create({ capability_key: 'x' }).path).toBe('/security/detectors')
    expect(detectorAPI.delete(8, { version: 2 }).config.data).toEqual({ version: 2 })

    const qualityParams = { sensitive_data_type_id: 9 }
    discoveryQualityAPI.get(qualityParams)
    expect(client.get).toHaveBeenLastCalledWith('/security/discovery-quality', { params: qualityParams })
    expect(protectionBaselineAPI.update(4, {}).path).toBe('/security/protection-baselines/4')
    expect(protectionBaselineAPI.delete(4, { version: 2 }).config.data).toEqual({ version: 2 })

    const listParams = { scope: 'released', page: 2, page_size: 20 }
    protectionEnrollmentAPI.list(listParams)
    expect(client.get).toHaveBeenLastCalledWith('/security/protection-enrollments', { params: listParams })
    const findingParams = { snapshot_scope: 'current', review_state: 'pending' }
    findingAPI.list(findingParams)
    expect(client.get).toHaveBeenLastCalledWith('/security/findings', { params: findingParams })
    expect(protectionEnrollmentAPI.components('enrollment-1')).toBe('/security/protection-enrollments/enrollment-1/components')

    expect(assessmentAPI.create({ enrollment_id: 'enrollment-1' }).path).toBe('/security/assessments')
    const revokeAssessment = assessmentAPI.revoke('assessment-1', { version: 2, rationale: 'verified' })
    expect(revokeAssessment.path).toBe('/security/assessments/assessment-1')
    expect(revokeAssessment.config.data).toEqual({ version: 2, rationale: 'verified' })

    const targetParams = { target_identity: 'fingerprint', consumer_owner: 'manager', action: 'preview' }
    protectionAccessRequestAPI.targets(targetParams)
    expect(client.get).toHaveBeenLastCalledWith('/security/protection-access-request-targets', { params: targetParams })
    expect(protectionAccessRequestAPI.create({ assessment_id: 'assessment-1' }).path).toBe('/security/protection-access-requests')
    expect(protectionAccessRequestAPI.decide('request-1', { decision: 'approve' }).path).toBe('/security/protection-access-requests/request-1/decisions')

    const revokeExemption = protectionExemptionAPI.revoke('exemption-1', { version: 3, rationale: 'done' })
    expect(revokeExemption.config.data).toEqual({ version: 3, rationale: 'done' })
    expect(protectionExemptionAPI).not.toHaveProperty('create')
    expect(protectionExemptionAPI).not.toHaveProperty('renew')

    const reenrollment = protectionEnrollmentAPI.reEnroll('enrollment-1', { version: 5 })
    expect(reenrollment.path).toBe('/security/protection-enrollments/enrollment-1/re-enrollments')
    expect(reenrollment.data).toEqual({ version: 5 })
    const release = protectionEnrollmentAPI.release('enrollment-1', { version: 4, basis: 'no_supported_findings', reason: 'reviewed' })
    expect(release.path).toBe('/security/protection-enrollments/enrollment-1/releases')
    expect(release.data).toEqual({ version: 4, basis: 'no_supported_findings', reason: 'reviewed' })
    expect(protectionEnrollmentAPI.rediscover('enrollment-1', { version: 3 }).path).toBe('/security/protection-enrollments/enrollment-1/discovery-executions')
    expect(metaAPI.getItem(51657)).toBe('/meta/items/51657')
  })
})
