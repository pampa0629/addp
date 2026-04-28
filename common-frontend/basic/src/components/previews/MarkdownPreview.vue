<template>
  <div ref="containerRef" class="markdown-preview-container">
    <pre v-if="rawMode" class="markdown-raw">{{ rawText || '暂无可用内容' }}</pre>
    <div v-else class="markdown-body" v-html="sanitizedHtml"></div>
    <div v-if="truncated" class="truncate-tip">内容较大，仅展示部分</div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import mermaid from 'mermaid'

const props = defineProps({
  data: {
    type: Object,
    required: true
  },
  rawMode: {
    type: Boolean,
    default: false
  }
})

marked.setOptions({
  breaks: true,
  gfm: true,
  mangle: false,
  headerIds: false
})

const rawText = computed(() => {
  const content = props.data?.object?.content || {}
  if ((content.kind || '').toLowerCase() === 'unsupported') {
    return ''
  }
  return content.text || ''
})

const truncated = computed(() => {
  return props.data?.object?.content?.truncated || props.data?.object?.truncated || false
})

const mermaidInitialized = ref(false)
const containerRef = ref(null)

// 解析 mermaid 块，生成占位 HTML + 定义列表
const parsedContent = ref({ html: '', definitions: [] })

watch(
  () => rawText.value,
  (source) => {
    if (!source) {
      parsedContent.value = { html: '', definitions: [] }
      return
    }
    const definitions = []
    const token = Date.now()
    let index = 0
    const fenceRegex = /```mermaid\s*\n([\s\S]*?)```/gi
    const html = source.replace(fenceRegex, (_, code) => {
      const id = `md-mermaid-${token}-${index++}`
      definitions.push({ id, code: String(code || '').trim() })
      return `<div class="mermaid-diagram" data-mermaid-id="${id}"><div class="mermaid-loading">Rendering Mermaid...</div></div>`
    })
    parsedContent.value = { html, definitions }
  },
  { immediate: true }
)

const sanitizedHtml = computed(() => {
  const source = parsedContent.value.html
  if (!source) {
    return '<p class="markdown-empty">暂无可用内容</p>'
  }
  const html = marked.parse(source)
  return DOMPurify.sanitize(html, {
    USE_PROFILES: { html: true },
    ADD_ATTR: ['data-mermaid-id']
  })
})

const initMermaid = () => {
  if (mermaidInitialized.value) return
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'loose',
    theme: 'dark'
  })
  mermaidInitialized.value = true
}

const forceLabelContrast = (svgEl) => {
  const textSelectors = [
    'text',
    'tspan',
    '.nodeLabel',
    '.edgeLabel',
    '.cluster-label text',
    '.label text'
  ]

  svgEl.querySelectorAll(textSelectors.join(',')).forEach((el) => {
    el.style.setProperty('fill', '#ffffff', 'important')
    el.style.setProperty('stroke', '#111827', 'important')
    el.style.setProperty('stroke-width', '1.2px', 'important')
    el.style.setProperty('paint-order', 'stroke fill', 'important')
    el.style.setProperty('opacity', '1', 'important')
    el.style.setProperty('font-weight', '700', 'important')
  })

  svgEl.querySelectorAll('foreignObject div, foreignObject span, foreignObject p').forEach((el) => {
    el.style.setProperty('color', '#ffffff', 'important')
    el.style.setProperty('opacity', '1', 'important')
    el.style.setProperty('font-weight', '700', 'important')
    el.style.setProperty('text-shadow', '0 0 1px #111827, 0 0 2px #111827, 1px 1px 0 #111827, -1px -1px 0 #111827', 'important')
  })
}

const setupZoom = (host, svgEl) => {
  const vb = svgEl.getAttribute('viewBox')
  let nw = 0, nh = 0
  if (vb) {
    const p = vb.trim().split(/[\s,]+/)
    nw = parseFloat(p[2]) || 0
    nh = parseFloat(p[3]) || 0
  }
  if (!nw) nw = parseFloat(svgEl.getAttribute('width')) || 800
  if (!nh) nh = parseFloat(svgEl.getAttribute('height')) || 400
  if (!svgEl.getAttribute('viewBox')) svgEl.setAttribute('viewBox', `0 0 ${nw} ${nh}`)

  let scale = 1
  const apply = () => {
    svgEl.style.width = `${nw * scale}px`
    svgEl.style.height = `${nh * scale}px`
  }

  const cw = host.clientWidth - 32
  if (cw > 0 && nw > cw) { scale = cw / nw }
  apply()

  const btn = (label, title, fn) => {
    const b = document.createElement('button')
    b.className = 'mermaid-btn'
    b.title = title
    b.textContent = label
    b.addEventListener('click', fn)
    return b
  }

  let panEnabled = false
  let dragging = false
  let startX = 0
  let startY = 0
  let startLeft = 0
  let startTop = 0

  const toolbar = document.createElement('div')
  toolbar.className = 'mermaid-toolbar'
  toolbar.appendChild(btn('＋', '放大', () => { scale = Math.min(scale * 1.25, 5); apply() }))
  toolbar.appendChild(btn('－', '缩小', () => { scale = Math.max(scale / 1.25, 0.1); apply() }))
  toolbar.appendChild(btn('↺', '重置', () => { scale = 1; apply() }))
  toolbar.appendChild(btn('⊡', '适应宽度', () => { const w = host.clientWidth - 32; if (w > 0 && nw > 0) { scale = w / nw; apply() } }))

  const panBtn = btn('✋', '平移模式', () => {
    panEnabled = !panEnabled
    panBtn.classList.toggle('is-active', panEnabled)
    host.classList.toggle('is-pan-enabled', panEnabled)
  })
  toolbar.appendChild(panBtn)

  host.addEventListener('pointerdown', (e) => {
    if (!panEnabled || e.button !== 0 || e.target.closest('.mermaid-toolbar')) return
    dragging = true
    startX = e.clientX
    startY = e.clientY
    startLeft = host.scrollLeft
    startTop = host.scrollTop
    host.classList.add('is-panning')
    host.setPointerCapture(e.pointerId)
    e.preventDefault()
  })

  host.addEventListener('pointermove', (e) => {
    if (!dragging) return
    host.scrollLeft = startLeft - (e.clientX - startX)
    host.scrollTop = startTop - (e.clientY - startY)
  })

  const stopPan = () => {
    dragging = false
    host.classList.remove('is-panning')
  }
  host.addEventListener('pointerup', stopPan)
  host.addEventListener('pointercancel', stopPan)

  host.addEventListener('wheel', (e) => {
    if (e.ctrlKey || e.metaKey) {
      e.preventDefault()
      scale = Math.max(0.1, Math.min(5, scale * (e.deltaY > 0 ? 0.9 : 1.1)))
      apply()
    }
  }, { passive: false })

  host.insertBefore(toolbar, svgEl)
}

const renderMermaid = async (definitions) => {
  if (!definitions.length) return
  initMermaid()
  await nextTick()

  for (const { id, code } of definitions) {
    const host = containerRef.value?.querySelector(`.mermaid-diagram[data-mermaid-id="${id}"]`)
    if (!host) continue
    try {
      const { svg } = await mermaid.render(id, code)
      // mermaid 已内置安全处理，直接使用其输出
      host.innerHTML = svg
      const svgEl = host.querySelector('svg')
      if (svgEl) {
        forceLabelContrast(svgEl)
        setupZoom(host, svgEl)
      }
    } catch (err) {
      console.error('Mermaid 渲染失败:', err)
      host.innerHTML = ''
      const pre = document.createElement('pre')
      pre.className = 'mermaid-error'
      pre.textContent = code
      host.appendChild(pre)
    }
  }
}

watch(
  () => [parsedContent.value, props.rawMode],
  async ([content, rawMode]) => {
    if (rawMode || !content.definitions.length) return
    await nextTick()
    await renderMermaid(content.definitions)
  },
  { immediate: true, deep: true }
)

onBeforeUnmount(() => {
  parsedContent.value = { html: '', definitions: [] }
})
</script>

<style scoped>
.markdown-preview-container {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.markdown-body {
  padding: 18px 20px;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  color: var(--el-text-color-primary);
  line-height: 1.7;
  font-size: 14px;
  overflow: auto;
  max-height: 540px;
}

.markdown-body :deep(pre) {
  background: rgba(0, 0, 0, 0.05);
  padding: 12px;
  border-radius: 6px;
  overflow: auto;
}

.markdown-body :deep(code) {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, Courier, monospace;
}

.markdown-body :deep(h1),
.markdown-body :deep(h2),
.markdown-body :deep(h3),
.markdown-body :deep(h4),
.markdown-body :deep(h5),
.markdown-body :deep(h6) {
  margin-top: 1.2em;
  margin-bottom: 0.6em;
  font-weight: 600;
}

.markdown-body :deep(p) {
  margin: 0.6em 0;
}

.markdown-body :deep(ul),
.markdown-body :deep(ol) {
  padding-left: 24px;
  margin: 0.6em 0;
}

.markdown-body :deep(table) {
  border-collapse: collapse;
  width: 100%;
}

.markdown-body :deep(th),
.markdown-body :deep(td) {
  border: 1px solid var(--el-border-color-lighter);
  padding: 8px;
  text-align: left;
}

.markdown-body :deep(.markdown-empty) {
  color: var(--el-text-color-secondary);
  text-align: center;
  margin: 0;
}

.markdown-body :deep(.mermaid-diagram) {
  position: relative;
  margin: 12px 0;
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  overflow: auto;
  background: var(--el-bg-color-page);
  cursor: default;
}

.markdown-body :deep(.mermaid-diagram.is-pan-enabled) {
  cursor: grab;
}

.markdown-body :deep(.mermaid-diagram.is-panning) {
  cursor: grabbing;
}

.markdown-body :deep(.mermaid-toolbar) {
  position: sticky;
  top: 0;
  left: 0;
  display: flex;
  gap: 4px;
  padding: 4px 6px;
  background: var(--el-bg-color-page);
  border-bottom: 1px solid var(--el-border-color-lighter);
  z-index: 1;
  justify-content: flex-end;
}

.markdown-body :deep(.mermaid-btn) {
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 28px;
  height: 28px;
  padding: 0 4px;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  background: var(--el-fill-color);
  color: var(--el-text-color-regular);
  cursor: pointer;
  font-size: 15px;
  line-height: 1;
  transition: background 0.15s, color 0.15s, border-color 0.15s;
}

.markdown-body :deep(.mermaid-btn.is-active) {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  border-color: var(--el-color-primary-light-5);
}

.markdown-body :deep(.mermaid-btn:hover) {
  background: var(--el-color-primary-light-9);
  color: var(--el-color-primary);
  border-color: var(--el-color-primary-light-5);
}

.markdown-body :deep(.mermaid-diagram svg) {
  display: block;
  padding: 8px;
}

.markdown-body :deep(.mermaid-diagram svg text),
.markdown-body :deep(.mermaid-diagram svg tspan),
.markdown-body :deep(.mermaid-diagram svg .label),
.markdown-body :deep(.mermaid-diagram svg .nodeLabel) {
  fill: unset !important;
  color: unset !important;
}

.markdown-body :deep(.mermaid-diagram svg foreignObject),
.markdown-body :deep(.mermaid-diagram svg foreignObject div),
.markdown-body :deep(.mermaid-diagram svg foreignObject span),
.markdown-body :deep(.mermaid-diagram svg foreignObject p),
.markdown-body :deep(.mermaid-diagram svg .label div) {
  color: unset !important;
  fill: unset !important;
  font-weight: unset;
}

.markdown-body :deep(.mermaid-loading) {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  padding: 8px;
}

.markdown-body :deep(.mermaid-error) {
  margin: 0;
  padding: 8px;
  border-radius: 6px;
  background: rgba(255, 100, 100, 0.12);
  color: var(--el-color-danger);
}

.truncate-tip {
  font-size: 12px;
  color: var(--el-color-primary);
  text-align: center;
}

.markdown-raw {
  padding: 18px 20px;
  background: var(--el-fill-color-lighter);
  border: 1px solid var(--el-border-color-light);
  border-radius: 8px;
  color: var(--el-text-color-primary);
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, Courier, monospace;
  font-size: 13px;
  line-height: 1.6;
  overflow: auto;
  max-height: 540px;
  white-space: pre-wrap;
  word-break: break-word;
  margin: 0;
}
</style>
