<template>
  <div class="metric-detail" v-loading="loading">
    <div class="page-header">
      <div class="header-left">
        <el-button :icon="ArrowLeft" @click="goBack">{{ $t('standard.common.back') }}</el-button>
        <h2>{{ $t('standard.metric.detailTitle') }}</h2>
        <el-tag :type="statusType(metric.status)" size="small" v-if="metric.status">
          {{ statusLabel(metric.status) }}
        </el-tag>
        <el-tag :type="typeTagType(metric.type)" size="small" v-if="metric.type">
          {{ typeLabel(metric.type) }}
        </el-tag>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="saveChanges" :loading="saving">{{ $t('standard.common.save') }}</el-button>
        <el-button type="success" @click="handleApprove" v-if="metric.status === 'draft'" :disabled="saving">{{ $t('standard.common.approve') }}</el-button>
        <el-button type="warning" @click="handleDeprecate" v-if="metric.status === 'approved'" :disabled="saving">{{ $t('standard.common.deprecate') }}</el-button>
      </div>
    </div>

    <el-row :gutter="20">
      <el-col :span="16">
        <!-- 基本信息 -->
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.metric.basicInfo') }}</h3></template>
          <el-form :model="metric" label-width="100px" size="default">
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item :label="$t('standard.metric.nameLabel')">
                  <el-input v-model="metric.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('standard.metric.codeLabel')">
                  <el-input v-model="metric.code" disabled />
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item :label="$t('standard.metric.typeLabel')">
                  <el-select v-model="metric.type" style="width:100%">
                    <el-option :label="$t('standard.metric.atomic')" value="atomic" />
                    <el-option :label="$t('standard.metric.derived')" value="derived" />
                    <el-option :label="$t('standard.metric.composite')" value="composite" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('standard.metric.categoryLabel')">
                  <el-tree-select
                    v-model="metric.category_id"
                    :data="categoryTree"
                    :props="{ label: 'name', value: 'id', children: 'children' }"
                    clearable style="width:100%"
                  />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item :label="$t('standard.metric.definitionLabel')">
              <el-input v-model="metric.definition" type="textarea" :rows="4" :placeholder="$t('standard.metric.definitionPlaceholder')" />
            </el-form-item>
            <el-form-item :label="$t('standard.metric.formulaLabel')" v-if="metric.type === 'composite'">
              <el-input v-model="metric.formula" type="textarea" :rows="2" :placeholder="$t('standard.metric.formulaPlaceholder')" />
            </el-form-item>
            <el-form-item :label="$t('standard.metric.baseMetricLabel')" v-if="metric.type === 'derived'">
              <el-select v-model="metric.base_metric_id" filterable clearable style="width:100%" :placeholder="$t('standard.metric.baseMetricPlaceholder')">
                <el-option v-for="m in atomicMetrics" :key="m.id" :label="`${m.name}（${m.code}）`" :value="m.id" />
              </el-select>
            </el-form-item>
            <el-form-item :label="$t('standard.common.tags')">
              <el-select
                v-model="metric.tags"
                multiple filterable allow-create default-first-option
                :placeholder="$t('standard.common.tagsPlaceholder')" style="width:100%"
              />
            </el-form-item>
          </el-form>
        </el-card>

        <!-- 关联文档 -->
        <DocumentPanel v-if="metric.id" entity-type="metric" :entity-id="metric.id" />
      </el-col>

      <el-col :span="8">
        <!-- 元数据 -->
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.common.metadata') }}</h3></template>
          <el-descriptions :column="1" size="small">
            <el-descriptions-item :label="$t('standard.common.id')">{{ metric.id }}</el-descriptions-item>
            <el-descriptions-item :label="$t('standard.common.createdAt')">{{ formatTime(metric.created_at) }}</el-descriptions-item>
            <el-descriptions-item :label="$t('standard.common.updatedAt')">{{ formatTime(metric.updated_at) }}</el-descriptions-item>
          </el-descriptions>
        </el-card>

        <!-- 关联数据元（只读） -->
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.metric.relatedElements') }}</h3></template>
          <div v-if="!relatedElements || relatedElements.length === 0" style="color: var(--el-text-color-secondary); font-size: 13px; padding: 8px 0;">
            {{ $t('standard.metric.noElements') }}
          </div>
          <div v-else class="element-list">
            <div v-for="e in relatedElements" :key="e.id" class="element-item">
              <div class="element-name">{{ e.name }}</div>
              <div class="element-code">{{ e.code }}</div>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { metricAPI, metricCategoryAPI } from '../api/standard'
import DocumentPanel from '../components/DocumentPanel.vue'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const loading = ref(false)
const saving = ref(false)
const metric = ref({})
const categories = ref([])
const atomicMetrics = ref([])
const relatedElements = ref([])

const categoryTree = computed(() => buildTree(categories.value))
function buildTree(list, parentId = null) {
  return list.filter(i => (i.parent_id || null) === parentId).map(i => ({ ...i, children: buildTree(list, i.id) }))
}

const typeLabel = (type) => ({ atomic: t('standard.metric.atomic'), derived: t('standard.metric.derived'), composite: t('standard.metric.composite') }[type] || type)
const typeTagType = (type) => ({ atomic: 'primary', derived: 'warning', composite: 'success' }[type] || '')
const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({ draft: t('standard.common.draft'), approved: t('standard.common.approved'), deprecated: t('standard.common.deprecated') }[s] || s)

const formatTime = (time) => {
  if (!time) return '-'
  return new Date(time).toLocaleString('zh-CN')
}

const goBack = () => router.push('/standard/metrics')

const loadMetric = async () => {
  loading.value = true
  try {
    const res = await metricAPI.get(route.params.id)
    metric.value = res || {}
    if (!metric.value.tags) metric.value.tags = []
  } catch (e) {
    ElMessage.error(t('standard.common.loadFailed'))
    goBack()
  } finally {
    loading.value = false
  }
}

const loadCategories = async () => {
  try {
    const res = await metricCategoryAPI.list()
    categories.value = res || []
  } catch (e) {
    console.error('加载目录失败:', e)
  }
}

const loadAtomicMetrics = async () => {
  try {
    const res = await metricAPI.list({ type: 'atomic', page_size: 500 })
    atomicMetrics.value = (res.data || []).filter(m => m.id !== Number(route.params.id))
  } catch (e) {
    console.error('加载原子指标失败:', e)
  }
}

const saveChanges = async () => {
  saving.value = true
  try {
    await metricAPI.update(route.params.id, metric.value)
    ElMessage.success(t('standard.common.saveSuccess'))
    await loadMetric()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('standard.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  try {
    await ElMessageBox.confirm(t('standard.metric.confirmApprove'), t('standard.common.hint'), { type: 'info' })
    await metricAPI.approve(route.params.id)
    ElMessage.success(t('standard.common.approveSuccess'))
    await loadMetric()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('standard.common.approveFailed'))
  }
}

const handleDeprecate = async () => {
  try {
    await ElMessageBox.confirm(t('standard.metric.confirmDeprecate'), t('standard.common.hint'), { type: 'warning' })
    await metricAPI.deprecate(route.params.id)
    ElMessage.success(t('standard.common.deprecated'))
    await loadMetric()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(t('standard.common.operationFailed'))
  }
}

onMounted(() => {
  loadMetric()
  loadCategories()
  loadAtomicMetrics()
})
</script>

<style scoped>
.metric-detail {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-left h2 {
  margin: 0;
  font-size: 18px;
  color: var(--el-text-color-primary);
}

.header-right {
  display: flex;
  gap: 8px;
}

.section-card {
  margin-bottom: 20px;
}

.element-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.element-item {
  padding: 6px 8px;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
}

.element-name {
  font-size: 13px;
  font-weight: 500;
  color: var(--el-color-primary);
}

.element-code {
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
