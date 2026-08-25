<template>
  <div class="element-detail" v-loading="loading">
    <div class="page-header">
      <div class="header-left">
        <el-button :icon="ArrowLeft" @click="goBack">{{ $t('standard.common.back') }}</el-button>
        <h2>{{ $t('standard.element.detailTitle') }}</h2>
        <el-tag :type="statusType(element.status)" size="small" v-if="element.status">
          {{ statusLabel(element.status) }}
        </el-tag>
        <el-tag v-if="isDirty" type="warning" size="small">{{ $t('standard.common.unsaved') }}</el-tag>
      </div>
      <div class="header-right">
        <el-button v-if="canUpdate" type="primary" @click="saveChanges" :loading="saving">{{ $t('standard.common.save') }}</el-button>
        <el-button v-if="canApprove && element.status === 'draft'" type="success" @click="handleApprove" :loading="isActionLocked(actionKey)" :disabled="saving">{{ $t('standard.common.approve') }}</el-button>
      </div>
    </div>

    <el-row :gutter="20">
      <el-col :span="16">
        <!-- 基本信息 -->
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.element.basicInfo') }}</h3></template>
          <el-form :model="element" label-width="120px" size="default" :disabled="!canUpdate">
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item :label="$t('standard.element.nameLabel')">
                  <el-input v-model="element.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('standard.element.codeLabel')">
                  <el-input v-model="element.code" disabled />
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item :label="$t('standard.element.dataTypeLabel')">
                  <el-select v-model="element.data_type" style="width: 100%">
                    <el-option :label="$t('standard.element.dataTypeString')" value="string" />
                    <el-option :label="$t('standard.element.dataTypeInt')" value="int" />
                    <el-option :label="$t('standard.element.dataTypeFloat')" value="float" />
                    <el-option :label="$t('standard.element.dataTypeDate')" value="date" />
                    <el-option :label="$t('standard.element.dataTypeBool')" value="bool" />
                    <el-option :label="$t('standard.element.dataTypeJson')" value="json" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('standard.element.lengthLabel')">
                  <el-input-number v-model="element.length" :min="1" style="width: 100%" />
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item :label="$t('standard.element.nullableLabel')">
                  <el-switch v-model="element.nullable" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('standard.glossary.domainLabel')">
                  <el-select v-model="element.domain_id" filterable :placeholder="$t('standard.common.domainOptional')" style="width: 100%">
                    <el-option v-for="domain in domainList" :key="domain.id" :label="domain.name" :value="domain.id" />
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item :label="$t('standard.element.unitLabel')">
                  <el-select v-model="element.unit_id" clearable filterable :placeholder="$t('standard.element.selectUnit')" style="width:100%">
                    <el-option-group v-for="cat in unitsByCategory" :key="cat.id" :label="cat.name">
                      <el-option v-for="u in cat.units" :key="u.id" :label="`${u.name}（${u.symbol}）`" :value="u.id" />
                    </el-option-group>
                  </el-select>
                </el-form-item>
              </el-col>
            </el-row>
            <el-row :gutter="20">
              <el-col :span="12">
                <el-form-item :label="$t('standard.element.securityLevelLabel')">
                  <el-select v-model="element.security_level" clearable :placeholder="$t('standard.element.selectSecurityLevel')" style="width:100%">
                    <el-option v-for="g in gradingLevels" :key="g.level" :label="`${g.level} ${g.name}`" :value="g.level" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('standard.element.classificationLabel')">
                  <el-tree-select
                    v-model="element.classification_id"
                    :data="classificationTree"
                    :props="{ label: 'name', value: 'id', children: 'children' }"
                    clearable :placeholder="$t('standard.element.selectClassification')" style="width:100%"
                  />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item :label="$t('standard.element.codeSetLabel')">
              <el-select v-model="element.code_set_id" clearable filterable style="width: 100%" @change="handleCodeSetChange">
                <el-option
                  v-for="cs in codeSets"
                  :key="cs.id"
                  :label="`${cs.name} (${cs.code})`"
                  :value="cs.id"
                />
              </el-select>
            </el-form-item>
            <el-form-item :label="$t('standard.element.defaultValueLabel')">
              <el-input v-model="element.default_value" />
            </el-form-item>
            <el-form-item :label="$t('standard.element.definitionLabel')">
              <el-input v-model="element.definition" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item :label="$t('standard.element.exampleValuesLabel')">
              <el-select
                v-model="element.example_values"
                multiple
                filterable
                allow-create
                default-first-option
                :placeholder="$t('standard.element.exampleValuesPlaceholder')"
                style="width: 100%"
              />
            </el-form-item>
          </el-form>
        </el-card>

        <!-- 质量规则 -->
        <el-card class="section-card">
          <template #header>
            <div class="card-header">
              <h3><el-icon class="header-icon"><CircleCheck /></el-icon>{{ $t('standard.element.qualityRules') }}</h3>
              <el-button v-if="canUpdate" size="small" @click="addRule">{{ $t('standard.element.addRule') }}</el-button>
            </div>
          </template>
          <div v-if="!qualityRules || qualityRules.length === 0" class="empty-rules">
            <el-empty :description="$t('standard.element.noRules')" />
          </div>
          <div v-else class="rules-list">
            <div v-for="(rule, index) in qualityRules" :key="rule.rule_key" class="rule-item">
              <div class="rule-header">
                <el-checkbox v-model="rule.enabled">{{ $t('standard.element.ruleEnabled') }}</el-checkbox>
                <el-select v-model="rule.type" size="small" style="width: 140px" @change="handleRuleTypeChange(rule)">
                  <el-option :label="$t('standard.element.ruleNotNull')" value="not_null" />
                  <el-option :label="$t('standard.element.ruleFormat')" value="format" />
                  <el-option :label="$t('standard.element.ruleLength')" value="length" />
                  <el-option :label="$t('standard.element.ruleUnique')" value="unique" />
                  <el-option :label="$t('standard.element.ruleValueRange')" value="value_range" />
                  <el-option :label="$t('standard.element.ruleAllowedValues')" value="allowed_values" />
                </el-select>
                <el-select v-model="rule.severity" size="small" style="width: 100px">
                  <el-option :label="$t('standard.element.severityError')" value="error" />
                  <el-option :label="$t('standard.element.severityWarning')" value="warning" />
                  <el-option :label="$t('standard.element.severityInfo')" value="info" />
                </el-select>
                <el-button v-if="canUpdate" link type="danger" @click="removeRule(index)">{{ $t('standard.common.delete') }}</el-button>
              </div>
              <el-input
                v-model="rule.message"
                :placeholder="$t('standard.element.ruleMessage')"
                size="small"
                class="rule-message"
              />
              <div v-if="rule.type === 'format'" class="rule-params">
                <el-input
                  v-model="rule.params.pattern"
                  :placeholder="$t('standard.element.ruleFormatPlaceholder')"
                  size="small"
                />
              </div>
              <div v-if="rule.type === 'length'" class="rule-params">
                <el-row :gutter="10">
                  <el-col :span="12">
                    <el-input-number v-model="rule.params.min" :placeholder="$t('standard.element.ruleLengthMin')" size="small" style="width: 100%" />
                  </el-col>
                  <el-col :span="12">
                    <el-input-number v-model="rule.params.max" :placeholder="$t('standard.element.ruleLengthMax')" size="small" style="width: 100%" />
                  </el-col>
                </el-row>
              </div>
              <div v-if="rule.type === 'value_range'" class="rule-params">
                <el-row :gutter="10" style="margin-top: 8px">
                  <el-col :span="12">
                    <el-input-number v-model="rule.params.min" :placeholder="$t('standard.element.ruleMinPlaceholder')" size="small" style="width: 100%" />
                  </el-col>
                  <el-col :span="12">
                    <el-input-number v-model="rule.params.max" :placeholder="$t('standard.element.ruleMaxPlaceholder')" size="small" style="width: 100%" />
                  </el-col>
                </el-row>
              </div>
              <div v-if="rule.type === 'allowed_values'" class="rule-params">
                <el-select
                  v-model="rule.params.values"
                  multiple
                  filterable
                  allow-create
                  default-first-option
                  :placeholder="$t('standard.element.ruleEnumPlaceholder')"
                  size="small"
                  style="width: 100%"
                />
              </div>
            </div>
          </div>
        </el-card>

        <!-- 关联的码值项（只读展示） -->
        <el-card class="section-card" v-if="element.code_set_id">
          <template #header><h3><el-icon class="header-icon"><List /></el-icon>{{ $t('standard.element.codeItems') }}</h3></template>
          <el-table :data="codeItems" v-loading="codeItemsLoading" size="small" max-height="300">
            <el-table-column :label="$t('standard.common.code')" prop="code" width="120" />
            <el-table-column :label="$t('standard.codeSet.itemValue')" prop="value" min-width="140" />
            <el-table-column :label="$t('standard.common.description')" prop="description" show-overflow-tooltip />
          </el-table>
        </el-card>

        <!-- 关联文档 -->
        <DocumentPanel v-if="element.id" entity-type="element" :entity-id="element.id" v-model:entity-version="element.version" />
      </el-col>

      <el-col :span="8">
        <!-- 元数据信息 -->
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.common.metadata') }}</h3></template>
          <el-descriptions :column="1" size="small">
            <el-descriptions-item :label="$t('standard.common.id')">{{ element.id }}</el-descriptions-item>
            <el-descriptions-item :label="$t('standard.common.createdAt')">{{ formatTime(element.created_at) }}</el-descriptions-item>
            <el-descriptions-item :label="$t('standard.common.updatedAt')">{{ formatTime(element.updated_at) }}</el-descriptions-item>
          </el-descriptions>
        </el-card>

        <!-- 关联的业务术语（只读） -->
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.element.relatedGlossaries') }}</h3></template>
          <div v-if="relatedGlossaries.length === 0" style="color: var(--el-text-color-secondary); font-size: 13px; padding: 8px 0;">
            {{ $t('standard.element.noGlossaries') }}
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
import { ref, onMounted, computed, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useConsolePageDescriptor } from '@common-ui'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, CircleCheck, List } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { domainAPI, elementAPI, codeSetAPI, glossaryAPI, unitAPI, classificationAPI, gradingLevelAPI } from '../api/standard'
import DocumentPanel from '../components/DocumentPanel.vue'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { formatStandardDateTime } from '../utils/dateTime'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { useActionLock } from '../composables/useActionLock'
import { useUnsavedChanges } from '../composables/useUnsavedChanges'

const router = useRouter()
const route = useRoute()
const { t, locale } = useI18n()
const { canUpdate, canApprove } = useStandardPermissions('element')
const { isLocked: isActionLocked, runLocked } = useActionLock()
const loading = ref(false)
const saving = ref(false)
const actionKey = computed(() => `element:${route.params.id}`)
const element = ref({})
useConsolePageDescriptor(router, 'standard', {
  title: computed(() => t('standard.element.recentVisitTitle')),
  subject: computed(() => element.value?.name || ''),
  ready: computed(() => Boolean(element.value?.name))
})
const codeSets = ref([])
const domainList = ref([])
const codeItems = ref([])
const codeItemsLoading = ref(false)
const relatedGlossaries = ref([])
const units = ref([])
const gradingLevels = ref([])
const classifications = ref([])
const editableState = computed(() => {
  const {
    id,
    status,
    created_at,
    updated_at,
    created_by,
    updated_by,
    ...editable
  } = element.value
  return editable
})
const { isDirty, markSaved } = useUnsavedChanges({ state: editableState, t })

const unitsByCategory = computed(() => {
  const map = {}
  units.value.forEach(u => {
    const catId = u.category_id || 0
    const catName = u.category?.name || t('standard.element.other')
    if (!map[catId]) map[catId] = { id: catId, name: catName, units: [] }
    map[catId].units.push(u)
  })
  return Object.values(map)
})

const classificationTree = computed(() => buildTree(classifications.value))
function buildTree(list, parentId = null) {
  return list.filter(i => (i.parent_id || null) === parentId).map(i => ({ ...i, children: buildTree(list, i.id) }))
}

const flattenDomains = (nodes) => {
  const result = []
  const traverse = (list) => {
    for (const node of list) {
      result.push(node)
      if (node.children) traverse(node.children)
    }
  }
  traverse(nodes)
  return result
}

const qualityRules = computed({
  get() {
    if (!element.value.quality_rules) return []
    const qr = element.value.quality_rules
    if (qr.schema_version === 'addp.quality.rules/v1' && Array.isArray(qr.rules)) return qr.rules
    return []
  },
  set(val) {
    if (!element.value.quality_rules) {
      element.value.quality_rules = { schema_version: 'addp.quality.rules/v1', rules: val }
    } else {
      element.value.quality_rules.rules = val
    }
  }
})

const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({
  draft: t('standard.common.draft'),
  approved: t('standard.common.approved'),
  deprecated: t('standard.common.deprecated')
}[s] || s)

const goToGlossary = (id) => {
  navigateStandardRoute(router, `/glossaries/${id}`)
}

const formatTime = (time) => {
  return formatStandardDateTime(time, locale.value)
}

const goBack = () => navigateStandardRoute(router, { path: '/elements', query: route.query }, { history: 'replace' })

const loadElement = async () => {
  loading.value = true
  try {
    const res = await elementAPI.get(route.params.id)
    element.value = res || {}
    if (!element.value.quality_rules) {
      element.value.quality_rules = { schema_version: 'addp.quality.rules/v1', rules: [] }
    } else if (element.value.quality_rules.schema_version !== 'addp.quality.rules/v1' || !Array.isArray(element.value.quality_rules.rules)) {
      throw new Error('invalid quality rules document')
    }
    if (element.value.code_set_id) {
      loadCodeItems(element.value.code_set_id)
    }
    markSaved()
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.loadFailed'))
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
    codeSets.value = []
  }
}

const loadDomains = async () => {
  try {
    const res = await domainAPI.list()
    domainList.value = flattenDomains(res || [])
  } catch (e) {
    domainList.value = []
  }
}

const loadRelatedGlossaries = async () => {
  try {
    const res = await glossaryAPI.list({ element_id: route.params.id })
    relatedGlossaries.value = res || []
  } catch (e) {
    relatedGlossaries.value = []
  }
}

const loadUnits = async () => {
  try {
    const res = await unitAPI.list({ page_size: 500 })
    units.value = res || []
  } catch (e) {
    units.value = []
  }
}

const loadGradingLevels = async () => {
  try {
    const res = await gradingLevelAPI.list()
    gradingLevels.value = res || []
  } catch (e) {
    gradingLevels.value = []
  }
}

const loadClassifications = async () => {
  try {
    const res = await classificationAPI.list()
    classifications.value = res || []
  } catch (e) {
    classifications.value = []
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
    codeItems.value = res || []
  } catch (e) {
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
    rule_key: globalThis.crypto.randomUUID(),
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

const handleRuleTypeChange = (rule) => {
  rule.params = {}
}

const saveChanges = async () => {
  if (saving.value) return
  saving.value = true
  try {
    await elementAPI.update(route.params.id, element.value)
    ElMessage.success(t('standard.common.saveSuccess'))
    await loadElement()
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  if (isDirty.value) {
    ElMessage.warning(t('standard.common.saveBeforeAction'))
    return
  }
  await runLocked(actionKey.value, async () => {
    try {
      await ElMessageBox.confirm(t('standard.element.confirmApprove'), t('standard.common.hint'), { type: 'info' })
      await elementAPI.approve(route.params.id, element.value.version)
      ElMessage.success(t('standard.common.approveSuccess'))
      await loadElement()
    } catch (e) {
      if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.approveFailed'))
    }
  })
}

watch(() => route.params.id, () => {
  loadElement()
  loadRelatedGlossaries()
}, { immediate: true })

onMounted(() => {
  loadDomains()
  loadCodeSets()
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

@media (max-width: 768px) {
  .element-detail { padding: 12px; }
  .page-header { align-items: flex-start; flex-wrap: wrap; gap: 10px; }
  .header-left, .header-right { flex-wrap: wrap; }
  .element-detail :deep(.el-row) { margin-left: 0 !important; margin-right: 0 !important; }
  .element-detail :deep(.el-col) { max-width: 100%; flex: 0 0 100%; }
  .element-detail :deep(.el-col + .el-col) { margin-top: 12px; }
}
</style>
