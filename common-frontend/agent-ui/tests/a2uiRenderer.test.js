import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'

import A2UISurface from '../src/a2ui/A2UISurface.vue'

const ElButtonStub = {
  props: ['disabled'],
  emits: ['click'],
  template: '<button :disabled="disabled" @click="$emit(\'click\')"><slot /></button>'
}

const SlotStub = { template: '<span><slot /></span>' }

const presentationI18n = createI18n({
  legacy: false,
  locale: 'zh-cn',
  messages: {
    'zh-cn': {
      agent: { chat: { presentation: {
        mapTruncated: '地图样本', tableSummary: '显示 {visible} / {total} 行',
        truncated: '受限样本', nullValue: '空值'
      } } },
      map: {
        featureId: '要素 ID', unknown: '未知', unknownGeometry: '未知几何',
        nullValue: '空值', noFieldData: '暂无字段', geometryPoint: '点', geometryMultiPoint: '多点',
        geometryLineString: '线', geometryMultiLineString: '多线', geometryPolygon: '面',
        geometryMultiPolygon: '多面', mapServiceNotConfigured: '未配置地图服务'
      }
    }
  }
})

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

  it('approval actions can only open owner or request a status check', async () => {
    const i18n = createI18n({
      legacy: false,
      locale: 'zh-cn',
      messages: {
        'zh-cn': {
          agent: { chat: { approval: {
            title: '审批', pending: '待审批', workflowEngine: '引擎', taskCount: '任务数',
            expiresAt: '过期时间', openOwner: '打开', checkStatus: '检查'
          } } }
        }
      }
    })
    const wrapper = mount(A2UISurface, {
      props: {
        operations: [
          {
            version: 'v0.9',
            createSurface: { surfaceId: 'surface-3', catalogId: 'addp.catalog/v1' }
          },
          {
            version: 'v0.9',
            updateComponents: {
              surfaceId: 'surface-3',
              components: [{
                id: 'root',
                component: 'ApprovalRequest',
                interactionId: '1b842c47-cdf4-4228-af6e-25bfbaa8609b',
                owner: 'develop',
                ownerInteractionId: '46ea0d75-b9bc-4b25-8d4a-441947081813',
                openUrl: '/develop/approvals/46ea0d75-b9bc-4b25-8d4a-441947081813',
                requestFingerprint: 'a'.repeat(64),
                requestSummary: { workflow_engine_id: 20, task_count: 2 },
                expiresAt: '2026-07-17T10:15:00Z'
              }]
            }
          }
        ]
      },
      global: {
        plugins: [i18n],
        stubs: {
          'el-button': ElButtonStub,
          'el-tag': SlotStub,
          'el-icon': SlotStub
        }
      }
    })

    const buttons = wrapper.findAll('button')
    await buttons[0].trigger('click')
    await buttons[1].trigger('click')
    await vi.waitFor(() => expect(wrapper.emitted('action')).toHaveLength(2))

    const openAction = wrapper.emitted('action')[0][0]
    const checkAction = wrapper.emitted('action')[1][0]
    expect(openAction.name).toBe('owner.open')
    expect(checkAction.name).toBe('interaction.submit')
    expect(checkAction.context.answer).toEqual({ action: 'check' })
    expect(checkAction.context.answer.approved).toBeUndefined()
  })

  it('renders bounded table and map projections', async () => {
    const wrapper = mount(A2UISurface, {
      props: {
        operations: [
          {
            version: 'v0.9',
            createSurface: { surfaceId: 'surface-table', catalogId: 'addp.catalog/v1' }
          },
          {
            version: 'v0.9',
            updateComponents: {
              surfaceId: 'surface-table',
              components: [{
                id: 'root',
                component: 'TablePreview',
                columns: ['name', 'length'],
                rows: [{ name: 'railway', length: 1200 }],
                total: 1,
                truncated: false
              }]
            }
          },
          {
            version: 'v0.9',
            createSurface: { surfaceId: 'surface-map', catalogId: 'addp.catalog/v1' }
          },
          {
            version: 'v0.9',
            updateComponents: {
              surfaceId: 'surface-map',
              components: [{
                id: 'root',
                component: 'MapView',
                crs: 'EPSG:4326',
                features: [{
                  type: 'Feature',
                  geometry: { type: 'Point', coordinates: [104, 30] },
                  properties: { name: 'railway' }
                }],
                height: 320,
                truncated: false
              }]
            }
          }
        ]
      },
      global: {
        plugins: [presentationI18n],
        stubs: {
          'el-table': { template: '<div class="table-stub"><slot /></div>' },
          'el-table-column': { template: '<span class="column-stub" />' },
          MapContainer: { props: ['features', 'featuresOnly'], template: '<div class="map-stub" />' }
        }
      }
    })

    expect(wrapper.find('.agent-table-preview').exists()).toBe(true)
    expect(wrapper.find('.agent-map-view').exists()).toBe(true)
    expect(wrapper.emitted('error')).toBeUndefined()
  })

  it('resource picker dispatches only the observed locator option', async () => {
    const locator = 'addp://engine/8/path/public/railway?type=table&item_id=60'
    const wrapper = mount(A2UISurface, {
      props: {
        operations: [
          {
            version: 'v0.9',
            createSurface: { surfaceId: 'surface-resource', catalogId: 'addp.catalog/v1' }
          },
          {
            version: 'v0.9',
            updateComponents: {
              surfaceId: 'surface-resource',
              components: [{
                id: 'root',
                component: 'ResourcePicker',
                interactionId: '1b842c47-cdf4-4228-af6e-25bfbaa8609b',
                prompt: '请选择铁路数据',
                options: [{
                  label: 'public.railway',
                  value: locator,
                  candidate: { locator, engine_id: 8, full_name: 'public.railway' }
                }]
              }]
            }
          }
        ]
      }
    })

    await wrapper.get('.resource-option').trigger('click')
    await vi.waitFor(() => expect(wrapper.emitted('action')).toHaveLength(1))
    expect(wrapper.emitted('action')[0][0].context.answer).toEqual({
      label: 'public.railway',
      value: locator,
      candidate: { locator, engine_id: 8, full_name: 'public.railway' }
    })
  })

  it.each([
    {
      component: {
        component: 'MapView',
        crs: 'EPSG:4326',
        features: [],
        truncated: false,
        url: 'https://untrusted.example/data.geojson'
      }
    },
    {
      component: {
        component: 'TablePreview',
        columns: ['value'],
        rows: Array.from({ length: 101 }, (_, value) => ({ value })),
        total: 101,
        truncated: true
      }
    },
    {
      component: {
        component: 'ResourcePicker',
        interactionId: '1b842c47-cdf4-4228-af6e-25bfbaa8609b',
        prompt: '请选择资源',
        options: [{
          label: '伪造资源',
          value: 'https://untrusted.example/resource',
          candidate: { locator: 'https://untrusted.example/resource' }
        }]
      }
    },
    {
      component: {
        component: 'MapView',
        crs: 'EPSG:4326',
        features: [
          {
            type: 'Feature',
            geometry: {
              type: 'LineString',
              coordinates: Array.from({ length: 1500 }, (_, index) => [index, 30])
            },
            properties: {}
          },
          {
            type: 'Feature',
            geometry: {
              type: 'LineString',
              coordinates: Array.from({ length: 1500 }, (_, index) => [index, 31])
            },
            properties: {}
          }
        ],
        truncated: true
      }
    }
  ])('rejects unsafe or over-limit presentation %#', async ({ component }) => {
    const wrapper = mount(A2UISurface, {
      props: {
        operations: [
          {
            version: 'v0.9',
            createSurface: { surfaceId: 'surface-invalid', catalogId: 'addp.catalog/v1' }
          },
          {
            version: 'v0.9',
            updateComponents: {
              surfaceId: 'surface-invalid',
              components: [{ id: 'root', ...component }]
            }
          }
        ]
      },
      global: { plugins: [presentationI18n] }
    })

    await vi.waitFor(() => expect(wrapper.emitted('error')).toHaveLength(1))
  })
})
