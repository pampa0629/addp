<template>
  <div class="my-applications" v-loading="loading">
    <div class="page-header">
      <h2 class="page-title">{{ t('portal.myApplications.title') }}</h2>
    </div>

    <!-- 状态筛选 -->
    <div class="filter-bar">
      <el-radio-group v-model="displayStatus" @change="applyFilter">
        <el-radio-button value="">{{ t('portal.myApplications.all') }}</el-radio-button>
        <el-radio-button value="pending">{{ t('portal.myApplications.pending') }}</el-radio-button>
        <el-radio-button value="authorized">{{ t('portal.myApplications.authorized') }}</el-radio-button>
        <el-radio-button value="expired">{{ t('portal.myApplications.expired') }}</el-radio-button>
        <el-radio-button value="revoked">{{ t('portal.myApplications.revoked') }}</el-radio-button>
        <el-radio-button value="rejected">{{ t('portal.myApplications.rejected') }}</el-radio-button>
      </el-radio-group>
    </div>

    <el-empty v-if="!loading && filteredApplications.length === 0" :description="t('portal.myApplications.noRecords')" />

    <div v-else class="application-list">
      <el-card
        v-for="app in filteredApplications"
        :key="app.id"
        class="application-card"
        shadow="never"
      >
        <div class="card-body">
          <div class="card-main">
            <div class="asset-name-row">
              <router-link
                :to="`/portal/assets/${app.asset_id}`"
                class="asset-link"
              >{{ app.asset_name || t('portal.myApplications.assetFallback', { id: app.asset_id }) }}</router-link>
              <el-tag :type="DISPLAY_STATUS_CONFIG[deriveDisplayStatus(app)]?.type" size="small">
                {{ DISPLAY_STATUS_CONFIG[deriveDisplayStatus(app)]?.label }}
              </el-tag>
            </div>
            <div class="reason-row" v-if="app.reason">
              <span class="label">{{ t('portal.myApplications.reasonLabel') }}</span>
              <span class="value">{{ app.reason }}</span>
            </div>
            <div class="reject-reason" v-if="deriveDisplayStatus(app) === 'rejected' && app.review_note">
              <el-icon color="var(--el-color-danger)"><WarningFilled /></el-icon>
              <span>{{ t('portal.myApplications.rejectReasonLabel') }}{{ app.review_note }}</span>
            </div>
            <div class="revoked-tip" v-if="deriveDisplayStatus(app) === 'revoked'">
              <el-icon color="var(--el-text-color-placeholder)"><InfoFilled /></el-icon>
              <span>{{ t('portal.myApplications.revokedTip') }}</span>
            </div>
          </div>
          <div class="card-footer">
            <div class="card-meta">
              <span>{{ t('portal.myApplications.durationDays', { count: app.duration_day }) }}</span>
              <span v-if="app.auth_expires_at" :class="{ 'expired-text': isExpired(app.auth_expires_at) }">
                {{ t('portal.myApplications.expiresLabel') }}{{ formatDate(app.auth_expires_at) }}
              </span>
              <span>{{ formatDate(app.created_at) }}</span>
            </div>
            <!-- 已授权：查看使用方式 -->
            <el-button
              v-if="deriveDisplayStatus(app) === 'authorized'"
              type="primary"
              size="small"
              plain
              @click="openUsageDialog(app)"
            >{{ t('portal.myApplications.viewUsage') }}</el-button>
            <!-- 已过期：重新申请 -->
            <router-link
              v-else-if="deriveDisplayStatus(app) === 'expired'"
              :to="`/portal/assets/${app.asset_id}`"
            >
              <el-button type="warning" size="small" plain>{{ t('portal.myApplications.reapply') }}</el-button>
            </router-link>
          </div>
        </div>
      </el-card>
    </div>

    <!-- 使用方式弹窗 -->
    <el-dialog
      v-model="usageDialogVisible"
      :title="t('portal.myApplications.usageDialogTitle', { name: currentApp?.asset_name })"
      width="600px"
      destroy-on-close
    >
      <div v-loading="endpointsLoading" class="usage-dialog-body">
        <template v-if="!endpointsLoading">
          <!-- 有效期提示 -->
          <el-alert
            v-if="currentApp?.auth_expires_at"
            :title="t('portal.myApplications.authValidUntil', { date: formatDate(currentApp.auth_expires_at) })"
            type="success"
            :closable="false"
            style="margin-bottom: 16px"
          />

          <!-- 数据服务类型：显示端点 -->
          <template v-if="currentEndpoints && Object.keys(currentEndpoints.endpoints || {}).length > 0">
            <div class="endpoints-section">
              <div class="section-title">{{ currentEndpoints.title || t('portal.myApplications.serviceEndpoints') }}</div>
              <div
                v-for="(url, proto) in currentEndpoints.endpoints"
                :key="proto"
                class="endpoint-item"
              >
                <span class="endpoint-proto">{{ proto }}</span>
                <el-input
                  :value="url"
                  readonly
                  size="small"
                  class="endpoint-url"
                >
                  <template #append>
                    <el-button @click="copyText(url)" :icon="CopyDocument" />
                  </template>
                </el-input>
              </div>
            </div>
          </template>

          <!-- 非数据服务类型：通用说明 -->
          <template v-else>
            <el-result
              icon="success"
              :title="t('portal.myApplications.authActive')"
              :sub-title="t('portal.myApplications.authActiveDesc')"
            >
              <template #extra>
                <router-link :to="`/portal/assets/${currentApp?.asset_id}`" @click="usageDialogVisible = false">
                  <el-button type="primary">{{ t('portal.myApplications.goToDetail') }}</el-button>
                </router-link>
              </template>
            </el-result>
          </template>
        </template>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { WarningFilled, InfoFilled, CopyDocument } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { formatDate } from '@common-ui'
import { myApplicationAPI, assetAPI } from '../api/portal'

const { t } = useI18n()
const loading = ref(false)
const displayStatus = ref('')
const applications = ref([])

const usageDialogVisible = ref(false)
const endpointsLoading = ref(false)
const currentApp = ref(null)
const currentEndpoints = ref(null)

const DISPLAY_STATUS_CONFIG = computed(() => ({
  pending:    { label: t('portal.myApplications.pending'), type: 'warning' },
  authorized: { label: t('portal.myApplications.authorized'), type: 'success' },
  expired:    { label: t('portal.myApplications.expired'), type: 'info' },
  revoked:    { label: t('portal.myApplications.revoked'), type: 'info' },
  rejected:   { label: t('portal.myApplications.rejected'), type: 'danger' },
}))

function deriveDisplayStatus(app) {
  if (app.status === 'pending') return 'pending'
  if (app.status === 'rejected') return 'rejected'
  if (app.status === 'approved') {
    if (app.auth_is_active === false) return 'revoked'
    if (app.auth_expires_at && new Date(app.auth_expires_at) <= new Date()) return 'expired'
    return 'authorized'
  }
  return 'pending'
}

function isExpired(dateStr) {
  return dateStr && new Date(dateStr) <= new Date()
}

const filteredApplications = computed(() => {
  if (!displayStatus.value) return applications.value
  return applications.value.filter(a => deriveDisplayStatus(a) === displayStatus.value)
})

function applyFilter() {}

async function openUsageDialog(app) {
  currentApp.value = app
  currentEndpoints.value = null
  usageDialogVisible.value = true
  endpointsLoading.value = true
  try {
    const data = await assetAPI.getEndpoints(app.asset_id)
    currentEndpoints.value = data
  } catch (err) {
    currentEndpoints.value = { endpoints: {} }
  } finally {
    endpointsLoading.value = false
  }
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success(t('portal.common.copied'))
  } catch {
    ElMessage.error(t('portal.common.copyFailed'))
  }
}

async function fetchApplications() {
  loading.value = true
  try {
    const data = await myApplicationAPI.list()
    applications.value = data || []
  } catch (err) {
    console.error('获取申请列表失败:', err)
    applications.value = []
  } finally {
    loading.value = false
  }
}

onMounted(fetchApplications)
</script>

<style scoped>
.my-applications {
  padding: 24px;
  max-width: 860px;
  margin: 0 auto;
}

.page-header {
  margin-bottom: 20px;
}

.page-title {
  font-size: 20px;
  font-weight: 700;
  margin: 0;
  color: var(--el-text-color-primary);
}

.filter-bar {
  margin-bottom: 20px;
}

.application-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.application-card {
  border-radius: 8px;
}

.card-body {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.card-main {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.asset-name-row {
  display: flex;
  align-items: center;
  gap: 10px;
}

.asset-link {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-color-primary);
  text-decoration: none;
}

.asset-link:hover {
  text-decoration: underline;
}

.reason-row {
  font-size: 13px;
  color: var(--el-text-color-regular);
}

.reason-row .label {
  color: var(--el-text-color-secondary);
}

.reject-reason {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-color-danger);
  background: var(--el-color-danger-light-9);
  padding: 6px 10px;
  border-radius: 4px;
}

.revoked-tip {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-text-color-placeholder);
}

.card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-top: 1px solid var(--el-border-color-lighter);
  padding-top: 8px;
  margin-top: 4px;
}

.card-meta {
  display: flex;
  gap: 20px;
  font-size: 12px;
  color: var(--el-text-color-placeholder);
}

.expired-text {
  color: var(--el-color-danger);
}

.usage-dialog-body {
  min-height: 120px;
}

.endpoints-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin-bottom: 4px;
}

.endpoint-item {
  display: flex;
  align-items: center;
  gap: 10px;
}

.endpoint-proto {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-color-primary);
  background: var(--el-color-primary-light-9);
  padding: 2px 8px;
  border-radius: 4px;
  white-space: nowrap;
  min-width: 80px;
  text-align: center;
}

.endpoint-url {
  flex: 1;
  font-family: monospace;
}
</style>
