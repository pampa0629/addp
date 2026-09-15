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

const app = createApp({ render: () => h(PolicyConfiguration, { owner: 'develop' }) })
const pinia = createPinia()
app.use(pinia)
const auth = useAuthStore()
const params = new URL(location.href).searchParams
const scope = params.get('scope') || 'platform'
const permissions = ['develop.configuration.read']
if (params.get('readonly') !== 'true') permissions.push('develop.configuration.update')
auth.authContext = { context: { type: scope }, authorization: { role_assignments: [{ permissions }] } }
const { i18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zh, en }, listenToConsole: false })
app.use(i18n).use(ElementPlus).mount('#app')
