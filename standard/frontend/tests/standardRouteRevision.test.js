import { describe, it, expect, vi } from 'vitest'
import { getStandardRouteRevision } from '../src/utils/standardRevision'
describe('exact Standard revision navigation', () => {
 it('retains the management draft when no historical revision is requested', async () => {
  const draft = { id: 2 }, read = vi.fn()
  expect(await getStandardRouteRevision({ draft_revision: draft }, undefined, read)).toBe(draft)
  expect(read).not.toHaveBeenCalled()
 })
 it('reads the exact adopted revision without converting its int64 identity', async () => {
  const historical = { id: '9007199254740993', status: 'withdrawn' }, read = vi.fn(async () => historical)
  expect(await getStandardRouteRevision({ draft_revision: { id: 2 } }, historical.id, read)).toBe(historical)
  expect(read).toHaveBeenCalledWith('9007199254740993')
 })
 it.each(['01', ['1'], '', 'bad'])('rejects a noncanonical revision %s', async id => {
  const read = vi.fn()
  await expect(getStandardRouteRevision({}, id, read)).rejects.toThrow()
  expect(read).not.toHaveBeenCalled()
 })
 it('does not substitute a draft for a missing, mismatched or forbidden revision', async () => {
  for (const read of [async () => null, async () => ({ id: '2' }), async () => { throw new Error('403') }]) {
   await expect(getStandardRouteRevision({ draft_revision: { id: '2' } }, '1', read)).rejects.toThrow()
  }
 })
})
