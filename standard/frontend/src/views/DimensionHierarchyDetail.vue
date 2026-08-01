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
                <span v-if="row.element_id" class="text-link">
                  Element#{{ row.element_id }}
                </span>
                <span v-else class="text-muted">—</span>
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.common.description')" prop="description" min-width="160" show-overflow-tooltip />
            <el-table-column :label="$t('standard.common.actions')" width="120" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openLevelDialog(row)">{{ $t('standard.common.edit') }}</el-button>
                <el-popconfirm :title="$t('standard.dimHierarchy.confirmDeleteLevel')" @confirm="handleDeleteLevel(row.id)">
                  <template #reference>
                    <el-button link type="danger">{{ $t('standard.common.delete') }}</el-button>
                  </template>
                </el-popconfirm>
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
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Plus } from '@element-plus/icons-vue'
import { dimensionHierarchyAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const goBack = () => navigateStandardRoute(router, '/dimension-hierarchies', { history: 'replace' })
const hierarchyId = parseInt(route.params.id)

const hierarchy = ref({})
const form = reactive({ name: '', description: '', domain_id: null })
const saving = ref(false)

const levels = ref([])
const levelLoading = ref(false)
const levelVisible = ref(false)
const levelSaving = ref(false)
const levelFormRef = ref(null)
const editingLevel = ref(null)
const levelForm = reactive({ level_num: 1, name: '', description: '', sort_order: 0 })
const levelRules = computed(() => ({
  level_num: [{ required: true, message: t('standard.dimHierarchy.levelNumRequired'), trigger: 'blur' }],
  name: [{ required: true, message: t('standard.dimHierarchy.levelNameRequired'), trigger: 'blur' }]
}))

const sortedLevels = computed(() => [...levels.value].sort((a, b) => a.level_num - b.level_num))

async function loadHierarchy() {
  try {
    const res = await dimensionHierarchyAPI.get(hierarchyId)
    const data = res
    hierarchy.value = data
    form.name = data.name
    form.description = data.description
    form.domain_id = data.domain_id
    levels.value = data.levels || []
  } catch {
    ElMessage.error(t('standard.dimHierarchy.loadFailed'))
  }
}

async function handleSave() {
  saving.value = true
  try {
    await dimensionHierarchyAPI.update(hierarchyId, { name: form.name, description: form.description, domain_id: form.domain_id })
    ElMessage.success(t('standard.dimHierarchy.saveSuccess'))
    hierarchy.value.name = form.name
  } catch {
    ElMessage.error(t('standard.dimHierarchy.saveFailed'))
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
      description: level.description || '',
      sort_order: level.sort_order || 0
    })
  } else {
    Object.assign(levelForm, {
      level_num: (levels.value.length > 0 ? Math.max(...levels.value.map(l => l.level_num)) + 1 : 1),
      name: '',
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
      const res = await dimensionHierarchyAPI.updateLevel(hierarchyId, editingLevel.value.id, { ...levelForm })
      const idx = levels.value.findIndex(l => l.id === editingLevel.value.id)
      if (idx >= 0) levels.value[idx] = res
    } else {
      const res = await dimensionHierarchyAPI.createLevel(hierarchyId, { ...levelForm })
      levels.value.push(res)
    }
    levelVisible.value = false
    ElMessage.success(t('standard.dimHierarchy.saveSuccess'))
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('standard.dimHierarchy.saveFailed'))
  } finally {
    levelSaving.value = false
  }
}

async function handleDeleteLevel(levelId) {
  try {
    await dimensionHierarchyAPI.deleteLevel(hierarchyId, levelId)
    levels.value = levels.value.filter(l => l.id !== levelId)
    ElMessage.success(t('standard.dimHierarchy.levelDeleted'))
  } catch {
    ElMessage.error(t('standard.dimHierarchy.levelDeleteFailed'))
  }
}

onMounted(loadHierarchy)
</script>

<style scoped>
.detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}
.header-left { display: flex; align-items: center; gap: 10px; }
.header-right { display: flex; gap: 8px; }
.title { font-size: 18px; font-weight: 600; }
.card-header-with-action { display: flex; justify-content: space-between; align-items: center; }
.card-title { font-weight: 600; }
.hierarchy-chain { margin-top: 16px; padding: 12px 16px; background: #f5f7fa; border-radius: 6px; }
.arrow { color: #909399; margin: 0 4px; }
.form-tip { font-size: 12px; color: #909399; margin-top: 4px; }
.text-link { color: #409eff; cursor: pointer; }
.text-muted { color: #c0c4cc; }
.info-card { margin-bottom: 0; }
</style>
