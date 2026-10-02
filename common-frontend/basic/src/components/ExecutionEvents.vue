<template>
  <section class="execution-events" :aria-label="t('common.executionEvents.title')">
    <h4>{{ t('common.executionEvents.title') }}</h4>
    <p>{{ t('common.executionEvents.retention') }}</p>
    <p v-if="error" role="alert">{{ t('common.executionEvents.unavailable') }}</p>
    <p v-else-if="!items.length && !loading">{{ t('common.executionEvents.unrecorded') }}</p>
    <div v-if="items.length" class="event-scroll">
      <table><thead><tr>
        <th>{{ t('common.executionEvents.time') }}</th>
        <th>{{ t('common.executionEvents.attempt') }}</th>
        <th>{{ t('common.executionEvents.kind') }}</th>
        <th>{{ t('common.executionSteps.step') }}</th>
        <th>{{ t('common.executionEvents.counters') }}</th>
      </tr></thead><tbody>
        <tr v-for="event in items" :key="event.id">
          <td>{{ new Date(event.occurred_at).toLocaleString() }}</td>
          <td>{{ event.attempt }}</td>
          <td>{{ t(`common.executionEvents.kinds.${event.kind}`) }}</td>
          <td>{{ event.step_id || '—' }}</td>
          <td><span v-for="(value, key) in event.counters" :key="key">{{ t(`common.executionEvents.fields.${key}`) }}: {{ value }} </span></td>
        </tr>
      </tbody></table>
    </div>
    <p v-if="truncated" role="status">{{ t('common.executionEvents.truncated') }}</p>
    <el-button :loading="loading" @click="reload">{{ t('common.executionEvents.refresh') }}</el-button>
    <el-button v-if="hasMore && items.length < 1000" :disabled="loading" @click="loadNext">{{ t('common.executionEvents.more') }}</el-button>
  </section>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
const props = defineProps({
  executionId: { type: String, required: true },
  loadPage: { type: Function, required: true },
  running: { type: Boolean, default: false }
})
const { t } = useI18n()
const items = ref([]), hasMore = ref(false), loading = ref(false), error = ref(false)
const cursor = ref(0)
let generation = 0, timer
const truncated = computed(() => items.value.some(item => item.kind === 'truncated') || (hasMore.value && items.value.length >= 1000))
function stopTimer() { clearTimeout(timer); timer = undefined }
async function loadNext() {
  if (loading.value || items.value.length >= 1000) return
  stopTimer()
  const current = generation
  loading.value = true
  try {
    const page = await props.loadPage(props.executionId, { after: cursor.value, limit: 100 })
    if (current !== generation) return
    const existing = new Set(items.value.map(item => item.id))
    items.value = [...items.value, ...page.items.filter(item => !existing.has(item.id))]
      .filter(item => new Date(item.occurred_at) >= new Date(page.retained_after)).slice(0, 1000)
    cursor.value = page.next_cursor || cursor.value
    hasMore.value = page.has_more
    error.value = false
  } catch {
    if (current !== generation) return
    items.value = []; cursor.value = 0; hasMore.value = false; error.value = true
  } finally {
    if (current === generation) {
      loading.value = false
      if (props.running && !error.value && !hasMore.value && items.value.length < 1000) timer = setTimeout(loadNext, 5000)
    }
  }
}
function reload() {
  generation++; stopTimer()
  items.value = []; cursor.value = 0; hasMore.value = false; error.value = false; loading.value = false
  loadNext()
}
watch(() => props.executionId, reload, { immediate: true })
watch(() => props.running, running => {
  stopTimer()
  if (running && !error.value && !hasMore.value && !loading.value) timer = setTimeout(loadNext, 5000)
  if (!running && !loading.value && !error.value && !hasMore.value) loadNext()
})
onBeforeUnmount(() => { generation++; stopTimer() })
</script>

<style scoped>
.event-scroll { max-height: 360px; overflow: auto; max-width: 100%; }
table { width: 100%; border-collapse: collapse; font-size: var(--el-font-size-base); }
th, td { text-align: left; vertical-align: top; padding: 8px; border-bottom: 1px solid var(--el-border-color); overflow-wrap: anywhere; }
th { white-space: nowrap; }
td:first-child { min-width: 145px; }
p { color: var(--el-text-color-secondary); }
</style>
