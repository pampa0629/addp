import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'

import { toConsoleBridgeValue } from '@common-ui/utils/consoleBridge'

describe('console bridge payload normalization', () => {
  it('removes nested Vue proxies before postMessage structured cloning', () => {
    const payload = reactive({
      action: 'sync',
      scanConfig: {
        scheduled_scan: true,
        schedule_value: [1, 3, 5]
      }
    })

    const normalized = toConsoleBridgeValue(payload)

    expect(normalized).toEqual({
      action: 'sync',
      scanConfig: {
        scheduled_scan: true,
        schedule_value: [1, 3, 5]
      }
    })
    expect(() => structuredClone(normalized)).not.toThrow()
  })
})
