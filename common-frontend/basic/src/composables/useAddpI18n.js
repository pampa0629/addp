import { ref, watch } from 'vue'
import { createI18n, useI18n } from 'vue-i18n'
import zhCnCommon from '../i18n/zh-cn.json'
import enCommon from '../i18n/en.json'

const STORAGE_KEY = 'addp-lang'
export const SUPPORTED_LANGS = ['zh-cn', 'en']
export const DEFAULT_LANG = 'zh-cn'

/**
 * 支持的语言配置列表
 */
export const SUPPORTED_LANGUAGES = [
  { value: 'zh-cn', label: '中文' },
  { value: 'en', label: 'English' }
]

/**
 * 读取用户语言偏好（优先 localStorage，降级用浏览器语言，最后用默认值）
 */
function readStoredLang() {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (SUPPORTED_LANGS.includes(stored)) return stored

  // 尝试读取浏览器语言
  const browserLang = navigator.language?.toLowerCase()
  if (browserLang?.startsWith('zh')) return 'zh-cn'
  if (browserLang?.startsWith('en')) return 'en'

  return DEFAULT_LANG
}

/**
 * 创建 ADDP i18n 实例（在 main.js 中调用，然后 app.use(i18n)）
 *
 * @param {Object} options
 * @param {Object} options.moduleMessages - 模块自定义翻译，格式: { 'zh-cn': {...}, 'en': {...} }
 * @param {boolean} options.listenToConsole - 是否监听来自 Console 的语言切换消息（被嵌入 iframe 时使用）
 * @returns {{ i18n, locale, switchLang, init, cleanup }}
 *
 * @example
 * // main.js
 * import { createAddpI18n } from '@common-ui/composables/useAddpI18n'
 * import moduleMessages from './i18n/messages'
 *
 * const { i18n, init } = createAddpI18n({ moduleMessages })
 * app.use(i18n)
 * init()
 */
export function createAddpI18n(options = {}) {
  const { moduleMessages = {}, listenToConsole = true } = options

  const locale = ref(readStoredLang())

  // 合并公共翻译 + 模块翻译（模块翻译可覆盖公共翻译中的同名 key）
  const messages = {
    'zh-cn': { ...zhCnCommon, ...(moduleMessages['zh-cn'] || {}) },
    'en': { ...enCommon, ...(moduleMessages['en'] || {}) }
  }

  const i18n = createI18n({
    legacy: false,       // composition API 模式
    locale: locale.value,
    fallbackLocale: DEFAULT_LANG,
    messages,
  })

  // 同步外部 locale ref 与 i18n 实例的 locale
  watch(locale, (newLang) => {
    i18n.global.locale.value = newLang
  })

  let cleanupConsoleListener = null

  /**
   * 切换语言
   * @param {string} lang - 'zh-cn' | 'en'
   */
  const switchLang = (lang) => {
    if (!SUPPORTED_LANGS.includes(lang)) {
      console.warn('[I18n] 不支持的语言:', lang, '支持:', SUPPORTED_LANGS)
      return
    }
    locale.value = lang
    localStorage.setItem(STORAGE_KEY, lang)
    console.log('[I18n] 语言切换为:', lang)
  }

  /**
   * 初始化来自 Console 的 postMessage 监听（模块被嵌入 iframe 时使用）
   */
  const initConsoleListener = () => {
    if (window.self === window.top) {
      // 不在 iframe 中，无需监听
      return () => {}
    }

    const handleMessage = (event) => {
      // 验证来源 hostname
      try {
        const eventUrl = new URL(event.origin)
        const currentUrl = new URL(window.location.origin)
        if (eventUrl.hostname !== currentUrl.hostname) return
      } catch {
        return
      }

      if (event.data?.type !== 'lang-change' || event.data?.source !== 'addp-console') {
        return
      }

      console.log('[I18n] 收到 Console 语言切换消息:', event.data.lang)
      switchLang(event.data.lang)
    }

    window.addEventListener('message', handleMessage)
    console.log('[I18n] Console 消息监听器已启动')

    return () => {
      window.removeEventListener('message', handleMessage)
    }
  }

  /**
   * 初始化 i18n 系统（通常在 main.js 中调用）
   */
  const init = () => {
    if (listenToConsole) {
      cleanupConsoleListener = initConsoleListener()
    }
    console.log('[I18n] 初始化完成, locale:', locale.value)
  }

  /**
   * 清理监听器
   */
  const cleanup = () => {
    if (cleanupConsoleListener) {
      cleanupConsoleListener()
      cleanupConsoleListener = null
    }
  }

  return { i18n, locale, switchLang, init, cleanup }
}

/**
 * 在组件内使用 i18n（需要先在 main.js 调用 createAddpI18n 并 app.use(i18n)）
 *
 * @returns {{ t, locale, switchLang }}
 *
 * @example
 * // SomeComponent.vue
 * import { useAddpI18n } from '@common-ui'
 * const { t, switchLang } = useAddpI18n()
 * // template: {{ t('common.confirm') }}
 */
export function useAddpI18n() {
  const { t, locale } = useI18n()

  const switchLang = (lang) => {
    if (!SUPPORTED_LANGS.includes(lang)) return
    locale.value = lang
    localStorage.setItem(STORAGE_KEY, lang)
    console.log('[I18n] 语言切换为:', lang)
  }

  return { t, locale, switchLang }
}

/**
 * 获取当前语言代码（不依赖 vue-i18n 实例，可在任意上下文使用）
 */
export function getCurrentLang() {
  return readStoredLang()
}
