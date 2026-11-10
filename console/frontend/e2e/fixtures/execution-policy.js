import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import 'element-plus/theme-chalk/dark/css-vars.css'
import '@common-ui/styles/theme.css'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import { useAuthStore } from '../../src/store/auth'
import PolicyConfiguration from '../../src/components/configuration/PolicyConfiguration.vue'
import zh from '../../src/i18n/zh-cn.json'
import en from '../../src/i18n/en.json'

const params = new URL(location.href).searchParams
const owner = params.get('owner') || 'develop'
const app = createApp({ render: () => h(PolicyConfiguration, { owner }) })
const pinia = createPinia()
app.use(pinia)
const auth = useAuthStore()
const scope = params.get('scope') || 'platform'
const permissions = [`${owner}.configuration.read`]
if (params.get('readonly') !== 'true') permissions.push(`${owner}.configuration.update`)
const tenantID = '3'
auth.authContext = {
  context: scope === 'tenant' ? { type: 'tenant', tenant_id: tenantID } : { type: 'platform' },
  authorization: { role_assignments: [{
    scope: scope === 'tenant' ? { type: 'tenant', tenant_id: tenantID } : { type: 'platform' },
    permissions
  }] }
}
const { i18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zh, en }, listenToConsole: false })
app.use(i18n).use(ElementPlus).mount('#app')
