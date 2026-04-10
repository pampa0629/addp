import { defineStore } from 'pinia'
import { ref, watch } from 'vue'

const STORAGE_KEY = 'addp-lang'
const SUPPORTED_LANGS = ['zh-cn', 'en']
const DEFAULT_LANG = 'zh-cn'

function readStoredLang() {
  const stored = localStorage.getItem(STORAGE_KEY)
  if (SUPPORTED_LANGS.includes(stored)) return stored
  const browserLang = navigator.language?.toLowerCase()
  if (browserLang?.startsWith('zh')) return 'zh-cn'
  if (browserLang?.startsWith('en')) return 'en'
  return DEFAULT_LANG
}

/**
 * Console 语言管理 Store
 *
 * 功能:
 * 1. 管理当前语言偏好（持久化到 localStorage）
 * 2. 监听语言变化并通过 postMessage 广播到所有 iframe 模块
 */
export const useLangStore = defineStore('lang', () => {
  const lang = ref(readStoredLang())

  /**
   * 切换语言
   * @param {string} newLang - 'zh-cn' | 'en'
   */
  const setLang = (newLang) => {
    if (!SUPPORTED_LANGS.includes(newLang)) return
    lang.value = newLang
    localStorage.setItem(STORAGE_KEY, newLang)
    console.log('[Console Lang] 语言切换为:', newLang)
  }

  // 监听语言变化，广播到所有 iframe 模块
  watch(lang, (newLang) => {
    const iframes = document.querySelectorAll('iframe.module-iframe')
    if (iframes.length === 0) return

    console.log(`[Console Lang] 广播语言变化到 ${iframes.length} 个 iframe`, newLang)

    iframes.forEach((iframe, index) => {
      try {
        const iframeUrl = new URL(iframe.src)
        const targetOrigin = `${iframeUrl.protocol}//${iframeUrl.host}`

        iframe.contentWindow?.postMessage({
          type: 'lang-change',
          source: 'addp-console',
          lang: newLang,
          timestamp: Date.now()
        }, targetOrigin)

        console.log(`[Console Lang] 已发送消息到 iframe #${index} (${targetOrigin})`)
      } catch (e) {
        console.warn(`[Console Lang] 发送消息到 iframe #${index} 失败:`, e)
      }
    })
  })

  return {
    lang,
    setLang,
    SUPPORTED_LANGS
  }
})
