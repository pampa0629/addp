import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import A2UISurface from '../src/a2ui/A2UISurface.vue'

const ElButtonStub = {
  props: ['disabled'],
  emits: ['click'],
  template: '<button :disabled="disabled" @click="$emit(\'click\')"><slot /></button>'
}

describe('ADDP A2UI renderer', () => {
  it('renders clarification options and dispatches a validated action', async () => {
    const operations = [
      {
        version: 'v0.9',
        createSurface: { surfaceId: 'surface-1', catalogId: 'addp.catalog/v1' }
      },
      {
        version: 'v0.9',
        updateComponents: {
          surfaceId: 'surface-1',
          components: [{
            id: 'root',
            component: 'ClarificationChoice',
            interactionId: '1b842c47-cdf4-4228-af6e-25bfbaa8609b',
            prompt: '请选择数据源',
            options: [{ label: 'railway', value: 'locator-1' }]
          }]
        }
      }
    ]

    const wrapper = mount(A2UISurface, {
      props: { operations },
      global: { stubs: { 'el-button': ElButtonStub } }
    })
    await wrapper.get('button').trigger('click')
    await vi.waitFor(() => expect(wrapper.emitted('action')).toHaveLength(1))

    const action = wrapper.emitted('action')[0][0]
    expect(action.name).toBe('interaction.submit')
    expect(action.surfaceId).toBe('surface-1')
    expect(action.context.answer.label).toBe('railway')
  })

  it('rejects components not registered in the ADDP catalog', async () => {
    const wrapper = mount(A2UISurface, {
      props: {
        operations: [
          {
            version: 'v0.9',
            createSurface: { surfaceId: 'surface-2', catalogId: 'addp.catalog/v1' }
          },
          {
            version: 'v0.9',
            updateComponents: {
              surfaceId: 'surface-2',
              components: [{ id: 'root', component: 'ArbitraryScript' }]
            }
          }
        ]
      }
    })
    await vi.waitFor(() => expect(wrapper.emitted('error')).toHaveLength(1))
  })
})
