<template>
  <div class="metric-detail" v-loading="loading">
    <div class="page-header">
      <div class="header-left">
        <el-button :icon="ArrowLeft" @click="goBack">返回</el-button>
        <h2>指标详情</h2>
        <el-tag :type="statusType(metric.status)" size="small" v-if="metric.status">
          {{ statusLabel(metric.status) }}
        </el-tag>
        <el-tag :type="typeTagType(metric.type)" size="small" v-if="metric.type">
          {{ typeLabel(metric.type) }}
        </el-tag>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="saveChanges" :loading="saving">保存</el-button>
        <el-button type="success" @click="handleApprove" v-if="metric.status === 'draft'" :disabled="saving">审批</el-button>
        <el-button type="warning" @click="handleDeprecate" v-if="metric.status === 'approved'" :disabled="saving">废弃</el-button>
      </div>
    </div>

    <el-row :gutter="20">
      <el-col :span="16">
        <!-- 基本信息 -->
        <el-card class="section-card">
          <template #header><h3>基本信息</h3></template>
          <el-form :model="metric" label-width="100px" size="default">
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item label="指标名称">
                  <el-input v-model="metric.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="英文编码">
                  <el-input v-model="metric.code" disabled />
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item label="指标类型">
                  <el-select v-model="metric.type" style="width:100%">
                    <el-option label="原子指标" value="atomic" />
                    <el-option label="派生指标" value="derived" />
                    <el-option label="复合指标" value="composite" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="所属目录">
                  <el-tree-select
                    v-model="metric.category_id"
                    :data="categoryTree"
                    :props="{ label: 'name', value: 'id', children: 'children' }"
                    clearable style="width:100%"
                  />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="业务口径">
              <el-input v-model="metric.definition" type="textarea" :rows="4" placeholder="描述指标的业务含义和计算口径" />
            </el-form-item>
            <el-form-item label="计算公式" v-if="metric.type === 'composite'">
              <el-input v-model="metric.formula" type="textarea" :rows="2" placeholder="如：metric_a / metric_b × 100%" />
            </el-form-item>
            <el-form-item label="基准指标" v-if="metric.type === 'derived'">
              <el-select v-model="metric.base_metric_id" filterable clearable style="width:100%" placeholder="选择基准原子指标">
                <el-option v-for="m in atomicMetrics" :key="m.id" :label="`${m.name}（${m.code}）`" :value="m.id" />
              </el-select>
            </el-form-item>
            <el-form-item label="标签">
              <el-select
                v-model="metric.tags"
                multiple filterable allow-create default-first-option
                placeholder="输入后回车添加标签" style="width:100%"
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
          <template #header><h3>元数据</h3></template>
          <el-descriptions :column="1" size="small">
            <el-descriptions-item label="ID">{{ metric.id }}</el-descriptions-item>
            <el-descriptions-item label="创建时间">{{ formatTime(metric.created_at) }}</el-descriptions-item>
            <el-descriptions-item label="更新时间">{{ formatTime(metric.updated_at) }}</el-descriptions-item>
          </el-descriptions>
        </el-card>

        <!-- 关联数据元（只读） -->
        <el-card class="section-card">
          <template #header><h3>关联数据元</h3></template>
          <div v-if="!relatedElements || relatedElements.length === 0" style="color: var(--el-text-color-secondary); font-size: 13px; padding: 8px 0;">
            暂无关联数据元
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
import { ArrowLeft } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { metricAPI, metricCategoryAPI } from '../api/standard'
import DocumentPanel from '../components/DocumentPanel.vue'

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

const typeLabel = (t) => ({ atomic: '原子指标', derived: '派生指标', composite: '复合指标' }[t] || t)
const typeTagType = (t) => ({ atomic: 'primary', derived: 'warning', composite: 'success' }[t] || '')
const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({ draft: '草稿', approved: '已审批', deprecated: '已废弃' }[s] || s)

const formatTime = (t) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

const goBack = () => router.push('/standard/metrics')

const loadMetric = async () => {
  loading.value = true
  try {
    const res = await metricAPI.get(route.params.id)
    metric.value = res || {}
    if (!metric.value.tags) metric.value.tags = []
  } catch (e) {
    ElMessage.error('加载失败')
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
    atomicMetrics.value = (res.data || []).filter(m => m.id !== Number(route.params.id))  // 分页格式，保留 .data
  } catch (e) {
    console.error('加载原子指标失败:', e)
  }
}

const saveChanges = async () => {
  saving.value = true
  try {
    await metricAPI.update(route.params.id, metric.value)
    ElMessage.success('保存成功')
    await loadMetric()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  try {
    await ElMessageBox.confirm('确认审批通过？审批后状态变为"已审批"', '提示', { type: 'info' })
    await metricAPI.approve(route.params.id)
    ElMessage.success('审批成功')
    await loadMetric()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('审批失败')
  }
}

const handleDeprecate = async () => {
  try {
    await ElMessageBox.confirm('确认废弃该指标？', '提示', { type: 'warning' })
    await metricAPI.deprecate(route.params.id)
    ElMessage.success('已废弃')
    await loadMetric()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('操作失败')
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
