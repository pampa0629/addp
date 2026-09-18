import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import * as Vue from 'vue'
import * as icons from '@element-plus/icons-vue'
import { parse, compileScript } from '@vue/compiler-sfc'
import { renderToString } from '@vue/server-renderer'

const read = path => readFileSync(new URL(path, import.meta.url), 'utf8')
const layout = read('../src/components/Layout.vue')
const consoleConfig = read('../../../console/frontend/src/config/portalConfig.js')
const consoleMenu = consoleConfig.match(/service: \{\s*label: 'console\.menus\.service\.label'[\s\S]*?items: \[([\s\S]*?)\]/)[1]
const expected = [...consoleMenu.matchAll(/index: '\/service([^']+)',\s*icon: (\w+),\s*label: 'console\.menus\.service\.(\w+)'/g)]
  .map(([, path, icon, label]) => ({ path, icon, label }))

test('Service navigation has one layout owner', () => {
  const app = read('../src/App.vue')
  assert.doesNotMatch(app, /<el-(?:aside|menu|header|container)\b/)
  assert.equal((app.match(/<router-view\b/g) || []).length, 1)
  assert.match(read('../src/router/index.js'), /component: Layout/)
})

test('standalone menu routes, order, icons and translations match Console', () => {
  const actual = [...layout.matchAll(/<el-menu-item index="([^"]+)">\s*<el-icon><(\w+)\s*\/><\/el-icon>\s*<span>\{\{ t\('service\.nav\.(\w+)'\) \}\}<\/span>/g)]
    .map(([, path, icon, label]) => ({ path, icon, label }))
  assert.equal(expected.length, 5)
  assert.deepEqual(actual.map(({ path, icon }) => ({ path, icon })), expected.map(({ path, icon }) => ({ path, icon })))
  for (const locale of ['zh-cn', 'en']) {
    const serviceLabels = JSON.parse(read(`../src/i18n/${locale}.json`)).service.nav
    const consoleLabels = JSON.parse(read(`../../../console/frontend/src/i18n/${locale}.json`)).console.menus.service
    assert.deepEqual(actual.map(item => serviceLabels[item.label]), expected.map(item => consoleLabels[item.label]))
  }
})

async function renderLayout(embedded, path) {
  const { descriptor } = parse(layout)
  let code = compileScript(descriptor, { id: 'service-layout', inlineTemplate: true }).content
  code = code.replace(/import \{([^}]+)\} from ["']([^"']+)["']/g,
    (_, names, module) => `const {${names.replace(/ as /g, ': ')}} = modules[${JSON.stringify(module)}]`)
    .replace('export default', 'return')
  const modules = {
    vue: Vue,
    'vue-router': { useRouter: () => ({}), useRoute: () => ({ path }) },
    '../store/auth': { useAuthStore: () => ({ user: { username: 'tester' } }) },
    'vue-i18n': { useI18n: () => ({ t: key => key }) },
    '@element-plus/icons-vue': icons
  }
  const window = { self: {} }
  window.top = embedded ? {} : window.self
  const Component = new Function('modules', 'window', code)(modules, window)
  const app = Vue.createSSRApp(Component)
  for (const tag of ['header', 'dropdown', 'dropdown-menu', 'dropdown-item', 'icon', 'container', 'aside', 'menu', 'menu-item', 'main']) {
    app.component(`el-${tag}`, { setup: (_, { attrs, slots }) => () => Vue.h(`test-${tag}`, attrs, slots.default?.()) })
  }
  app.component('router-view', { render: () => Vue.h('section', 'service-page') })
  return renderToString(app)
}

test('Console iframe renders content without module navigation from the first render', async () => {
  const html = await renderLayout(true, '/query-services')
  assert.match(html, /service-page/)
  assert.doesNotMatch(html, /<test-(?:aside|menu|header)\b/)
})

test('standalone detail pages keep the corresponding Console menu active', async () => {
  for (const { path } of expected) {
    const html = await renderLayout(false, `${path}/42`)
    assert.equal((html.match(/<test-aside\b/g) || []).length, 1)
    assert.match(html, new RegExp(`default-active="${path}"`))
    assert.match(html, /service-page/)
  }
})
