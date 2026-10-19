// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, reactive, ref } from 'vue'
import { existsSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import CollectionDetail from '../src/views/CollectionDetail.vue'

const fixtures = vi.hoisted(() => ({ auth: null, api: {
  getCollection: vi.fn(), listMyProjectGroups: vi.fn(), updateCollection: vi.fn(), deleteCollection: vi.fn()
} }))
vi.mock('../src/store/auth', () => ({ useAuthStore: () => fixtures.auth }))
vi.mock('../src/api/catalog', () => fixtures.api)
vi.mock('../src/components/CollectionEntryPicker.vue', () => ({ default: { render: () => null } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { id: 'collection' } }), useRouter: () => ({}) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: key => key, locale: ref('en') }) }))
vi.mock('@common-ui', () => ({ navigateConsoleModuleRoute: vi.fn(), useConsolePageDescriptor: vi.fn() }))

const mounted = []
async function settle() { for (let index = 0; index < 5; index++) { await Promise.resolve(); await nextTick() } }
async function mount() {
  const root = document.createElement('div')
  document.body.appendChild(root)
  const app = createApp(CollectionDetail)
  for (const name of ['button', 'card', 'descriptions', 'descriptions-item', 'alert', 'empty', 'skeleton', 'result', 'input', 'form-item', 'form']) {
    app.component(`el-${name}`, { setup: (_, { slots }) => () => h('div', slots.default?.()) })
  }
  // Neither hidden dialogs nor empty tables render their scoped content.
  for (const name of ['dialog', 'table', 'table-column']) app.component(`el-${name}`, { render: () => null })
  app.mount(root)
  mounted.push({ app, root })
  await settle()
  return root
}

beforeEach(() => {
  vi.resetAllMocks()
  fixtures.auth = reactive({ permissions: ['catalog.collection.read', 'catalog.collection.update'],
    hasPermission(permission) { return this.permissions.includes(permission) } })
  fixtures.api.getCollection.mockResolvedValue({ id: 'collection', name: 'C', project_group_id: '9007199254740993', entries: [] })
  fixtures.api.listMyProjectGroups.mockResolvedValue({ data: [
    { project_group_id: '9007199254740993', name: 'Delivery', relation_role: 'member', can_read: true, can_update: true }
  ] })
})
afterEach(() => mounted.splice(0).forEach(({ app, root }) => { app.unmount(); root.remove() }))

describe('Catalog collection tenant permissions and member resources', () => {
  it('uses the same server-resolved member option as the collection list', async () => {
    const root = await mount()
    expect(root.textContent).toContain('catalog.collections.edit')
    expect(root.textContent).toContain('catalog.collections.delete')
    fixtures.auth.permissions = ['catalog.collection.read']
    await settle()
    expect(root.textContent).not.toContain('catalog.collections.edit')
    expect(root.textContent).not.toContain('catalog.collections.delete')
  })

  it.each(['not-member', 'read-only', 'unreadable', 'unavailable'])('does not expose writes for %s resources', async state => {
    if (state === 'unavailable') fixtures.api.listMyProjectGroups.mockRejectedValue(new Error('unavailable'))
    else fixtures.api.listMyProjectGroups.mockResolvedValue({ data: state === 'not-member' ? [] : [
      { project_group_id: '9007199254740993', name: 'Delivery', can_read: state !== 'unreadable', can_update: state !== 'read-only' }
    ] })
    const root = await mount()
    expect(root.textContent).not.toContain('catalog.collections.edit')
    expect(root.textContent).not.toContain('catalog.collections.delete')
    expect(fixtures.api.updateCollection).not.toHaveBeenCalled()
    expect(fixtures.api.deleteCollection).not.toHaveBeenCalled()
  })

  it('removes the old scoped-role interpretation instead of keeping a second route', () => {
    expect(existsSync(resolve('src/utils/projectGroupScope.js'))).toBe(false)
    for (const name of ['CollectionDetail', 'CollectionList']) {
      const source = readFileSync(resolve(`src/views/${name}.vue`), 'utf8')
      expect(source).not.toContain('projectGroupScope')
      expect(source).not.toContain('role_assignments')
      expect(source).toContain('group.can_update')
    }
  })

  it('declares tenant functional permissions without retiring organization resource rules', () => {
    const manifest = readFileSync(resolve('../authorization/permissions.yaml'), 'utf8')
    expect(manifest).not.toMatch(/allowed_scope_types:.*(?:department|project_group)/)
    for (const permission of ['catalog.entry.read', 'catalog.entry.update', 'catalog.collection.read', 'catalog.collection.update']) {
      expect(manifest).toContain(`- key: ${permission}\n    allowed_scope_types: [tenant]`)
    }
  })
})
