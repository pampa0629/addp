import { describe, expect, it } from 'vitest'

import { findingDecisionState, findingOutletRules } from '../src/utils/protectionEnrollment.mjs'

describe('Security finding explanation view model', () => {
  it('accepts only the decision states defined by the Security contract', () => {
    expect(findingDecisionState({ explanation: { decision_state: 'formal' } })).toBe('formal')
    expect(findingDecisionState({ explanation: { decision_state: 'revoked' } })).toBe('revoked')
    expect(findingDecisionState({ explanation: { decision_state: 'baseline_missing' } })).toBe('baseline_missing')
    expect(findingDecisionState({ explanation: { decision_state: 'invented' } })).toBe('awaiting_review')
  })

  it('reads actual outlet rules without predicting them from the baseline', () => {
    const finding = {
      explanation: {
        outlets: [{
          consumer_owner: 'manager',
          projection_state: 'active',
          acknowledged: true,
          rules: [
            { action: 'preview', effect: 'mask', algorithm: 'keep_prefix_suffix_v1' },
            { action: 'profile', effect: 'suppress' }
          ]
        }]
      }
    }

    expect(findingOutletRules(finding, 'manager')).toEqual({
      consumerOwner: 'manager',
      projectionState: 'active',
      acknowledged: true,
      rules: [
        { action: 'preview', effect: 'mask', algorithm: 'keep_prefix_suffix_v1' },
        { action: 'profile', effect: 'suppress', algorithm: '' }
      ]
    })
    expect(findingOutletRules(finding, 'transfer')).toBeNull()
  })
})
