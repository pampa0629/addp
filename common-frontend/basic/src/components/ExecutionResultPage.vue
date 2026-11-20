<template>
  <section class="execution-result">
    <header>
      <h2>{{ title }}</h2>
      <MonitorExecutionsButton :module="module" :execution-id="executionId" :return-to="returnTo" scope="execution" />
      <el-button :loading="loading" @click="load">{{ t('common.refresh') }}</el-button>
    </header>
    <p>{{ executionId }}</p>
    <el-alert v-if="failed" type="error" :closable="false" :title="t('common.executionMonitor.resultReadFailed')" />
    <div v-else v-loading="loading" class="result-body">
      <template v-if="result">
        <ExecutionFailure :status="result.status" :error-details="result.error_details" :execution-id="executionId" />
        <slot :execution="result" />
      </template>
    </div>
  </section>
</template>

<script setup>
import { onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import ExecutionFailure from './ExecutionFailure.vue'
import MonitorExecutionsButton from './MonitorExecutionsButton.vue'
import { getBoundAuthStore } from '../composables/useAuth'
import { createLatestRequestScope } from '../utils/latestRequestScope.mjs'

const props = defineProps({
  module: { type: String, required: true },
  title: { type: String, required: true },
  executionId: { type: String, required: true },
  returnTo: { type: [String, Array], default: undefined },
  read: { type: Function, required: true }
})
const { t } = useI18n()
const auth = getBoundAuthStore()
const requests = createLatestRequestScope()
const result = ref(null)
const failed = ref(false)
const loading = ref(false)

async function load() {
  const id = props.executionId
  const ticket = requests.invalidate()
  loading.value = true
  failed.value = false
  result.value = null
  try {
    const response = await requests.request(options => props.read(id, options))
    if (!requests.current(ticket)) return
    if (response?.execution_id === id) result.value = response
    else failed.value = true
  } catch {
    if (requests.current(ticket)) failed.value = true
  } finally {
    if (requests.current(ticket)) loading.value = false
  }
}

watch([() => props.executionId, () => auth.authContext], load, { immediate: true })
onBeforeUnmount(() => requests.dispose())
</script>

<style scoped>
.execution-result { padding: 20px; overflow-wrap: anywhere; }
header { display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
h2 { margin: 0 auto 0 0; }
.result-body { min-height: 80px; }
</style>
