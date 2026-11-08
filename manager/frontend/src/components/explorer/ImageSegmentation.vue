<template>
  <el-button size="small" @click="open">{{ t('manager.segment.title') }}</el-button>
  <el-dialog
    v-model="visible"
    :title="t('manager.segment.title')"
    width="min(94vw, 1100px)"
    destroy-on-close
    @close="close"
  >
    <div class="segment-tools">
      <el-radio-group v-model="mode" size="small">
        <el-radio-button
          v-for="key in ['point', 'box', 'positive', 'negative', 'window']"
          :key="key"
          :value="key"
          >{{ t('manager.segment.' + key) }}</el-radio-button
        >
      </el-radio-group>
      <el-input
        v-model="concept"
        :aria-label="t('manager.segment.concept')"
        :placeholder="t('manager.segment.conceptExample')"
        maxlength="128"
        @keyup.enter="find"
      />
      <el-button :disabled="!concept.trim() || !frame" @click="find">{{
        t('manager.segment.find')
      }}</el-button>
      <el-button :disabled="!frame" @click="prepare(null)">{{ t('manager.segment.full') }}</el-button>
    </div>
    <el-alert v-if="error" type="error" :title="error" :closable="false" />
    <p role="status" aria-live="polite">{{ busy ? t('manager.segment.working') : status }}</p>
    <canvas
      ref="canvas"
      v-show="frame"
      :aria-label="t('manager.segment.title')"
      @pointerdown="start"
      @pointerup="end"
      @pointercancel="anchor = null"
    />
  </el-dialog>
</template>
<script setup>
import { nextTick, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import client from '@/api/client'
const props = defineProps({ locator: { type: String, required: true } })
const { t } = useI18n()
const visible = ref(false),
  canvas = ref(null),
  mode = ref('point'),
  concept = ref(''),
  error = ref(''),
  busy = ref(false),
  status = ref('')
const frame = shallowRef(null)
let anchor = null,
  points = [],
  labels = [],
  sequence = 0
const call = (body) => client.post('/manager/image-segmentation', body, { timeout: 120000 })
function release(f) {
  if (f)
    void call({ action: 'release', locator: f.locator, window: f.inputWindow, session_id: f.sid }).catch(
      () => {},
    )
}
function close() {
  sequence++
  release(frame.value)
  frame.value = null
  anchor = null
  points = []
  labels = []
  busy.value = false
  status.value = ''
}
watch(
  () => props.locator,
  () => {
    visible.value = false
    close()
  },
)
onBeforeUnmount(close)
// Measure two frames so the draw preceding the first callback can reach the screen.
function measurePaint(name, start, n) {
  requestAnimationFrame(() =>
    requestAnimationFrame(() => {
      if (sequence === n && visible.value)
        performance.measure('manager.segmentation.' + name, { start, end: performance.now() })
    }),
  )
}
function bitmap(base64) {
  const img = new Image()
  img.src = 'data:image/png;base64,' + base64
  return img.decode().then(() => img)
}
function open() {
  visible.value = true
  mode.value = 'point'
  concept.value = ''
  void prepare(null)
}
async function prepare(window) {
  const started = performance.now(),
    n = ++sequence,
    locator = props.locator
  release(frame.value)
  frame.value = null
  error.value = ''
  busy.value = true
  status.value = ''
  points = []
  labels = []
  try {
    const r = await call({ action: 'prepare', locator, window })
    const f = { locator, inputWindow: window, window: r.window, sid: r.session_id }
    if (n !== sequence) {
      release(f)
      return
    }
    f.image = await bitmap(r.image)
    if (n !== sequence) {
      release(f)
      return
    }
    frame.value = f
    await nextTick()
    if (n !== sequence || !canvas.value) return
    canvas.value.width = f.image.width
    canvas.value.height = f.image.height
    canvas.value.getContext('2d').drawImage(f.image, 0, 0)
    mode.value = 'point'
    measurePaint(window ? 'window' : 'open', started, n)
  } catch (e) {
    if (n === sequence) error.value = e?.response?.data?.error || e.message
  } finally {
    if (n === sequence) busy.value = false
  }
}
function xy(e) {
  const c = canvas.value,
    b = c.getBoundingClientRect()
  return [
    Math.max(0, Math.min(c.width - 1, Math.floor(((e.clientX - b.left) * c.width) / b.width))),
    Math.max(0, Math.min(c.height - 1, Math.floor(((e.clientY - b.top) * c.height) / b.height))),
  ]
}
function start(e) {
  if (!frame.value || e.button !== 0) return
  anchor = xy(e)
  e.currentTarget.setPointerCapture(e.pointerId)
}
function end(e) {
  if (!anchor || !frame.value) return
  const a = anchor,
    b = xy(e)
  anchor = null
  if (mode.value === 'window' || mode.value === 'box') {
    const x = Math.min(a[0], b[0]),
      y = Math.min(a[1], b[1]),
      x2 = Math.max(a[0], b[0]),
      y2 = Math.max(a[1], b[1])
    if (x === x2 || y === y2) return
    if (mode.value === 'window') {
      const f = frame.value,
        w = f.window
      const left = Math.floor((x * w.width) / f.image.width),
        top = Math.floor((y * w.height) / f.image.height)
      void prepare({
        x: w.x + left,
        y: w.y + top,
        width: Math.ceil(((x2 + 1) * w.width) / f.image.width) - left,
        height: Math.ceil(((y2 + 1) * w.height) / f.image.height) - top,
      })
      return
    }
    points = [
      [x, y],
      [x2, y2],
    ]
    labels = [2, 3]
  } else {
    if (mode.value === 'point') {
      points = []
      labels = []
    }
    if (points.length >= 64) return
    points = [...points, b]
    labels = [...labels, mode.value === 'negative' ? 0 : 1]
  }
  void predict({ points, labels })
}
function find() {
  if (frame.value && concept.value.trim()) {
    points = []
    labels = []
    void predict({ text: concept.value.trim() })
  }
}
async function predict(prompt) {
  const started = performance.now(),
    f = frame.value,
    n = ++sequence
  busy.value = true
  error.value = ''
  try {
    const r = await call({
      action: 'prompt',
      locator: f.locator,
      window: f.inputWindow,
      session_id: f.sid,
      prompt,
    })
    if (n !== sequence) return
    const masks = await Promise.all((r.masks || []).map(async (m) => ({ ...m, img: await bitmap(m.png) })))
    if (n !== sequence) return
    const ctx = canvas.value.getContext('2d')
    ctx.drawImage(f.image, 0, 0)
    const color = getComputedStyle(canvas.value).getPropertyValue('--el-color-primary').trim()
    for (const m of masks) {
      const tile = document.createElement('canvas')
      tile.width = m.width
      tile.height = m.height
      const tc = tile.getContext('2d')
      tc.drawImage(m.img, 0, 0)
      const rgba = tc.getImageData(0, 0, m.width, m.height)
      for (let i = 0; i < rgba.data.length; i += 4) rgba.data[i + 3] = rgba.data[i]
      tc.putImageData(rgba, 0, 0)
      tc.globalCompositeOperation = 'source-in'
      tc.fillStyle = color
      tc.fillRect(0, 0, m.width, m.height)
      ctx.globalAlpha = 0.48
      ctx.drawImage(tile, m.x, m.y)
      ctx.globalAlpha = 1
    }
    status.value = t('manager.segment.count', { n: masks.length })
    measurePaint(
      prompt.text
        ? 'text'
        : prompt.labels.includes(2)
          ? 'box'
          : prompt.labels.length > 1
            ? 'refine'
            : 'point',
      started,
      n,
    )
  } catch (e) {
    if (n === sequence) error.value = e?.response?.data?.error || e.message
  } finally {
    if (n === sequence) busy.value = false
  }
}
</script>
<style scoped>
.segment-tools {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.segment-tools .el-input {
  width: 12em;
}
canvas {
  max-width: 100%;
  max-height: 64vh;
  touch-action: none;
  cursor: crosshair;
  display: block;
  margin: auto;
}
</style>
