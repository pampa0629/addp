<template>
  <div class="asset-detail" v-loading="loading">
    <!-- 返回按钮 -->
    <div class="back-bar">
      <el-button text :icon="ArrowLeft" @click="handleBack">{{ t('portal.common.back') }}</el-button>
    </div>

    <template v-if="asset">
      <!-- 头部信息 -->
      <div class="detail-header">
        <div class="header-main">
          <div class="header-title-row">
            <h2 class="asset-title">{{ asset.name }}</h2>
            <el-tag class="type-badge">{{ getTypeName(asset.type_code, asset.type_name) }}</el-tag>
          </div>
          <div class="catalog-path">
            <el-icon><FolderOpened /></el-icon>
            <span>{{ asset.catalog_name || t('portal.assetDetail.uncategorized') }}</span>
          </div>
          <div class="tags" v-if="asset.tags?.length">
            <el-tag
              v-for="tag in asset.tags"
              :key="tag"
              size="small"
              type="info"
            >{{ tag }}</el-tag>
          </div>
        </div>

        <!-- 状态按钮 -->
        <div class="header-actions">
          <el-tooltip v-if="applyStatus === 'pending'" :content="t('portal.assetDetail.pendingTooltip')" placement="top">
            <el-button type="info" disabled size="large">{{ t('portal.assetDetail.reviewing') }}</el-button>
          </el-tooltip>
          <el-button v-else-if="applyStatus === 'approved'" type="success" disabled size="large">{{ t('portal.assetDetail.authorized') }}</el-button>
          <el-button v-else type="primary" size="large" @click="applyDialogVisible = true">
            {{ t('portal.assetDetail.applyUsage') }}
          </el-button>
        </div>
      </div>

      <!-- 描述 -->
      <el-card class="detail-section" shadow="never" v-if="asset.description">
        <template #header>
          <span class="section-title">{{ t('portal.assetDetail.description') }}</span>
        </template>
        <p class="description-text">{{ asset.description }}</p>
      </el-card>

      <!-- 基本信息 -->
      <el-card class="detail-section" shadow="never">
        <template #header>
          <span class="section-title">{{ t('portal.assetDetail.basicInfo') }}</span>
        </template>
        <el-descriptions :column="2" border>
          <el-descriptions-item :label="t('portal.assetDetail.assetType')">
            {{ getTypeName(asset.type_code, asset.type_name) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('portal.assetDetail.sourceModule')">
            {{ sourceModuleName(asset.source_module) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('portal.assetDetail.catalog')">
            {{ asset.catalog_name || t('portal.assetDetail.uncategorized') }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('portal.assetDetail.owner')">
            {{ asset.owner_name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('portal.assetDetail.publishedAt')">
            {{ formatDate(asset.updated_at) }}
          </el-descriptions-item>
          <el-descriptions-item :label="t('portal.assetDetail.assetId')">
            {{ asset.id }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- 扩展字段（如有） -->
      <el-card class="detail-section" shadow="never" v-if="hasExtFields">
        <template #header>
          <span class="section-title">{{ t('portal.assetDetail.extInfo') }}</span>
        </template>
        <el-descriptions :column="2" border>
          <el-descriptions-item
            v-for="(value, key) in asset.ext_fields"
            :key="key"
            :label="key"
          >
            {{ formatExtValue(value) }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- 服务地址（已授权时展示） -->
      <el-card class="detail-section" shadow="never" v-if="applyStatus === 'approved'" v-loading="endpointsLoading">
        <template #header>
          <span class="section-title">{{ t('portal.assetDetail.serviceAddress') }}</span>
        </template>
        <template v-if="serviceEndpoints">
          <div
            v-for="(url, protocol) in serviceEndpoints.endpoints"
            :key="protocol"
            class="endpoint-item"
          >
            <div class="endpoint-label">{{ protocolLabel(protocol) }}</div>
            <div class="endpoint-url-row">
              <el-input :value="url" readonly class="endpoint-url-input" />
              <el-button size="small" @click="copyUrl(url)">{{ t('portal.common.copied') }}</el-button>
            </div>
          </div>
          <el-empty
            v-if="!serviceEndpoints.endpoints || Object.keys(serviceEndpoints.endpoints).length === 0"
            :description="t('portal.assetDetail.noEndpoints')"
            :image-size="60"
          />
        </template>
      </el-card>

      <!-- 评价区 -->
      <el-card class="detail-section" shadow="never" v-loading="ratingsLoading">
        <template #header>
          <div class="section-header-with-action">
            <span class="section-title">
              {{ t('portal.assetDetail.userRatings') }}
              <span v-if="ratingStats.count > 0" class="rating-summary">
                <el-rate :model-value="ratingStats.avg_score" disabled allow-half show-score text-color="#ff9900" />
                <span class="rating-count">（{{ ratingStats.count }} {{ t('portal.assetDetail.ratingCount') }}）</span>
              </span>
            </span>
            <el-button
              v-if="applyStatus === 'approved'"
              type="primary"
              size="small"
              plain
              @click="openRatingDialog"
            >{{ myRating ? t('portal.assetDetail.editRating') : t('portal.assetDetail.submitRating') }}</el-button>
          </div>
        </template>

        <el-empty v-if="ratings.length === 0" :description="t('portal.assetDetail.noRatings')" :image-size="60" />

        <div v-else class="rating-list">
          <div v-for="r in ratings" :key="r.id" class="rating-item">
            <div class="rating-header">
              <span class="rating-user">{{ r.user_name || '匿名用户' }}</span>
              <el-rate :model-value="r.score" disabled allow-half size="small" />
              <span class="rating-date">{{ formatDate(r.created_at) }}</span>
            </div>
            <p v-if="r.comment" class="rating-comment">{{ r.comment }}</p>
            <div v-if="r.tags?.length" class="rating-tags">
              <el-tag v-for="tag in r.tags" :key="tag" size="small" type="warning">{{ tag }}</el-tag>
            </div>
          </div>
        </div>
      </el-card>
    </template>

    <el-empty v-else-if="!loading" :description="t('portal.assetDetail.notFound')" />

    <!-- 申请使用对话框 -->
    <el-dialog
      v-model="applyDialogVisible"
      :title="t('portal.assetDetail.applyUsage')"
      width="480px"
      :close-on-click-modal="false"
    >
      <el-form ref="applyFormRef" :model="applyForm" :rules="applyRules" label-width="90px">
        <el-form-item :label="t('portal.assetDetail.applyReason')" prop="reason">
          <el-input
            v-model="applyForm.reason"
            type="textarea"
            :rows="4"
            :placeholder="t('portal.assetDetail.applyReasonPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="t('portal.assetDetail.duration')" prop="duration_day">
          <el-select v-model="applyForm.duration_day" style="width: 100%">
            <el-option :label="t('portal.assetDetail.days7')" :value="7" />
            <el-option :label="t('portal.assetDetail.days30')" :value="30" />
            <el-option :label="t('portal.assetDetail.days90')" :value="90" />
            <el-option :label="t('portal.assetDetail.days180')" :value="180" />
            <el-option :label="t('portal.assetDetail.days365')" :value="365" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="applyDialogVisible = false">{{ t('portal.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submitApply">{{ t('portal.assetDetail.submitApply') }}</el-button>
      </template>
    </el-dialog>

    <!-- 评价对话框 -->
    <el-dialog
      v-model="ratingDialogVisible"
      :title="myRating ? t('portal.assetDetail.editRating') : t('portal.assetDetail.submitRating')"
      width="480px"
      destroy-on-close
    >
      <el-form :model="ratingForm" label-width="90px">
        <el-form-item :label="t('portal.assetDetail.score')" required>
          <el-rate v-model="ratingForm.score" allow-half show-text />
        </el-form-item>
        <el-form-item :label="t('portal.assetDetail.ratingContent')">
          <el-input
            v-model="ratingForm.comment"
            type="textarea"
            :rows="4"
            :placeholder="t('portal.assetDetail.ratingPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="t('portal.assetDetail.issueFeedback')">
          <el-checkbox-group v-model="ratingForm.tags">
            <el-checkbox :value="t('portal.assetDetail.issueDataQuality')" />
            <el-checkbox :value="t('portal.assetDetail.issueDocUnclear')" />
            <el-checkbox :value="t('portal.assetDetail.issueAccessError')" />
            <el-checkbox :value="t('portal.assetDetail.issueOther')" />
          </el-checkbox-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ratingDialogVisible = false">{{ t('portal.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submittingRating" @click="submitRating">{{ t('portal.common.submit') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowLeft, FolderOpened } from '@element-plus/icons-vue'
import { formatDate } from '@common-ui'
import { assetAPI } from '../api/portal'
import { useAssetType } from '../composables/useAssetType'
import { assetDetailReturnTarget } from '../utils/routeState'

const { t } = useI18n()
const { getTypeName } = useAssetType()
const route = useRoute()
const router = useRouter()

const loading = ref(false)
const asset = ref(null)
const applyStatus = ref('none')

const applyDialogVisible = ref(false)
const submitting = ref(false)
const applyFormRef = ref(null)
const applyForm = ref({ reason: '', duration_day: 30 })
const applyRules = computed(() => ({
  reason: [{ required: true, message: t('portal.assetDetail.applyReasonRequired'), trigger: 'blur' }],
  duration_day: [{ required: true, message: t('portal.assetDetail.durationRequired'), trigger: 'change' }]
}))

const endpointsLoading = ref(false)
const serviceEndpoints = ref(null)

const ratingsLoading = ref(false)
const ratings = ref([])
const myRating = ref(null)
const ratingStats = ref({ avg_score: 0, count: 0 })
const ratingDialogVisible = ref(false)
const submittingRating = ref(false)
const ratingForm = ref({ score: 5, comment: '', tags: [] })

const hasExtFields = computed(() => {
  return asset.value?.ext_fields && Object.keys(asset.value.ext_fields).length > 0
})

const moduleNameMap = computed(() => ({
  meta: t('portal.assetDetail.moduleMeta'),
  service: t('portal.assetDetail.moduleService'),
  standard: t('portal.assetDetail.moduleStandard'),
  develop: t('portal.assetDetail.moduleDevelop'),
  manager: t('portal.assetDetail.moduleManager'),
}))

const protocolLabelMap = {
  rest_api: 'REST API',
  wfs: 'WFS',
  ogc_features: 'OGC API Features',
  xyz: 'XYZ',
  wmts: 'WMTS',
  ogc_tiles: 'OGC Tiles'
}

function sourceModuleName(code) {
  return moduleNameMap.value[code] || code || '-'
}

function protocolLabel(key) {
  if (key === 'proxy') return t('portal.assetDetail.protocolProxy')
  if (key === 'original') return t('portal.assetDetail.protocolOriginal')
  if (key === 'xyz') return t('portal.assetDetail.protocolXyz')
  return protocolLabelMap[key] || key
}

function formatExtValue(value) {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

async function copyUrl(url) {
  try {
    await navigator.clipboard.writeText(url)
    ElMessage.success(t('portal.common.copied'))
  } catch {
    ElMessage.error(t('portal.common.copyFailed'))
  }
}

async function fetchEndpoints() {
  endpointsLoading.value = true
  try {
    serviceEndpoints.value = await assetAPI.getEndpoints(route.params.id)
  } catch {
    serviceEndpoints.value = { endpoints: {} }
  } finally {
    endpointsLoading.value = false
  }
}

async function fetchRatings() {
  ratingsLoading.value = true
  try {
    const data = await assetAPI.getRatings(route.params.id)
    ratings.value = data.ratings || []
    myRating.value = data.my_rating || null
    ratingStats.value = {
      avg_score: data.avg_score || 0,
      count: data.total || 0
    }
  } catch (err) {
    console.error('获取评价失败:', err)
  } finally {
    ratingsLoading.value = false
  }
}

function openRatingDialog() {
  if (myRating.value) {
    ratingForm.value = {
      score: myRating.value.score,
      comment: myRating.value.comment || '',
      tags: myRating.value.tags || []
    }
  } else {
    ratingForm.value = { score: 5, comment: '', tags: [] }
  }
  ratingDialogVisible.value = true
}

async function submitRating() {
  if (!ratingForm.value.score) {
    ElMessage.warning(t('portal.assetDetail.scoreRequired'))
    return
  }
  submittingRating.value = true
  try {
    await assetAPI.addRating(route.params.id, ratingForm.value)
    ElMessage.success(t('portal.assetDetail.ratingSubmitted'))
    ratingDialogVisible.value = false
    await fetchRatings()
  } catch (err) {
    ElMessage.error(err.message || t('portal.assetDetail.ratingFailed'))
  } finally {
    submittingRating.value = false
  }
}

async function fetchAsset() {
  loading.value = true
  try {
    const data = await assetAPI.get(route.params.id)
    asset.value = data
  } catch (err) {
    console.error('获取资产详情失败:', err)
    asset.value = null
  } finally {
    loading.value = false
  }
}

async function handleBack() {
  const target = assetDetailReturnTarget(window.history.state?.back, asset.value?.catalog_id)
  if (target.history === 'back') {
    router.back()
    return
  }
  await router.replace(target.location)
}

async function fetchApplyStatus() {
  try {
    const data = await assetAPI.getApplyStatus(route.params.id)
    applyStatus.value = data.status || 'none'
    if (applyStatus.value === 'approved') {
      fetchEndpoints()
    }
  } catch {
    applyStatus.value = 'none'
  }
}

async function submitApply() {
  if (!applyFormRef.value) return
  await applyFormRef.value.validate(async (valid) => {
    if (!valid) return
    submitting.value = true
    try {
      await assetAPI.apply(route.params.id, applyForm.value)
      ElMessage.success(t('portal.assetDetail.applySubmitted'))
      applyDialogVisible.value = false
      applyStatus.value = 'pending'
      applyForm.value = { reason: '', duration_day: 30 }
    } catch (err) {
      ElMessage.error(err.message || t('portal.assetDetail.applyFailed'))
    } finally {
      submitting.value = false
    }
  })
}

watch(() => route.params.id, async () => {
  asset.value = null
  applyStatus.value = 'none'
  serviceEndpoints.value = null
  ratings.value = []
  myRating.value = null
  ratingStats.value = { avg_score: 0, count: 0 }
  await fetchAsset()
  if (asset.value) {
    fetchApplyStatus()
    fetchRatings()
  }
}, { immediate: true })
</script>

<style scoped>
.asset-detail {
  padding: 24px;
  max-width: 960px;
  margin: 0 auto;
}

.back-bar {
  margin-bottom: 16px;
}

.detail-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
  background: var(--el-fill-color-blank);
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  padding: 20px;
}

.header-main {
  flex: 1;
}

.header-title-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 10px;
  flex-wrap: wrap;
}

.asset-title {
  font-size: 22px;
  font-weight: 700;
  margin: 0;
  color: var(--el-text-color-primary);
}

.type-badge {
  font-size: 12px;
}

.catalog-path {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-bottom: 10px;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.header-actions {
  flex-shrink: 0;
}

.detail-section {
  margin-bottom: 16px;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.section-header-with-action {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

.rating-summary {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-left: 12px;
  font-weight: normal;
}

.rating-count {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.description-text {
  font-size: 14px;
  color: var(--el-text-color-regular);
  line-height: 1.7;
  margin: 0;
  white-space: pre-wrap;
}

.endpoint-item {
  margin-bottom: 16px;
}

.endpoint-item:last-child {
  margin-bottom: 0;
}

.endpoint-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-regular);
  margin-bottom: 6px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.endpoint-url-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.endpoint-url-input {
  flex: 1;
  font-family: monospace;
  font-size: 13px;
}

.rating-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.rating-item {
  padding-bottom: 16px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.rating-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.rating-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}

.rating-user {
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.rating-date {
  font-size: 12px;
  color: var(--el-text-color-placeholder);
  margin-left: auto;
}

.rating-comment {
  font-size: 13px;
  color: var(--el-text-color-regular);
  margin: 6px 0;
  line-height: 1.6;
}

.rating-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}
</style>
