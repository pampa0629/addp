import { createApp, h } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
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
const scope = new URL(location.href).searchParams.get('scope') || 'platform'
auth.authContext = { context: { type: scope }, authorization: { role_assignments: [{ permissions: ['develop.configuration.read', 'develop.configuration.update'] }] } }
const { i18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zh, en }, listenToConsole: false })
app.use(i18n).use(ElementPlus).mount('#app')
