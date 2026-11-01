import { beforeEach, describe, expect, it, vi } from 'vitest'
import { handleEngineScanPolicyBridge } from '../src/utils/engineScanPolicy'
import { createManualScanRun, deleteEngineScanTask, getScanTasks, upsertEngineScanTask } from '../src/api/meta'

vi.mock('../src/api/meta', () => ({
  createManualScanRun: vi.fn(), deleteEngineScanTask: vi.fn(),
  getScanTasks: vi.fn(), upsertEngineScanTask: vi.fn()
}))
beforeEach(() => vi.resetAllMocks())

describe('engine scan policy bridge', () => {
  it('keeps a disabled PG plan and does not trigger an execution', async () => {
    await handleEngineScanPolicyBridge({ action: 'sync', engine: { id: 9, name: 'PG', engine_type: 'postgresql' }, scanConfig: { scheduled_scan: false, immediate_scan: false, cron_expression: '0 3 * * *' }, triggerImmediate: false })
    expect(upsertEngineScanTask).toHaveBeenCalledExactlyOnceWith(9, { engine_name: 'PG', scan_policy: { enabled: false, scheduled_scan: false, immediate_scan: false, cron_expression: '0 3 * * *' } })
    expect(deleteEngineScanTask).not.toHaveBeenCalled()
    expect(createManualScanRun).not.toHaveBeenCalled()
  })
  it('loads the disabled state and saved schedule from only the engine-bound plan', async () => {
    getScanTasks.mockResolvedValue([
      { id: 8, enabled: true, schedule: '* * * * *' },
      { id: 9, owner_module: 'system', owner_ref: 'engine:9', enabled: false, schedule: '0 3 * * *', parameters: { scan_depth: 'basic' } }
    ])
    const { scanConfig } = await handleEngineScanPolicyBridge({ action: 'load', engineId: 9 })
    expect(scanConfig.scheduled_scan).toBe(false)
    expect(scanConfig.cron_expression).toBe('0 3 * * *')
    expect(scanConfig.scan_depth).toBe('basic')
    expect(upsertEngineScanTask).not.toHaveBeenCalled()
    expect(createManualScanRun).not.toHaveBeenCalled()
    getScanTasks.mockResolvedValue([{ id: 8, enabled: true, schedule: '* * * * *' }])
    expect(await handleEngineScanPolicyBridge({ action: 'load', engineId: 9 })).toEqual({ scanConfig: null })
  })
  it('keeps immediate scanning independent from disabling scheduling', async () => {
    await handleEngineScanPolicyBridge({ action: 'sync', engine: { id: 9, engine_type: 'postgresql' }, scanConfig: { scheduled_scan: false, immediate_scan: true, immediate_depth: 'basic' }, triggerImmediate: true })
    expect(upsertEngineScanTask).toHaveBeenCalledTimes(1)
    expect(createManualScanRun).toHaveBeenCalledExactlyOnceWith(9, { scan_depth: 'basic', trigger_type: 'manual', source: 'console', force: false })
    expect(deleteEngineScanTask).not.toHaveBeenCalled()
  })
  it('stops on policy failure without starting the requested immediate scan', async () => {
    upsertEngineScanTask.mockRejectedValue(new Error('scope denied'))
    await expect(handleEngineScanPolicyBridge({ action: 'sync', engine: { id: 9, engine_type: 'postgresql' }, scanConfig: { immediate_scan: true }, triggerImmediate: true })).rejects.toThrow('scope denied')
    expect(createManualScanRun).not.toHaveBeenCalled()
  })
  it('preserves the existing non-PG disabled-plan behavior', async () => {
    await handleEngineScanPolicyBridge({ action: 'sync', engine: { id: 9, engine_type: 'minio' }, scanConfig: { scheduled_scan: false }, triggerImmediate: false })
    expect(deleteEngineScanTask).toHaveBeenCalledExactlyOnceWith(9)
    expect(upsertEngineScanTask).not.toHaveBeenCalled()
  })
})
