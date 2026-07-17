import { describe, expect, it, vi } from 'vitest'
import {
  executeWithCurrentResultConfirmation,
  requiresCurrentResultConfirmation,
  toQuickViewConfirmationPayload
} from '../../src/utils/currentResultConfirmation.js'

describe('current result confirmation', () => {
  it('retries once with the canonical execution parameter after confirmation', async () => {
    const error = { response: { status: 409, data: { code: 'existing_result_confirmation_required' } } }
    const execute = vi.fn()
      .mockRejectedValueOnce(error)
      .mockResolvedValueOnce({ execution_id: 'exec-2' })
    const confirm = vi.fn().mockResolvedValue()

    await expect(executeWithCurrentResultConfirmation(execute, confirm)).resolves.toEqual({ execution_id: 'exec-2' })
    expect(execute).toHaveBeenNthCalledWith(1, {})
    expect(execute).toHaveBeenNthCalledWith(2, { parameters: { confirm_existing_result: true } })
    expect(confirm).toHaveBeenCalledTimes(1)
  })

  it('does not treat another conflict as an overwrite confirmation request', async () => {
    const error = { response: { status: 409, data: { error: 'busy' } } }
    expect(requiresCurrentResultConfirmation(error)).toBe(false)
    await expect(executeWithCurrentResultConfirmation(() => Promise.reject(error), vi.fn())).rejects.toBe(error)
  })

  it('maps the standard execution parameter to the quick-view action DTO', () => {
    expect(toQuickViewConfirmationPayload({})).toEqual({ confirm_existing_result: false })
    expect(toQuickViewConfirmationPayload({ parameters: { confirm_existing_result: true } }))
      .toEqual({ confirm_existing_result: true })
  })
})
