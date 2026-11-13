<template>
  <section class="execution-trace-stages" :aria-label="t(`${prefix}.title`)">
    <div class="stage-heading">
      <h4>{{ t(`${prefix}.title`) }}</h4>
      <el-button :aria-expanded="opened" @click="toggle">
        {{ t(`${prefix}.${opened ? 'close' : 'open'}`) }}
      </el-button>
    </div>
    <div v-if="opened" aria-live="polite" :aria-busy="loading">
      <p v-if="loading">{{ t(`${prefix}.loading`) }}</p>
      <el-alert v-else-if="failed" :title="t(`${prefix}.failed`)" type="warning" :closable="false" />
      <template v-else-if="result">
        <p class="stage-hint">{{ t(`${prefix}.hint`) }}</p>
        <p v-if="!result.supported">{{ t(`${prefix}.unsupported`) }}</p>
        <div v-else class="stage-grid">
          <article v-for="stage in result.stages" :key="stage.slot" class="stage-card">
            <h5>{{ t(`${prefix}.slots.${stage.slot}`) }}</h5>
            <template v-if="stage.state === 'available'">
              <dl>
                <dt>{{ t(`${prefix}.phase`) }}</dt><dd>{{ t(`${prefix}.phases.${stage.phase}`) }}</dd>
                <dt>{{ t(`${prefix}.attempt`) }}</dt><dd>{{ stage.attempt }}</dd>
                <template v-if="stage.outcome">
                  <dt>{{ t(`${prefix}.outcome`) }}</dt><dd>{{ t(`${prefix}.outcomes.${stage.outcome}`) }}</dd>
                </template>
                <dt>{{ t(`${prefix}.start`) }}</dt><dd>{{ formatTime(stage.start) }}</dd>
                <dt>{{ t(`${prefix}.end`) }}</dt><dd>{{ formatTime(stage.end) }}</dd>
                <dt>{{ t(`${prefix}.duration`) }}</dt><dd>{{ stage.duration_ms }} ms</dd>
              </dl>
            </template>
            <p v-else>{{ t(`${prefix}.states.${stage.state}`) }}</p>
          </article>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/store/auth'
import { getExecutionTraceStages } from '@/api/monitor'

const props = defineProps({ executionId: { type: String, required: true } })
const emit = defineEmits(['read-blocked'])
const { t, locale } = useI18n()
const auth = useAuthStore()
const prefix = 'monitor.execution.traceStages'
const opened = ref(false)
const loading = ref(false)
const failed = ref(false)
const result = ref(null)
let generation = 0
let controller

function clear() {
  generation++
  controller?.abort()
  controller = null
  opened.value = false
  loading.value = false
  failed.value = false
  result.value = null
}

async function toggle() {
  if (opened.value) { clear(); return }
  clear()
  opened.value = true
  loading.value = true
  const request = generation
  const id = props.executionId
  controller = new AbortController()
  try {
    const data = await getExecutionTraceStages(id, { signal: controller.signal })
    if (request !== generation) return
    result.value = data
  } catch (error) {
    if (request !== generation) return
    result.value = null
    failed.value = true
    if ([401, 403, 404].includes(error.response?.status) ||
      (error.response?.status === 503 && error.response?.data?.error_code === 'execution_owner_unavailable')) {
      clear()
      emit('read-blocked')
    }
  } finally {
    if (request === generation) loading.value = false
  }
}

function formatTime(value) {
  return value ? new Date(value).toLocaleString(locale.value) : '—'
}

watch(() => [props.executionId, auth.user?.id, auth.authContext?.context?.type,
  auth.authContext?.context?.tenant_id, auth.permissions.join('|')], clear, { flush: 'sync' })
onBeforeUnmount(clear)
</script>

<style scoped>
.execution-trace-stages { margin: 20px 0; color: var(--addp-text-primary); }
.stage-heading { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 12px; }
.stage-heading h4 { margin: 0; }
.stage-hint { color: var(--addp-text-secondary); }
.stage-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 260px), 1fr)); gap: 12px; }
.stage-card { min-width: 0; padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-secondary); }
.stage-card h5 { margin: 0 0 12px; font-size: inherit; }
.stage-card dl { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 8px 16px; margin: 0; }
.stage-card dt { color: var(--addp-text-secondary); }
.stage-card dd { margin: 0; overflow-wrap: anywhere; }
</style>
