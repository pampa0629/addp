<template>
  <div class="dim-hierarchy-detail">
    <div class="detail-header">
      <div class="header-left">
        <el-button text @click="goBack">
          <el-icon><ArrowLeft /></el-icon>{{ $t('standard.common.back') }}
        </el-button>
        <span class="title">{{ hierarchy.name || $t('standard.common.loadFailed') }}</span>
        <el-tag type="info" size="small">{{ hierarchy.code }}</el-tag>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="handleSave" :loading="saving">{{ $t('standard.dimHierarchy.saveBasicInfo') }}</el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <!-- 基本信息 -->
      <el-col :span="24">
        <el-card shadow="never" class="info-card">
          <template #header><span class="card-title">{{ $t('standard.dimHierarchy.basicInfo') }}</span></template>
          <el-form :model="form" label-width="90px">
            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item :label="$t('standard.dimHierarchy.nameLabel')">
                  <el-input v-model="form.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item :label="$t('standard.common.description')">
                  <el-input v-model="form.description" />
                </el-form-item>
              </el-col>
            </el-row>
          </el-form>
        </el-card>
      </el-col>

      <!-- 层级定义 -->
      <el-col :span="24" style="margin-top:16px">
        <el-card shadow="never">
          <template #header>
            <div class="card-header-with-action">
              <span class="card-title">{{ $t('standard.dimHierarchy.levelDefinition') }}</span>
              <el-button type="primary" size="small" @click="openLevelDialog()">
                <el-icon><Plus /></el-icon>{{ $t('standard.dimHierarchy.addLevel') }}
              </el-button>
            </div>
          </template>

          <el-table :data="levels" v-loading="levelLoading" stripe>
            <el-table-column :label="$t('standard.dimHierarchy.levelNum')" prop="level_num" width="100">
              <template #default="{ row }">
                <el-tag size="small">L{{ row.level_num }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.dimHierarchy.levelName')" prop="name" min-width="120" />
            <el-table-column :label="$t('standard.dimHierarchy.relatedElement')" width="120">
              <template #default="{ row }">
                <el-link v-if="row.element_id" type="primary" @click="openElement(row.element_id)">
                  {{ elementNames[row.element_id] || `#${row.element_id}` }}
                </el-link>
                <span v-else class="text-muted">-</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.common.description')" prop="description" min-width="160" show-overflow-tooltip />
            <el-table-column :label="$t('standard.common.actions')" width="150" fixed="right">
              <template #default="{ row }">
                <div class="table-actions">
                  <el-button link type="primary" @click="openLevelDialog(row)">{{ $t('standard.common.edit') }}</el-button>
                  <el-button link type="danger" @click="handleDeleteLevel(row)">{{ $t('standard.common.delete') }}</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>

          <!-- 可视化层级链 -->
          <div v-if="levels.length" class="hierarchy-chain">
            <span v-for="(lvl, idx) in sortedLevels" :key="lvl.id">
              <el-tag>{{ lvl.name }}</el-tag>
              <span v-if="idx < sortedLevels.length - 1" class="arrow"> → </span>
            </span>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 层级对话框 -->
    <el-dialog v-model="levelVisible" :title="editingLevel ? $t('standard.dimHierarchy.editLevel') : $t('standard.dimHierarchy.addLevelTitle')" width="440px">
      <el-form ref="levelFormRef" :model="levelForm" :rules="levelRules" label-width="90px">
        <el-form-item :label="$t('standard.dimHierarchy.levelNumLabel')" prop="level_num">
          <el-input-number v-model="levelForm.level_num" :min="1" :max="20" style="width:100%" />
          <div class="form-tip">{{ $t('standard.dimHierarchy.levelNumTip') }}</div>
        </el-form-item>
        <el-form-item :label="$t('standard.dimHierarchy.levelNameLabel')" prop="name">
          <el-input v-model="levelForm.name" :placeholder="$t('standard.dimHierarchy.levelNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.dimHierarchy.relatedElement')">
          <el-select v-model="levelForm.element_id" clearable filterable :placeholder="$t('standard.dimHierarchy.elementPlaceholder')" style="width:100%">
            <el-option v-for="element in elementOptions" :key="element.id" :label="`${element.name} (${element.code})`" :value="element.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.dimHierarchy.levelDescription')">
          <el-input v-model="levelForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="$t('standard.dimHierarchy.levelSort')">
          <el-input-number v-model="levelForm.sort_order" :min="0" style="width:100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="levelVisible = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSaveLevel" :loading="levelSaving">{{ $t('standard.common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Plus } from '@element-plus/icons-vue'
import { dimensionHierarchyAPI, elementAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const hierarchyId = computed(() => Number(route.params.id))
const goBack = () => navigateStandardRoute(router, {
  path: '/dimension-hierarchies',
  query: route.query
}, { history: 'replace' })

const hierarchy = ref({})
const form = reactive({ name: '', description: '', domain_id: null })
const saving = ref(false)

const levels = ref([])
const elementNames = reactive({})
const elementOptions = ref([])
const levelLoading = ref(false)
const levelVisible = ref(false)
const levelSaving = ref(false)
const levelFormRef = ref(null)
const editingLevel = ref(null)
const levelForm = reactive({ level_num: 1, name: '', element_id: null, description: '', sort_order: 0 })
const levelRules = computed(() => ({
  level_num: [{ required: true, message: t('standard.dimHierarchy.levelNumRequired'), trigger: 'blur' }],
  name: [{ required: true, message: t('standard.dimHierarchy.levelNameRequired'), trigger: 'blur' }]
}))

const sortedLevels = computed(() => [...levels.value].sort((a, b) => a.level_num - b.level_num))

async function loadHierarchy() {
  const id = hierarchyId.value
  if (!Number.isInteger(id) || id <= 0) return
  try {
    const res = await dimensionHierarchyAPI.get(id)
    const data = res
    hierarchy.value = data
    form.name = data.name
    form.description = data.description
    form.domain_id = data.domain_id
    levels.value = data.levels || []
    await Promise.all([loadElementOptions(), loadElementNames(levels.value)])
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.dimHierarchy.loadFailed'))
  }
}

async function loadElementOptions() {
  if (elementOptions.value.length > 0) return
  try {
    const result = await elementAPI.list({ page: 1, page_size: 200 })
    elementOptions.value = result.data || []
    for (const element of elementOptions.value) {
      elementNames[element.id] = element.name || element.code || `#${element.id}`
    }
  } catch {
    elementOptions.value = []
  }
}

async function loadElementNames(items) {
  const ids = [...new Set(items.map(item => item.element_id).filter(Boolean))]
  await Promise.all(ids.map(async id => {
    try {
      const element = await elementAPI.get(id)
      elementNames[id] = element.name || element.code || `#${id}`
    } catch {
      elementNames[id] = `#${id}`
    }
  }))
}

function openElement(id) {
  navigateStandardRoute(router, `/elements/${id}`)
}

async function handleSave() {
  saving.value = true
  try {
    await dimensionHierarchyAPI.update(hierarchyId.value, { name: form.name, description: form.description, domain_id: form.domain_id })
    ElMessage.success(t('standard.dimHierarchy.saveSuccess'))
    hierarchy.value.name = form.name
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.dimHierarchy.saveFailed'))
  } finally {
    saving.value = false
  }
}

function openLevelDialog(level = null) {
  editingLevel.value = level
  if (level) {
    Object.assign(levelForm, {
      level_num: level.level_num,
      name: level.name,
      element_id: level.element_id || null,
      description: level.description || '',
      sort_order: level.sort_order || 0
    })
  } else {
    Object.assign(levelForm, {
      level_num: (levels.value.length > 0 ? Math.max(...levels.value.map(l => l.level_num)) + 1 : 1),
      name: '',
      element_id: null,
      description: '',
      sort_order: levels.value.length
    })
  }
  levelVisible.value = true
}

async function handleSaveLevel() {
  await levelFormRef.value.validate()
  levelSaving.value = true
  try {
    if (editingLevel.value) {
      const res = await dimensionHierarchyAPI.updateLevel(hierarchyId.value, editingLevel.value.id, { ...levelForm })
      const idx = levels.value.findIndex(l => l.id === editingLevel.value.id)
      if (idx >= 0) levels.value[idx] = res
    } else {
      const res = await dimensionHierarchyAPI.createLevel(hierarchyId.value, { ...levelForm })
      levels.value.push(res)
      await loadElementNames([res])
    }
    levelVisible.value = false
    ElMessage.success(t('standard.dimHierarchy.saveSuccess'))
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.dimHierarchy.saveFailed'))
  } finally {
    levelSaving.value = false
  }
}

async function handleDeleteLevel(level) {
  try {
    await ElMessageBox.confirm(t('standard.dimHierarchy.confirmDeleteLevel', { name: level.name }), t('standard.common.hint'), { type: 'warning' })
    await dimensionHierarchyAPI.deleteLevel(hierarchyId.value, level.id)
    levels.value = levels.value.filter(l => l.id !== level.id)
    ElMessage.success(t('standard.dimHierarchy.levelDeleted'))
  } catch (e) {
    if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.dimHierarchy.levelDeleteFailed'))
  }
}

watch(() => route.params.id, loadHierarchy, { immediate: true })
</script>

<style scoped>
.dim-hierarchy-detail { min-height: 100%; padding: 20px; color: var(--addp-text-primary); background: var(--addp-bg-secondary); }
.detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.header-left { display: flex; align-items: center; gap: 10px; }
.header-right { display: flex; gap: 8px; }
.title { font-size: 18px; font-weight: 600; color: var(--addp-text-primary); }
.dim-hierarchy-detail :deep(.el-card) { color: var(--addp-text-primary); background: var(--addp-bg-primary); border-color: var(--addp-border-color); box-shadow: var(--addp-shadow-card); }
.dim-hierarchy-detail :deep(.el-table) {
  --el-table-bg-color: var(--addp-bg-primary);
  --el-table-tr-bg-color: var(--addp-bg-primary);
  --el-table-header-bg-color: var(--addp-bg-secondary);
  --el-table-border-color: var(--addp-border-color-light);
  --el-table-text-color: var(--addp-text-primary);
  --el-table-header-text-color: var(--addp-text-secondary);
}
.card-header-with-action { display: flex; justify-content: space-between; align-items: center; gap: 12px; }
.card-title { font-weight: 600; color: var(--addp-text-primary); }
.table-actions { display: inline-flex; align-items: center; justify-content: flex-start; gap: 8px; min-width: max-content; white-space: nowrap; }
.table-actions :deep(.el-button) { white-space: nowrap; }
.hierarchy-chain { margin-top: 16px; padding: 12px 16px; background: var(--addp-bg-secondary); border: 1px solid var(--addp-border-color-light); border-radius: 6px; }
.arrow { color: var(--addp-text-tertiary); margin: 0 4px; }
.form-tip { font-size: 12px; color: var(--addp-text-secondary); margin-top: 4px; }
.text-muted { color: var(--addp-text-tertiary); }
.info-card { margin-bottom: 0; }

@media (max-width: 768px) {
  .dim-hierarchy-detail { padding: 12px; }
  .detail-header { align-items: flex-start; flex-wrap: wrap; gap: 10px; }
  .header-left, .header-right { flex-wrap: wrap; }
  .dim-hierarchy-detail :deep(.el-row) { margin-left: 0 !important; margin-right: 0 !important; }
  .dim-hierarchy-detail :deep(.el-col) { max-width: 100%; flex: 0 0 100%; }
  .dim-hierarchy-detail :deep(.el-col + .el-col) { margin-top: 12px; }
  .dim-hierarchy-detail :deep(.info-card .el-row) { row-gap: 0; }
}
</style>
