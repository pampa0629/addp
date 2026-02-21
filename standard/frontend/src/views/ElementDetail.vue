<template>
  <div class="element-detail" v-loading="loading">
    <div class="page-header">
      <div class="header-left">
        <el-button :icon="ArrowLeft" @click="goBack">返回</el-button>
        <h2>数据元详情</h2>
        <el-tag :type="statusType(element.status)" size="small" v-if="element.status">
          {{ statusLabel(element.status) }}
        </el-tag>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="saveChanges" :loading="saving">保存</el-button>
        <el-button type="success" @click="handleApprove" v-if="element.status === 'draft'" :disabled="saving">审批</el-button>
      </div>
    </div>

    <el-row :gutter="20">
      <el-col :span="16">
        <!-- 基本信息 -->
        <el-card class="section-card">
          <template #header><h3>基本信息</h3></template>
          <el-form :model="element" label-width="120px" size="default">
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item label="中文名称">
                  <el-input v-model="element.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="英文编码">
                  <el-input v-model="element.code" disabled />
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item label="数据类型">
                  <el-select v-model="element.data_type" style="width: 100%">
                    <el-option label="字符串 (string)" value="string" />
                    <el-option label="整数 (int)" value="int" />
                    <el-option label="浮点数 (float)" value="float" />
                    <el-option label="日期时间 (date)" value="date" />
                    <el-option label="布尔 (bool)" value="bool" />
                    <el-option label="JSON (json)" value="json" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="长度">
                  <el-input-number v-model="element.length" :min="1" style="width: 100%" />
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item label="允许为空">
                  <el-switch v-model="element.nullable" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="计量单位">
                  <el-select v-model="element.unit_id" clearable filterable placeholder="选择单位" style="width:100%">
                    <el-option-group v-for="cat in unitsByCategory" :key="cat.id" :label="cat.name">
                      <el-option v-for="u in cat.units" :key="u.id" :label="`${u.name}（${u.symbol}）`" :value="u.id" />
                    </el-option-group>
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item label="数据分级">
                  <el-select v-model="element.security_level" clearable placeholder="选择安全级别" style="width:100%">
                    <el-option v-for="g in gradingLevels" :key="g.level" :label="`${g.level} ${g.name}`" :value="g.level" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="数据分类">
                  <el-tree-select
                    v-model="element.classification_id"
                    :data="classificationTree"
                    :props="{ label: 'name', value: 'id', children: 'children' }"
                    clearable placeholder="选择分类" style="width:100%"
                  />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item label="关联码值集">
              <el-select v-model="element.code_set_id" clearable filterable style="width: 100%" @change="handleCodeSetChange">
                <el-option
                  v-for="cs in codeSets"
                  :key="cs.id"
                  :label="`${cs.name} (${cs.code})`"
                  :value="cs.id"
                />
              </el-select>
            </el-form-item>
            <el-form-item label="默认值">
              <el-input v-model="element.default_value" />
            </el-form-item>
            <el-form-item label="业务含义">
              <el-input v-model="element.definition" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item label="示例值">
              <el-select
                v-model="element.example_values"
                multiple
                filterable
                allow-create
                default-first-option
                placeholder="输入后回车添加"
                style="width: 100%"
              />
            </el-form-item>
          </el-form>
        </el-card>

        <!-- 质量规则 -->
        <el-card class="section-card">
          <template #header>
            <div class="card-header">
              <h3><el-icon class="header-icon"><CircleCheck /></el-icon>质量规则配置</h3>
              <el-button size="small" @click="addRule">添加规则</el-button>
            </div>
          </template>
          <div v-if="!qualityRules || qualityRules.length === 0" class="empty-rules">
            <el-empty description="暂无质量规则" />
          </div>
          <div v-else class="rules-list">
            <div v-for="(rule, index) in qualityRules" :key="index" class="rule-item">
              <div class="rule-header">
                <el-checkbox v-model="rule.enabled">启用</el-checkbox>
                <el-select v-model="rule.type" size="small" style="width: 140px">
                  <el-option label="非空检查" value="not_null" />
                  <el-option label="格式校验" value="format" />
                  <el-option label="长度范围" value="length" />
                  <el-option label="唯一性" value="unique" />
                  <el-option label="取值范围" value="value_range" />
                  <el-option label="数据类型" value="data_type" />
                  <el-option label="自定义SQL" value="custom" />
                </el-select>
                <el-select v-model="rule.severity" size="small" style="width: 100px">
                  <el-option label="错误" value="error" />
                  <el-option label="警告" value="warning" />
                  <el-option label="提示" value="info" />
                </el-select>
                <el-button link type="danger" @click="removeRule(index)">删除</el-button>
              </div>
              <el-input
                v-model="rule.message"
                placeholder="错误提示信息"
                size="small"
                class="rule-message"
              />
              <div v-if="rule.type === 'format'" class="rule-params">
                <el-input
                  v-model="rule.params.regex"
                  placeholder="正则表达式，如：^1[3-9]\d{9}$"
                  size="small"
                />
              </div>
              <div v-if="rule.type === 'length'" class="rule-params">
                <el-row :gutter="10">
                  <el-col :span="12">
                    <el-input-number v-model="rule.params.min" placeholder="最小长度" size="small" style="width: 100%" />
                  </el-col>
                  <el-col :span="12">
                    <el-input-number v-model="rule.params.max" placeholder="最大长度" size="small" style="width: 100%" />
                  </el-col>
                </el-row>
              </div>
              <div v-if="rule.type === 'value_range'" class="rule-params">
                <el-select
                  v-model="rule.params.enum"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  placeholder="枚举值（可选）"
                  size="small"
                  style="width: 100%"
                />
                <el-row :gutter="10" style="margin-top: 8px">
                  <el-col :span="12">
                    <el-input-number v-model="rule.params.min" placeholder="最小值（可选）" size="small" style="width: 100%" />
                  </el-col>
                  <el-col :span="12">
                    <el-input-number v-model="rule.params.max" placeholder="最大值（可选）" size="small" style="width: 100%" />
                  </el-col>
                </el-row>
              </div>
              <div v-if="rule.type === 'custom'" class="rule-params">
                <el-input
                  v-model="rule.params.sql"
                  type="textarea"
                  :rows="3"
                  placeholder="自定义检查SQL（返回不符合规则的记录）"
                  size="small"
                />
              </div>
            </div>
          </div>
        </el-card>

        <!-- 关联的码值项（只读展示） -->
        <el-card class="section-card" v-if="element.code_set_id">
          <template #header><h3><el-icon class="header-icon"><List /></el-icon>关联的码值项（参考）</h3></template>
          <el-table :data="codeItems" v-loading="codeItemsLoading" size="small" max-height="300">
            <el-table-column label="编码" prop="code" width="120" />
            <el-table-column label="显示值" prop="value" min-width="140" />
            <el-table-column label="描述" prop="description" show-overflow-tooltip />
          </el-table>
        </el-card>

        <!-- 关联文档 -->
        <DocumentPanel v-if="element.id" entity-type="element" :entity-id="element.id" />
      </el-col>

      <el-col :span="8">
        <!-- 元数据信息 -->
        <el-card class="section-card">
          <template #header><h3>元数据</h3></template>
          <el-descriptions :column="1" size="small">
            <el-descriptions-item label="ID">{{ element.id }}</el-descriptions-item>
            <el-descriptions-item label="创建时间">{{ formatTime(element.created_at) }}</el-descriptions-item>
            <el-descriptions-item label="更新时间">{{ formatTime(element.updated_at) }}</el-descriptions-item>
          </el-descriptions>
        </el-card>

        <!-- 关联的业务术语（只读） -->
        <el-card class="section-card">
          <template #header><h3>关联的业务术语</h3></template>
          <div v-if="relatedGlossaries.length === 0" style="color: var(--el-text-color-secondary); font-size: 13px; padding: 8px 0;">
            暂未被任何业务术语关联
          </div>
          <div v-else class="glossary-list">
            <div
              v-for="g in relatedGlossaries"
              :key="g.id"
              class="glossary-item"
              @click="goToGlossary(g.id)"
            >
              <div class="glossary-name">{{ g.name }}</div>
              <div class="glossary-def">{{ g.definition }}</div>
              <el-tag size="small" :type="statusType(g.status)" style="margin-top: 4px">{{ statusLabel(g.status) }}</el-tag>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ArrowLeft, CircleCheck, List } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { elementAPI, codeSetAPI, glossaryAPI, unitAPI, classificationAPI, gradingLevelAPI } from '../api/standard'
import DocumentPanel from '../components/DocumentPanel.vue'

const router = useRouter()
const route = useRoute()
const loading = ref(false)
const saving = ref(false)
const element = ref({})
const codeSets = ref([])
const codeItems = ref([])
const codeItemsLoading = ref(false)
const relatedGlossaries = ref([])
const units = ref([])
const gradingLevels = ref([])
const classifications = ref([])

const unitsByCategory = computed(() => {
  const map = {}
  units.value.forEach(u => {
    const catId = u.category_id || 0
    const catName = u.category?.name || '其他'
    if (!map[catId]) map[catId] = { id: catId, name: catName, units: [] }
    map[catId].units.push(u)
  })
  return Object.values(map)
})

const classificationTree = computed(() => buildTree(classifications.value))
function buildTree(list, parentId = null) {
  return list.filter(i => (i.parent_id || null) === parentId).map(i => ({ ...i, children: buildTree(list, i.id) }))
}

const qualityRules = computed({
  get() {
    if (!element.value.quality_rules) return []
    const qr = element.value.quality_rules
    if (qr.rules && Array.isArray(qr.rules)) return qr.rules
    return []
  },
  set(val) {
    if (!element.value.quality_rules) {
      element.value.quality_rules = { rules: val }
    } else {
      element.value.quality_rules.rules = val
    }
  }
})

const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({ draft: '草稿', approved: '已审批', deprecated: '已废弃' }[s] || s)

const goToGlossary = (id) => {
  router.push(`/standard/glossaries/${id}`)
}

const formatTime = (t) => {
  if (!t) return '-'
  return new Date(t).toLocaleString('zh-CN')
}

const goBack = () => router.push('/standard/elements')

const loadElement = async () => {
  loading.value = true
  try {
    const res = await elementAPI.get(route.params.id)
    element.value = res.data || {}
    if (!element.value.quality_rules) {
      element.value.quality_rules = { rules: [] }
    }
    // 如果已关联码值集，加载码值项
    if (element.value.code_set_id) {
      loadCodeItems(element.value.code_set_id)
    }
  } catch (e) {
    ElMessage.error('加载失败')
    goBack()
  } finally {
    loading.value = false
  }
}

const loadCodeSets = async () => {
  try {
    const res = await codeSetAPI.list({ page_size: 500 })
    codeSets.value = res.data || []
  } catch (e) {
    console.error('加载码值集失败:', e)
  }
}

const loadRelatedGlossaries = async () => {
  try {
    const res = await glossaryAPI.list({ element_id: route.params.id })
    relatedGlossaries.value = res.data || []
  } catch (e) {
    console.error('加载关联术语失败:', e)
  }
}

const loadUnits = async () => {
  try {
    const res = await unitAPI.list({ page_size: 500 })
    units.value = res.data || []
  } catch (e) {
    console.error('加载单位失败:', e)
  }
}

const loadGradingLevels = async () => {
  try {
    const res = await gradingLevelAPI.list()
    gradingLevels.value = res.data || []
  } catch (e) {
    console.error('加载分级失败:', e)
  }
}

const loadClassifications = async () => {
  try {
    const res = await classificationAPI.list()
    classifications.value = res.data || []
  } catch (e) {
    console.error('加载分类失败:', e)
  }
}

const loadCodeItems = async (codeSetId) => {
  if (!codeSetId) {
    codeItems.value = []
    return
  }
  codeItemsLoading.value = true
  try {
    const res = await codeSetAPI.getItems(codeSetId)
    codeItems.value = res.data || []
  } catch (e) {
    console.error('加载码值项失败:', e)
    codeItems.value = []
  } finally {
    codeItemsLoading.value = false
  }
}

const handleCodeSetChange = (codeSetId) => {
  loadCodeItems(codeSetId)
}

const addRule = () => {
  const newRule = {
    type: 'not_null',
    enabled: true,
    severity: 'error',
    message: '',
    params: {}
  }
  qualityRules.value = [...qualityRules.value, newRule]
}

const removeRule = (index) => {
  qualityRules.value = qualityRules.value.filter((_, i) => i !== index)
}

const saveChanges = async () => {
  saving.value = true
  try {
    await elementAPI.update(route.params.id, element.value)
    ElMessage.success('保存成功')
    await loadElement()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  try {
    await ElMessageBox.confirm('确认审批通过？审批后状态变为"已审批"', '提示', { type: 'info' })
    await elementAPI.approve(route.params.id)
    ElMessage.success('审批成功')
    await loadElement()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error('审批失败')
  }
}

onMounted(() => {
  loadElement()
  loadCodeSets()
  loadRelatedGlossaries()
  loadUnits()
  loadGradingLevels()
  loadClassifications()
})
</script>

<style scoped>
.element-detail {
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

.section-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header h3 {
  margin: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 15px;
  color: var(--el-text-color-primary);
}

.header-icon {
  color: var(--el-color-primary);
}

.empty-rules {
  padding: 40px 0;
}

.rules-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.rule-item {
  padding: 12px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  border: 1px solid var(--el-border-color);
}

.rule-header {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 8px;
}

.rule-message {
  margin-bottom: 8px;
}

.rule-params {
  margin-top: 8px;
}

.glossary-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.glossary-item {
  padding: 8px 10px;
  border: 1px solid var(--el-border-color);
  border-radius: 4px;
  cursor: pointer;
  transition: border-color 0.2s;
}

.glossary-item:hover {
  border-color: var(--el-color-primary);
}

.glossary-name {
  font-weight: 500;
  font-size: 14px;
  color: var(--el-color-primary);
}

.glossary-def {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
