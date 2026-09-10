<template>
  <div class="runtime-page" v-loading="loading" data-testid="data-application-runtime">
    <div v-if="pageError" class="runtime-error">
      <el-alert type="error" :closable="false" :title="pageError" />
      <el-button data-testid="runtime-retry-action" type="primary" :loading="loading" @click="load(route.params.id)">{{ t('workbench.retry') }}</el-button>
    </div>
    <DataApplicationCanvas v-else-if="application" :key="`${application.id}:${initialPresetKey}`" :application="application" :initial-preset-key="initialPresetKey" />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { createLatestRequestCoordinator } from '@common-ui'
import { getDataApplicationRuntime } from '../api/dataApplications'
import { applicationParameterPreset, commitLatestDataApplicationLoad } from '../utils/dataApplicationRuntime.mjs'
import DataApplicationCanvas from '../components/DataApplicationCanvas.vue'

const { t } = useI18n()
const route = useRoute()
const loading = ref(false)
const pageError = ref('')
const application = ref(null)
const requests = createLatestRequestCoordinator()
const initialPresetKey = computed(() => applicationParameterPreset(application.value?.snapshot, route.query.preset)?.key || '')

async function load(applicationID) {
  const targetID = String(applicationID || '').trim()
  const request = requests.begin(targetID)
  loading.value = true
  pageError.value = ''
  application.value = null
  try {
    const { data } = await getDataApplicationRuntime(targetID)
    commitLatestDataApplicationLoad(requests, request, route.params.id, () => {
      application.value = data
    })
  } catch (error) {
    commitLatestDataApplicationLoad(requests, request, route.params.id, () => {
      pageError.value = error?.response?.data?.error || t('workbench.loadFailed')
    })
  } finally {
    commitLatestDataApplicationLoad(requests, request, route.params.id, () => {
      loading.value = false
    })
  }
}

onBeforeUnmount(() => requests.invalidate())
watch(() => route.params.id, load, { immediate: true })
</script>

<style scoped>
.runtime-page{min-height:100vh;background:var(--addp-bg-secondary)}.runtime-error{display:flex;flex-direction:column;align-items:flex-start;gap:12px;padding:24px}.runtime-error>.el-alert{width:100%}
</style>
