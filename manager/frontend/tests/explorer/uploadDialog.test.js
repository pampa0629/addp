import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import UploadDialog from '../../src/components/explorer/UploadDialog.vue'

const mocks = vi.hoisted(() => ({ upload: vi.fn(), scan: vi.fn(), success: vi.fn(), error: vi.fn(), info: vi.fn(), own: vi.fn() }))
vi.mock('@/api/dataExplorer', () => ({ dataExplorerAPI: { uploadFiles: mocks.upload, getOwnUpload: mocks.own } }))
vi.mock('@/api/import', () => ({ getMetaScanExecutionStatus: mocks.scan }))
vi.mock('vue', async importOriginal => ({ ...await importOriginal(), useSSRContext: () => ({ modules: new Set() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key }) }))
vi.mock('element-plus', () => ({ ElMessage: { success: mocks.success, error: mocks.error, info: mocks.info, warning: vi.fn() } }))
vi.mock('@element-plus/icons-vue', () => ({ UploadFilled: {} }))

function dialog() {
  const emit = vi.fn()
  const state = UploadDialog.setup({ targetNodeLocator: 'addp://engine/12/path/addp/samples?type=prefix' }, { expose: () => {}, emit })
  state.handleFileChange(null, [{ raw: new Blob(['sample']) }])
  return { ...state, emit }
}

beforeEach(() => { vi.clearAllMocks(); vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })

describe('upload scan completion', () => {
  it('waits for the exact returned scan UUID before reporting success and refreshing', async () => {
    mocks.upload.mockResolvedValue({ scan_execution_id: 'exact-scan-uuid' })
    mocks.scan.mockResolvedValueOnce({ status: 'pending' }).mockResolvedValueOnce({ status: 'success' })
    const state = dialog()
    const pending = state.handleUpload()
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.scan).toHaveBeenCalledWith('exact-scan-uuid')
    expect(state.uploading.value).toBe(true)
    expect(state.emit).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2000)
    await pending
    expect(state.uploading.value).toBe(false)
    expect(state.emit).toHaveBeenCalledWith('success', { scan_execution_id: 'exact-scan-uuid' })
    expect(mocks.success).toHaveBeenCalledOnce()
  })

  it('clears the waiting state and never reports success when status lookup is denied', async () => {
    mocks.upload.mockResolvedValue({ scan_execution_id: 'exact-scan-uuid' })
    mocks.scan.mockRejectedValue({ response: { data: { error: 'execution is not readable' } } })
    const state = dialog()
    await state.handleUpload()
    expect(state.uploading.value).toBe(false)
    expect(state.progressText.value).toBe('')
    expect(state.emit).not.toHaveBeenCalled()
    expect(mocks.success).not.toHaveBeenCalled()
    expect(mocks.error).toHaveBeenCalledOnce()
    expect(mocks.upload).toHaveBeenCalledOnce()
  })
})


describe('new upload identification and initial authorization', () => {
  it('polls only the own-upload status and never requires broad Meta read permissions', async () => {
    mocks.upload.mockResolvedValue({ upload_id: 'own-upload', initial_status: 'pending' })
    mocks.own.mockResolvedValueOnce({ initial_status: 'pending' }).mockResolvedValueOnce({ upload_id: 'own-upload', status: 'success', initial_status: 'settled', scan_execution_id: 'exact-scan', outcomes: [{ outcome: 'issued', action: 'metadata_read' }] })
    const state = dialog()
    const pending = state.handleUpload()
    await vi.advanceTimersByTimeAsync(0)
    expect(mocks.own).toHaveBeenCalledWith('own-upload')
    expect(mocks.scan).not.toHaveBeenCalled()
    expect(state.emit).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2000)
    await pending
    expect(mocks.info).toHaveBeenCalledWith('manager.upload.initialMetadataIssued')
    expect(state.emit).toHaveBeenCalledWith('success', expect.objectContaining({ metadataReady: true, upload_id: 'own-upload' }))
    expect(mocks.upload).toHaveBeenCalledOnce()
  })

  it('keeps committed bytes distinct from failed status reads and rechecks without a second upload', async () => {
    mocks.upload.mockResolvedValue({ upload_id: 'own-upload', initial_status: 'pending' })
    mocks.own.mockRejectedValueOnce(new Error('status unavailable')).mockResolvedValueOnce({ initial_status: 'denied', outcomes: [] })
    const state = dialog()
    await state.handleUpload()
    expect(state.progressText.value).toBe('manager.upload.initialStatusUnavailable')
    expect(mocks.error).not.toHaveBeenCalled()
    expect(mocks.success).not.toHaveBeenCalled()
    await state.handleUpload()
    expect(mocks.upload).toHaveBeenCalledOnce()
    expect(mocks.info).toHaveBeenCalledWith('manager.upload.initialNotGranted')
  })
})
