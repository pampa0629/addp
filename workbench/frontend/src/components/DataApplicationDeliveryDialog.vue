<template>
  <el-dialog
    v-model="visible"
    class="addp-dialog"
    :title="t('workbench.deliveryTitle')"
    width="min(680px, calc(100vw - 24px))"
    @opened="focusClose"
    @close="invalidateRequest"
    @closed="clear"
  >
    <div v-loading="loading" class="delivery-content">
      <template v-if="runtime">
        <div class="delivery-header">
          <strong>{{ runtime.name }}</strong>
          <p>{{ t('workbench.deliveryHint', { revision: runtime.revision_number }) }}</p>
        </div>
        <div class="delivery-list">
          <div v-for="item in deliveryItems" :key="item.presetKey || 'default'" class="delivery-item">
            <div class="delivery-identity">
              <strong>{{ item.name }}</strong>
              <code v-if="item.presetKey">{{ item.presetKey }}</code>
            </div>
            <div class="delivery-actions">
              <el-button @click="run(item)">{{ t('workbench.run') }}</el-button>
              <el-button type="primary" @click="copyLink(item)">{{ t('workbench.copyLink') }}</el-button>
            </div>
          </div>
        </div>
      </template>
    </div>
    <template #footer>
      <el-button ref="closeButton" @click="visible = false">{{ t('common.close') }}</el-button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { createLatestRequestCoordinator } from '@common-ui'
import { getDataApplicationRuntime } from '../api/dataApplications'
import { dataApplicationDeliveryContext, dataApplicationDeliveryItems } from '../utils/dataApplicationDelivery.mjs'
import { commitLatestDataApplicationRequest } from '../utils/dataApplicationDraft.mjs'
import { dataApplicationRuntimeURL, openDataApplicationRuntime } from '../utils/moduleNavigation'

const { t } = useI18n()
const visible = ref(false)
const loading = ref(false)
const closeButton = ref(null)
const target = ref(null)
const runtime = ref(null)
const requests = createLatestRequestCoordinator()
const deliveryItems = computed(() => dataApplicationDeliveryItems(runtime.value, t('workbench.defaultScenario')))

async function open(applicationID, revisionNumber) {
  const nextTarget = { id: String(applicationID || '').trim(), revisionNumber }
  if (!nextTarget.id) return false
  const context = dataApplicationDeliveryContext(nextTarget.id, nextTarget.revisionNumber)
  const request = requests.begin(context)
  target.value = nextTarget
  runtime.value = null
  visible.value = true
  loading.value = true
  try {
    const { data } = await getDataApplicationRuntime(nextTarget.id)
    return commit(request, context, () => { runtime.value = data })
  } catch (error) {
    commit(request, context, () => {
      visible.value = false
      ElMessage.error(error?.response?.data?.error || t('workbench.loadDeliveryFailed'))
    })
    return false
  } finally {
    commit(request, context, () => { loading.value = false })
  }
}

function commit(request, context, action) {
  return commitLatestDataApplicationRequest(requests, request, context, action)
}

function run(item) {
  if (!target.value) return false
  return openDataApplicationRuntime(target.value.id, item.presetKey)
}

async function copyLink(item) {
  if (!target.value) return
  try {
    await navigator.clipboard.writeText(dataApplicationRuntimeURL(target.value.id, item.presetKey))
    ElMessage.success(t('workbench.linkCopied'))
  } catch {
    ElMessage.error(t('workbench.linkCopyFailed'))
  }
}

function invalidateRequest() {
  requests.invalidate()
  loading.value = false
}

function focusClose() {
  closeButton.value?.$el?.focus()
}

function clear() {
  target.value = null
  runtime.value = null
}

defineExpose({ open })
onBeforeUnmount(invalidateRequest)
</script>

<style scoped>
.delivery-content{min-height:120px}.delivery-header p{margin:6px 0 0;color:var(--addp-text-secondary)}.delivery-list{display:flex;flex-direction:column;gap:8px;margin-top:16px}.delivery-item{display:flex;align-items:center;justify-content:space-between;gap:16px;padding:12px 16px;background:var(--addp-bg-secondary);border:1px solid var(--addp-border-color);border-radius:8px}.delivery-identity{display:flex;align-items:center;gap:8px;min-width:0}.delivery-identity strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.delivery-identity code{color:var(--addp-text-secondary)}.delivery-actions{display:flex;flex-shrink:0;gap:8px}@media (max-width:768px){.delivery-item{align-items:stretch;flex-direction:column}.delivery-actions{justify-content:flex-end}}
</style>
