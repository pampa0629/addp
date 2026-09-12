import { describe, expect, it, vi } from 'vitest'

import { confirmDangerousAction } from '../src/utils/confirmation.mjs'

describe('Security dangerous confirmation', () => {
  it('uses the shared message-box contract and focuses the safe cancel action', async () => {
    const cancelButton = {
      classList: { contains: () => false },
      focus: vi.fn()
    }
    const deleteButton = {
      classList: { contains: className => className === 'el-button--danger' },
      focus: vi.fn()
    }
    const messageBox = {
      querySelectorAll: vi.fn(() => [cancelButton, deleteButton])
    }
    const documentRef = {
      querySelectorAll: vi.fn(() => [messageBox])
    }
    const confirm = vi.fn(() => Promise.resolve('confirm'))

    await expect(confirmDangerousAction(confirm, {
      message: 'Delete this definition?',
      title: 'Confirmation',
      confirmButtonText: 'Delete',
      cancelButtonText: 'Cancel'
    }, {
      documentRef,
      schedule: callback => callback()
    })).resolves.toBe('confirm')

    expect(confirm).toHaveBeenCalledWith('Delete this definition?', 'Confirmation', {
      type: 'warning',
      customClass: 'addp-message-box',
      confirmButtonText: 'Delete',
      cancelButtonText: 'Cancel',
      confirmButtonClass: 'el-button--danger',
      autofocus: false
    })
    expect(messageBox.querySelectorAll).toHaveBeenCalledWith('.el-message-box__btns button')
    expect(cancelButton.focus).toHaveBeenCalledWith({ preventScroll: true })
    expect(deleteButton.focus).not.toHaveBeenCalled()
  })
})
