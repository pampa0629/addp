import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import '@common-ui/styles/theme.css'
import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
import ManualScanDialog from '../src/components/ManualScanDialog.vue'
import zh from '../src/i18n/zh-cn.json'
import en from '../src/i18n/en.json'

const options = new URLSearchParams(location.search)
const locale = options.get('lang') === 'zh-cn' ? 'zh-cn' : 'en'
localStorage.setItem('addp-lang', locale)
const { i18n } = createAddpI18n({ moduleMessages: { 'zh-cn': zh, en }, listenToConsole: false })
i18n.global.locale.value = locale
const submissions = ref([])
const context = {
  engine: { id: 33, name: 'Fixture HDFS', engine_type: 'hdfs', lifecycle_state: 'active', connection_status: 'online' },
  scope: { kind: 'catalog', paths: ['samples'] },
  force: options.get('checked') === 'true'
}

// Mount the production dialog and CSS; no backend request or personal identity is used.
createApp({ render: () => h('div', [
  h(ManualScanDialog, { context, onSubmit: payload => submissions.value.push(payload) }),
  h('pre', { 'data-testid': 'manual-scan-submissions' }, JSON.stringify(submissions.value))
]) }).use(createPinia()).use(i18n).use(ElementPlus).mount('#app')
