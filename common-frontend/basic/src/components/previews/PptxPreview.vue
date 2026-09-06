<template>
  <div class="pptx-preview">
    <PdfPreview v-if="pdfData" :data="pdfData" />
    <div v-else class="pptx-state">
      <el-icon v-if="loading" class="is-loading"><Loading /></el-icon>
      <el-icon v-else-if="error" class="pptx-error"><WarningFilled /></el-icon>
      <el-icon v-else><Document /></el-icon>
      <strong>{{ stateTitle }}</strong>
      <span v-if="loading">{{ t('pptxPreview.convertingHint') }}</span>
      <el-alert v-if="error" type="error" :closable="false" :title="error" />
      <el-button v-if="error" type="primary" @click="retryPreview">
        <el-icon><RefreshRight /></el-icon>
        {{ t('common.refresh') }}
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Document, Loading, RefreshRight, WarningFilled } from '@element-plus/icons-vue'
import { getAccessToken } from '../../auth/authSession'
import PdfPreview from './PdfPreview.vue'

const props = defineProps({
  data: { type: Object, required: true },
  source: { type: Object, default: null }
})

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const preview = ref(null)
let loadToken = 0
let pollTimer = 0

const stateTitle = computed(() => error.value ? t('pptxPreview.failed') : t('pptxPreview.converting'))
const pdfData = computed(() => {
  if (!preview.value?.preview_url) return null
  return {
    object: {
      path: String(props.source?.name || 'presentation.pptx').replace(/\.pptx$/i, '.pdf'),
      size_bytes: preview.value.size_bytes || 0,
      content_type: 'application/pdf',
      content: {
        kind: 'pdf',
        url: preview.value.preview_url,
        preview_url: preview.value.preview_url,
        metadata: { content_type: 'application/pdf', page_count: preview.value.page_count || 0 }
      }
    }
  }
})

const authHeaders = () => {
  const token = getAccessToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

const requestJSON = async (url, options = {}) => {
  const response = await fetch(url, {
    credentials: 'include',
    ...options,
    headers: { Accept: 'application/json', ...authHeaders(), ...(options.headers || {}) }
  })
  const payload = await response.json().catch(() => ({}))
  if (!response.ok) throw new Error(payload.error || `${response.status} ${response.statusText}`)
  return payload.data || payload
}

const pollExecution = async (executionID, token) => {
  if (!executionID || token !== loadToken) return
  try {
    const execution = await requestJSON(`/api/v1/manager/executions/${encodeURIComponent(executionID)}`)
    if (token !== loadToken) return
    if (execution.status === 'success') {
      await resolvePreview(token)
      return
    }
    if (['failed', 'timeout', 'cancelled'].includes(execution.status)) {
      loading.value = false
      error.value = execution.error_details?.message || t('pptxPreview.failed')
      return
    }
    pollTimer = window.setTimeout(() => pollExecution(executionID, token), 1500)
  } catch (err) {
    if (token === loadToken) {
      loading.value = false
      error.value = err.message || t('pptxPreview.failed')
    }
  }
}

const resolvePreview = async (existingToken, retry = false) => {
  const token = typeof existingToken === 'number' ? existingToken : ++loadToken
  window.clearTimeout(pollTimer)
  preview.value = null
  error.value = ''
  loading.value = true
  const source = props.source || {}
  if (!source.locator) {
    loading.value = false
    error.value = t('pptxPreview.identityMissing')
    return
  }
  try {
    const result = await requestJSON('/api/v1/manager/pptx_pdf/preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ locator: source.locator, ...(retry ? { retry: true } : {}) })
    })
    if (token !== loadToken) return
    if (result.status === 'ready' && result.preview_url) {
      preview.value = result
      loading.value = false
      return
    }
    if (result.status === 'failed') {
      loading.value = false
      error.value = result.error || t('pptxPreview.failed')
      return
    }
    if (!result.execution_id) {
      throw new Error(t('pptxPreview.failed'))
    }
    pollTimer = window.setTimeout(() => pollExecution(result.execution_id, token), 800)
  } catch (err) {
    if (token === loadToken) {
      loading.value = false
      error.value = err.message || t('pptxPreview.failed')
    }
  }
}

const retryPreview = () => {
  const token = ++loadToken
  resolvePreview(token, true)
}

watch(() => props.source?.locator, () => resolvePreview(), { immediate: true })
onUnmounted(() => {
  loadToken += 1
  window.clearTimeout(pollTimer)
})
</script>

<style scoped>
.pptx-preview {
  width: 100%;
  height: 100%;
  min-height: 420px;
  background: var(--addp-bg-secondary);
}

.pptx-state {
  height: 100%;
  min-height: 420px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  color: var(--addp-text-secondary);
}

.pptx-state > .el-icon {
  font-size: 42px;
  color: var(--el-color-primary);
}

.pptx-state > .pptx-error {
  color: var(--el-color-danger);
}

.pptx-state .el-alert {
  width: min(560px, calc(100% - 48px));
}
</style>
