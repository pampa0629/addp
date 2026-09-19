import { createApp, h, ref, watch, onBeforeUnmount, nextTick } from 'vue'
import { createRouter, createWebHashHistory, RouterView, useRouter } from 'vue-router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import { useUnsavedChangesGuard, useConsoleUnsavedChangesGuard } from '@common-ui/composables/useUnsavedChangesGuard'
import { navigateConsoleModuleRoute } from '@common-ui/utils/moduleRouteNavigation'
import { openConsoleRoute, CONSOLE_NAVIGATION_CHANNEL } from '@common-ui/utils/taskOwnerUrl'
import { registerConsoleBridgeHandler } from '@common-ui/utils/consoleBridge'
import PortalSidebar from '../../src/components/portal/PortalSidebar.vue'
import PortalIframe from '../../src/components/portal/PortalIframe.vue'
import { SIDEBAR_MENUS } from '../../src/config/portalConfig'
import zh from '../../src/i18n/zh-cn.json'
import en from '../../src/i18n/en.json'

const child = new URL(location.href).searchParams.has('child')
const queryService = new URL(location.href).searchParams.get('editor') === 'query-service'
const serviceNavigation = new URL(location.href).searchParams.get('editor') === 'service-navigation'
const editorRoute = queryService ? '/service/query-services/create' : '/orchestrator'
const Editor = {
  render() { return h('div', [
    h('input', { 'aria-label': 'Draft', value: this.draft, onInput: event => { this.draft = event.target.value } }),
    h('button', { onClick: () => { this.draft = '' } }, 'Save'),
    h('button', { onClick: this.localLeave }, 'Internal leave'),
    h('button', { onClick: this.crossLeave }, 'Cross module'), h('output', this.result)
  ]) },
  setup() {
    const draft = ref(''), result = ref('')
    const router = useRouter()
    useUnsavedChangesGuard({ router, isDirty: () => Boolean(draft.value) })
    return { draft, result,
      localLeave: () => navigateConsoleModuleRoute(router, 'orchestrator', '/list'),
      crossLeave: async () => { result.value = String(await openConsoleRoute('/other')) }
    }
  }
}
const router = createRouter({ history: createWebHashHistory(), routes: child ? [
  { path: '/', component: Editor }, { path: '/list', component: { render: () => h('p', 'Editor list') } }
] : [
  { path: '/:pathMatch(.*)*', component: { render: () => h('span') } }
] })
const Host = {
  mounted() {
    if (serviceNavigation) this.$refs.serviceSidebar.openModule('service', ['service'])
  },
  render() {
    if (serviceNavigation) return h('div', { style: { display: 'flex', height: '100vh' } }, [
      h(PortalSidebar, {
        ref: 'serviceSidebar', activeGroupModules: ['service'], activeGroupKey: 'dev-monitor',
        activeMenu: this.route.split('?')[0].split('/').slice(0, 3).join('/'),
        sidebarMenus: SIDEBAR_MENUS, onMenuSelect: this.go
      }),
      h(PortalIframe, { iframeUrl: this.src, iframeKey: this.frameKey, onLoad: () => this.guard.requestState() })
    ])
    return h('div', [
    h('button', { onClick: () => this.go(editorRoute) }, 'Open editor'),
    h('button', { onClick: () => this.go('/other') }, 'Other page'),
    h('button', { onClick: () => this.go('/third') }, 'Third page'),
    h('p', { id: 'public-route' }, this.route),
    this.src ? h('iframe', { key: this.frameKey, src: this.src, title: 'Editor', onLoad: () => this.guard.requestState() }) : null
  ]) },
  setup() {
    const src = ref(''), frameKey = ref(0), route = ref('')
    let synchronized = ''
    const guard = useConsoleUnsavedChangesGuard(router, {
      getIframe: () => document.querySelector('iframe'),
      skipNavigation: to => synchronized === to.fullPath
    })
    watch(() => router.currentRoute.value.fullPath, fullPath => {
      route.value = fullPath
      if (synchronized === fullPath) return
      guard.reset()
      frameKey.value++
      src.value = queryService || serviceNavigation
        ? (fullPath.startsWith('/service/') ? `/e2e/service-fixture/e2e/${serviceNavigation ? 'navigation' : 'query-form'}.html#${fullPath.slice('/service'.length)}` : '')
        : (fullPath.startsWith('/orchestrator') ? './leave-guard.html?child#/' : '')
    }, { immediate: true })
    const stop = registerConsoleBridgeHandler(CONSOLE_NAVIGATION_CHANNEL, async (payload, message, event) => {
      if (event.source !== document.querySelector('iframe')?.contentWindow) throw new Error('inactive source')
      if (payload.synchronized) synchronized = payload.route
      const failure = await router[payload.history](payload.route)
      await nextTick()
      synchronized = ''
      return { cancelled: Boolean(failure) }
    }, { acknowledgePending: true })
    onBeforeUnmount(stop)
    return { src, frameKey, route, guard, go: path => router.push(path) }
  }
}
const { i18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zh, en }, listenToConsole: false })
createApp(child ? { render: () => h(RouterView) } : Host).use(router).use(i18n).use(ElementPlus).mount('#app')
