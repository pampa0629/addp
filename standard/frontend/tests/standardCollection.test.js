import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('Standard collection information architecture', () => {
  it('exposes one collection route family with revisions, assignments, and governance events', () => {
    const api = readFileSync(new URL('../src/api/standard.js', import.meta.url), 'utf8')
    const router = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')

    expect(router).toContain("path: 'collections'")
    expect(router).toContain("path: 'collections/:id'")
    expect(api).toContain("client.get(`/standard/collections/${id}/events`, { params })")
    expect(api).toContain("client.put(`/standard/collections/${id}/assignments`, data)")
    expect(api).toContain("client.post(`/standard/collections/${id}/revisions/${revisionId}/publish`, { version })")
  })

  it('keeps domain and scope semantics out of the collection editor', () => {
    const list = readFileSync(new URL('../src/views/StandardCollectionList.vue', import.meta.url), 'utf8')
    const detail = readFileSync(new URL('../src/views/StandardCollectionDetail.vue', import.meta.url), 'utf8')
    const zhCn = JSON.parse(readFileSync(new URL('../src/i18n/zh-cn.json', import.meta.url), 'utf8'))
    const en = JSON.parse(readFileSync(new URL('../src/i18n/en.json', import.meta.url), 'utf8'))

    expect(`${list}\n${detail}`).not.toContain('owner_domain_id')
    expect(`${list}\n${detail}`).not.toContain('scope_type')
    expect(detail).toContain('standard.collection.events')
    expect(detail).toContain('standard.collection.configureAssignments')
    expect(zhCn.standard.layout.collections).toBe('标准集管理')
    expect(en.standard.layout.collections).toBe('Standard Collections')
  })
})
