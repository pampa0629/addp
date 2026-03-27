<template>
  <div class="asset-detail" v-loading="loading">
    <!-- 返回按钮 -->
    <div class="back-bar">
      <el-button text :icon="ArrowLeft" @click="$router.back()">返回</el-button>
    </div>

    <template v-if="asset">
      <!-- 头部信息 -->
      <div class="detail-header">
        <div class="header-main">
          <div class="header-title-row">
            <h2 class="asset-title">{{ asset.name }}</h2>
            <el-tag class="type-badge">{{ asset.type_name || '未知类型' }}</el-tag>
          </div>
          <div class="catalog-path">
            <el-icon><FolderOpened /></el-icon>
            <span>{{ asset.catalog_name || '未编目' }}</span>
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
          <el-tooltip v-if="applyStatus === 'pending'" content="已提交申请，等待审批" placement="top">
            <el-button type="info" disabled size="large">审批中</el-button>
          </el-tooltip>
          <el-button v-else-if="applyStatus === 'approved'" type="success" disabled size="large">已授权</el-button>
          <el-button v-else type="primary" size="large" @click="applyDialogVisible = true">
            申请使用
          </el-button>
        </div>
      </div>

      <!-- 描述 -->
      <el-card class="detail-section" shadow="never" v-if="asset.description">
        <template #header>
          <span class="section-title">描述</span>
        </template>
        <p class="description-text">{{ asset.description }}</p>
      </el-card>

      <!-- 基本信息 -->
      <el-card class="detail-section" shadow="never">
        <template #header>
          <span class="section-title">基本信息</span>
        </template>
        <el-descriptions :column="2" border>
          <el-descriptions-item label="资产类型">
            {{ asset.type_name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="来源模块">
            {{ sourceModuleName(asset.source_module) }}
          </el-descriptions-item>
          <el-descriptions-item label="所属目录">
            {{ asset.catalog_name || '未编目' }}
          </el-descriptions-item>
          <el-descriptions-item label="负责人">
            {{ asset.owner_name || '-' }}
          </el-descriptions-item>
          <el-descriptions-item label="上架时间">
            {{ formatDate(asset.updated_at) }}
          </el-descriptions-item>
          <el-descriptions-item label="资产 ID">
            {{ asset.id }}
          </el-descriptions-item>
        </el-descriptions>
      </el-card>

      <!-- 扩展字段（如有） -->
      <el-card class="detail-section" shadow="never" v-if="hasExtFields">
        <template #header>
          <span class="section-title">扩展信息</span>
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
          <span class="section-title">服务地址</span>
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
              <el-button size="small" @click="copyUrl(url)">复制</el-button>
            </div>
          </div>
          <el-empty
            v-if="!serviceEndpoints.endpoints || Object.keys(serviceEndpoints.endpoints).length === 0"
            description="暂无可用地址"
            :image-size="60"
          />
        </template>
      </el-card>

      <!-- 评价区（Phase 6） -->
      <el-card class="detail-section" shadow="never" v-loading="ratingsLoading">
        <template #header>
          <div class="section-header-with-action">
            <span class="section-title">
              用户评价
              <span v-if="ratingStats.count > 0" class="rating-summary">
                <el-rate :model-value="ratingStats.avg_score" disabled allow-half show-score text-color="#ff9900" />
                <span class="rating-count">（{{ ratingStats.count }} 条评价）</span>
              </span>
            </span>
            <el-button
              v-if="applyStatus === 'approved'"
              type="primary"
              size="small"
              plain
              @click="openRatingDialog"
            >{{ myRating ? '修改评价' : '提交评价' }}</el-button>
          </div>
        </template>

        <el-empty v-if="ratings.length === 0" description="暂无评价" :image-size="60" />

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

    <el-empty v-else-if="!loading" description="资产不存在或已下架" />

    <!-- 申请使用对话框 -->
    <el-dialog
      v-model="applyDialogVisible"
      title="申请使用"
      width="480px"
      :close-on-click-modal="false"
    >
      <el-form ref="applyFormRef" :model="applyForm" :rules="applyRules" label-width="90px">
        <el-form-item label="申请理由" prop="reason">
          <el-input
            v-model="applyForm.reason"
            type="textarea"
            :rows="4"
            placeholder="请描述申请理由和使用目的..."
          />
        </el-form-item>
        <el-form-item label="使用时长" prop="duration_day">
          <el-select v-model="applyForm.duration_day" style="width: 100%">
            <el-option label="7 天" :value="7" />
            <el-option label="30 天" :value="30" />
            <el-option label="90 天" :value="90" />
            <el-option label="180 天" :value="180" />
            <el-option label="365 天" :value="365" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="applyDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitApply">提交申请</el-button>
      </template>
    </el-dialog>

    <!-- 评价对话框 -->
    <el-dialog
      v-model="ratingDialogVisible"
      :title="myRating ? '修改评价' : '提交评价'"
      width="480px"
      destroy-on-close
    >
      <el-form :model="ratingForm" label-width="90px">
        <el-form-item label="评分" required>
          <el-rate v-model="ratingForm.score" allow-half show-text />
        </el-form-item>
        <el-form-item label="评价内容">
          <el-input
            v-model="ratingForm.comment"
            type="textarea"
            :rows="4"
            placeholder="分享您的使用体验（可选）..."
          />
        </el-form-item>
        <el-form-item label="问题反馈">
          <el-checkbox-group v-model="ratingForm.tags">
            <el-checkbox value="数据质量问题" />
            <el-checkbox value="文档不清晰" />
            <el-checkbox value="访问异常" />
            <el-checkbox value="其他问题" />
          </el-checkbox-group>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ratingDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submittingRating" @click="submitRating">提交</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, FolderOpened } from '@element-plus/icons-vue'
import { formatDate } from '@common-ui'
import { assetAPI } from '../api/portal'

const route = useRoute()

const loading = ref(false)
const asset = ref(null)
const applyStatus = ref('none') // none | pending | approved

const applyDialogVisible = ref(false)
const submitting = ref(false)
const applyFormRef = ref(null)
const applyForm = ref({ reason: '', duration_day: 30 })
const applyRules = {
  reason: [{ required: true, message: '请填写申请理由', trigger: 'blur' }],
  duration_day: [{ required: true, message: '请选择使用时长', trigger: 'change' }]
}

const endpointsLoading = ref(false)
const serviceEndpoints = ref(null)

// 评价相关状态
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

const moduleNameMap = {
  meta: '元数据管理',
  service: '数据服务',
  standard: '数据标准',
  develop: '开发工作台',
  manager: '数据管理'
}

const protocolLabelMap = {
  rest_api: 'REST API',
  wfs: 'WFS',
  ogc_features: 'OGC API Features',
  proxy: '代理地址',
  original: '原始地址',
  xyz: 'XYZ 瓦片',
  wmts: 'WMTS',
  ogc_tiles: 'OGC Tiles'
}

function sourceModuleName(code) {
  return moduleNameMap[code] || code || '-'
}

function protocolLabel(key) {
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
    ElMessage.success('已复制')
  } catch {
    ElMessage.error('复制失败，请手动选择复制')
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
    ElMessage.warning('请选择评分')
    return
  }
  submittingRating.value = true
  try {
    await assetAPI.addRating(route.params.id, ratingForm.value)
    ElMessage.success('评价已提交')
    ratingDialogVisible.value = false
    await fetchRatings()
  } catch (err) {
    ElMessage.error(err.message || '提交评价失败')
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
      ElMessage.success('申请已提交，等待审批')
      applyDialogVisible.value = false
      applyStatus.value = 'pending'
      applyForm.value = { reason: '', duration_day: 30 }
    } catch (err) {
      ElMessage.error(err.message || '提交申请失败')
    } finally {
      submitting.value = false
    }
  })
}

onMounted(async () => {
  await fetchAsset()
  if (asset.value) {
    fetchApplyStatus()
    fetchRatings()
  }
})
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

/* 评价列表 */
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
