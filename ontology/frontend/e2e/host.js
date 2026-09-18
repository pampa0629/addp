// A controlled parent using the real shared auth/navigation/leave contracts.
import { createApp, h, ref, watch, nextTick } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import '@common-ui/styles/theme.css'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import { createIframeAuthCoordinator } from '@common-ui/auth/authSession'
import { useConsoleUnsavedChangesGuard } from '@common-ui/composables/useUnsavedChangesGuard'
import { registerConsoleBridgeHandler } from '@common-ui/utils/consoleBridge'
import { CONSOLE_NAVIGATION_CHANNEL } from '@common-ui/utils/taskOwnerUrl'

createIframeAuthCoordinator({
  allowedOrigins: [location.origin],
  getToken: () => 'isolated-parent-token',
  getExpiresAt: () => Date.now() + 300_000,
  refreshToken: async () => 'isolated-parent-token',
  logout: async () => {}
})
const router = createRouter({
  history: createWebHashHistory(),
  routes: [{ path: '/:pathMatch(.*)*', component: { render: () => null } }]
})
const Host = {
  setup() {
    const src = ref(''),
      key = ref(0),
      route = ref('')
    let synchronized = ''
    const guard = useConsoleUnsavedChangesGuard(router, {
      getIframe: () => document.querySelector('iframe'),
      skipNavigation: (to) => synchronized === to.fullPath
    })
    watch(
      () => router.currentRoute.value.fullPath,
      (path) => {
        route.value = path
        if (synchronized === path) return
        guard.reset()
        key.value++
        src.value = path.startsWith('/ontology/') ? path : ''
      },
      { immediate: true }
    )
    registerConsoleBridgeHandler(
      CONSOLE_NAVIGATION_CHANNEL,
      async (payload, message, event) => {
        if (event.source !== document.querySelector('iframe')?.contentWindow)
          throw new Error('inactive source')
        if (payload.synchronized) synchronized = payload.route
        const failure = await router[payload.history](payload.route)
        await nextTick()
        synchronized = ''
        return { cancelled: Boolean(failure) }
      },
      { acknowledgePending: true }
    )
    return { src, key, route, guard, leave: () => router.push('/other') }
  },
  render() {
    return h('div', [
      h('button', { onClick: this.leave }, 'Other module'),
      h('output', this.route),
      this.src
        ? h('iframe', {
            key: this.key,
            src: this.src,
            title: 'Ontology',
            style: { width: '100%', height: '900px' },
            onLoad: () => this.guard.requestState()
          })
        : null
    ])
  }
}
const { i18n } = createAddpI18n({ listenToConsole: false })
createApp(Host).use(i18n).use(router).use(ElementPlus).mount('#host')
