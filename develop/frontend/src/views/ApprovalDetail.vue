<template>
  <div class="approval-page" v-loading="loading">
    <div class="toolbar">
      <div class="toolbar-title">
        <el-icon><Stamp /></el-icon>
        <h2>{{ t('develop.approval.title') }}</h2>
        <el-tag v-if="approval" :type="statusType" size="large">
          {{ statusLabel }}
        </el-tag>
      </div>
      <el-button :disabled="loading" @click="loadApproval">
        <el-icon><Refresh /></el-icon>
        {{ t('develop.approval.refresh') }}
      </el-button>
    </div>

    <el-alert
      v-if="approval"
      :type="alertType"
      :title="statusMessage"
      :closable="false"
      show-icon
      class="status-alert"
    />

    <section v-if="approval" class="approval-content">
      <h3>{{ t('develop.approval.requestInfo') }}</h3>
      <el-descriptions :column="2" border>
        <el-descriptions-item :label="t('develop.approval.toolName')">
          <code>{{ approval.tool_name }}</code>
        </el-descriptions-item>
        <el-descriptions-item :label="t('develop.approval.workflowEngine')">
          {{ approval.request_summary?.workflow_engine_id || '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('develop.approval.taskCount')">
          {{ approval.request_summary?.task_count ?? '-' }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('develop.approval.timeout')">
          {{ formatTimeout(approval.request_summary?.timeout) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('develop.approval.requestedAt')">
          {{ formatTime(approval.requested_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('develop.approval.expiresAt')">
          {{ formatTime(approval.expires_at) }}
        </el-descriptions-item>
        <el-descriptions-item :label="t('develop.approval.fingerprint')" :span="2">
          <code class="fingerprint">{{ approval.request_fingerprint }}</code>
        </el-descriptions-item>
        <el-descriptions-item
          v-if="approval.execution_id"
          :label="t('develop.approval.executionId')"
          :span="2"
        >
          <el-button link type="primary" @click="openExecution">
            {{ approval.execution_id }}
          </el-button>
        </el-descriptions-item>
      </el-descriptions>

      <div v-if="approval.status === 'pending'" class="decision-bar">
        <el-button type="danger" plain :loading="deciding" @click="decide('rejected')">
          <el-icon><Close /></el-icon>
          {{ t('develop.approval.reject') }}
        </el-button>
        <el-button type="primary" :loading="deciding" @click="decide('approved')">
          <el-icon><Check /></el-icon>
          {{ t('develop.approval.approve') }}
        </el-button>
      </div>
    </section>

    <el-result
      v-else-if="!loading"
      icon="error"
      :title="t('develop.approval.notFound')"
      :sub-title="loadError"
    />
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { navigateDevelopRoute } from '@/utils/developNavigation'
import { Check, Close, Refresh, Stamp } from '@element-plus/icons-vue'
import { decideToolApproval, getToolApproval } from '@/api/approval'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const approval = ref(null)
const loading = ref(false)
const deciding = ref(false)
const loadError = ref('')

const statusType = computed(() => ({
  pending: 'warning',
  approved: 'success',
  rejected: 'danger',
  expired: 'info',
  consumed: 'success'
}[approval.value?.status] || 'info'))

const alertType = computed(() => ({
  pending: 'warning',
  approved: 'success',
  rejected: 'error',
  expired: 'info',
  consumed: 'success'
}[approval.value?.status] || 'info'))

const statusLabel = computed(() => t(`develop.approval.status.${approval.value?.status || 'unknown'}`))
const statusMessage = computed(() => t(`develop.approval.message.${approval.value?.status || 'unknown'}`))

const errorMessage = (error) => {
  const apiError = error.response?.data?.error
  return apiError?.message || apiError || error.message || t('develop.approval.loadFailed')
}

const loadApproval = async () => {
  loading.value = true
  loadError.value = ''
  try {
    approval.value = await getToolApproval(route.params.approval_id)
  } catch (error) {
    approval.value = null
    loadError.value = errorMessage(error)
  } finally {
    loading.value = false
  }
}

const decide = async (decision) => {
  const actionLabel = decision === 'approved'
    ? t('develop.approval.approve')
    : t('develop.approval.reject')
  try {
    await ElMessageBox.confirm(
      t('develop.approval.confirmMessage', { action: actionLabel }),
      t('develop.approval.confirmTitle'),
      {
        confirmButtonText: actionLabel,
        cancelButtonText: t('develop.approval.cancel'),
        type: decision === 'approved' ? 'warning' : 'error',
        customClass: 'addp-message-box',
        ...(decision === 'rejected' ? { confirmButtonClass: 'el-button--danger' } : {})
      }
    )
  } catch {
    return
  }

  deciding.value = true
  try {
    approval.value = await decideToolApproval(route.params.approval_id, decision)
    ElMessage.success(t('develop.approval.decisionSuccess'))
  } catch (error) {
    ElMessage.error(errorMessage(error))
    await loadApproval()
  } finally {
    deciding.value = false
  }
}

const formatTime = (value) => value ? new Date(value).toLocaleString() : '-'
const formatTimeout = (value) => value ? t('develop.approval.seconds', { value }) : '-'

const openExecution = () => {
  if (approval.value?.execution_id) {
    navigateDevelopRoute(router, {
      name: 'ExecutionDetail',
      params: { execution_id: approval.value.execution_id }
    })
  }
}

onMounted(loadApproval)
</script>

<style scoped>
.approval-page {
  min-height: 100%;
  padding: 20px;
  background: var(--addp-bg-secondary);
}

.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 16px;
}

.toolbar-title {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.toolbar-title h2 {
  margin: 0;
  font-size: 20px;
  color: var(--addp-text-primary);
}

.status-alert {
  margin-bottom: 16px;
}

.approval-content {
  padding: 20px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-primary);
}

.approval-content h3 {
  margin: 0 0 16px;
  font-size: 16px;
  color: var(--addp-text-primary);
}

.fingerprint {
  overflow-wrap: anywhere;
}

.decision-bar {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 20px;
}

@media (max-width: 720px) {
  .approval-page {
    padding: 12px;
  }

  .toolbar {
    align-items: flex-start;
  }

  .approval-content {
    padding: 12px;
  }
}
</style>
