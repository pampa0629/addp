import { describe, it, expect } from 'vitest'
import { hasActiveBuild, canCancelBuild, canRunBuild, buildCancellationIdentity } from '../src/utils/buildExecution'

describe('durable graph build controls', () => {
  it('distinguishes a draft from a queued attempt zero', () => {
    const draft = { status: 'pending', execution_id: '', execution_attempt: null }
    const queued = { status: 'pending', execution_id: 'execution-a', execution_attempt: 0 }
    expect(canRunBuild(draft)).toBe(true)
    expect(canCancelBuild(draft)).toBe(false)
    expect(hasActiveBuild(queued)).toBe(true)
    expect(canRunBuild(queued)).toBe(false)
    expect(buildCancellationIdentity(queued)).toEqual({ execution_id: 'execution-a', attempt: 0 })
  })
  it('never guesses an attempt or reuses a terminal identity', () => {
    for (const attempt of [undefined, null, -1, 1.2, '1']) {
      expect(() => buildCancellationIdentity({ status: 'running', execution_id: 'e', execution_attempt: attempt })).toThrow()
    }
    expect(canCancelBuild({ status: 'success', execution_id: 'old', execution_attempt: 1 })).toBe(false)
    expect(buildCancellationIdentity({ status: 'running', execution_id: 'new', execution_attempt: 2 })).toEqual({ execution_id: 'new', attempt: 2 })
  })
})
