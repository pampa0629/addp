<template>
  <div class="classification-list">
    <div class="page-header">
      <h2>{{ $t('standard.classification.title') }}</h2>
    </div>

    <el-row :gutter="20">
      <!-- 左侧：数据分类树 -->
      <el-col :span="12">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('standard.classification.classificationTitle') }}</span>
              <el-button size="small" type="primary" @click="openAddClassification(null)">{{ $t('standard.classification.addClassification') }}</el-button>
            </div>
          </template>
          <el-tree
            v-loading="loadingClassifications"
            :data="classificationTree"
            :props="{ label: 'name', children: 'children' }"
            node-key="id"
            default-expand-all
          >
            <template #default="{ data }">
              <div class="tree-node">
                <span>{{ data.name }}</span>
                <span class="tree-code">{{ data.code }}</span>
                <div class="tree-actions">
                  <el-button link size="small" @click.stop="openAddClassification(data.id)">{{ $t('standard.classification.addChild') }}</el-button>
                  <el-button link size="small" @click.stop="editClassification(data)">{{ $t('standard.common.edit') }}</el-button>
                  <el-button link size="small" type="danger" @click.stop="deleteClassification(data)">{{ $t('standard.common.delete') }}</el-button>
                </div>
              </div>
            </template>
          </el-tree>
          <el-empty v-if="!loadingClassifications && classificationTree.length === 0" :description="$t('standard.classification.empty')" />
        </el-card>
      </el-col>

      <!-- 右侧：数据分级 L1-L4 -->
      <el-col :span="12">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('standard.classification.gradingTitle') }}</span>
              <el-tooltip :content="$t('standard.classification.gradingTooltip')">
                <el-icon><QuestionFilled /></el-icon>
              </el-tooltip>
            </div>
          </template>
          <div v-loading="loadingGrading" class="grading-list">
            <div v-for="level in gradingLevels" :key="level.id" class="grading-item">
              <div class="grading-left">
                <el-tag :style="{ backgroundColor: level.color, color: '#fff', borderColor: level.color }" size="default">
                  {{ level.level }}
                </el-tag>
                <div class="grading-info">
                  <div class="grading-name">{{ level.name }}</div>
                  <div class="grading-desc">{{ level.description }}</div>
                </div>
              </div>
              <el-button size="small" @click="editGradingLevel(level)">{{ $t('standard.common.edit') }}</el-button>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 新增/编辑数据分类对话框 -->
    <el-dialog v-model="showClassificationDialog" :title="editingClassification ? $t('standard.classification.editClassification') : $t('standard.classification.createClassification')" width="420px">
      <el-form :model="classificationForm" label-width="80px">
        <el-form-item :label="$t('standard.common.name')" required>
          <el-input v-model="classificationForm.name" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.code')" required>
          <el-input v-model="classificationForm.code" :disabled="!!editingClassification" />
        </el-form-item>
        <el-form-item :label="$t('standard.classification.parentLabel')">
          <el-tree-select
            v-model="classificationForm.parent_id"
            :data="classificationTree"
            :props="{ label: 'name', value: 'id', children: 'children' }"
            clearable
            :placeholder="$t('standard.classification.parentPlaceholder')"
            style="width:100%"
          />
        </el-form-item>
        <el-form-item :label="$t('standard.common.description')">
          <el-input v-model="classificationForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.sort')">
          <el-input-number v-model="classificationForm.sort_order" :min="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showClassificationDialog = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="saveClassification" :loading="saving">{{ $t('standard.common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 编辑分级对话框 -->
    <el-dialog v-model="showGradingDialog" :title="$t('standard.classification.editGrading')" width="400px">
      <el-form :model="gradingForm" label-width="80px">
        <el-form-item :label="$t('standard.classification.levelLabel')">
          <el-input :value="editingGrading?.level" disabled />
        </el-form-item>
        <el-form-item :label="$t('standard.common.name')">
          <el-input v-model="gradingForm.name" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.description')">
          <el-input v-model="gradingForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="$t('standard.classification.colorLabel')">
          <el-color-picker v-model="gradingForm.color" />
          <span class="color-hint">{{ gradingForm.color }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showGradingDialog = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="saveGradingLevel" :loading="saving">{{ $t('standard.common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'
import { classificationAPI, gradingLevelAPI } from '../api/standard'

const { t } = useI18n()
const classifications = ref([])
const gradingLevels = ref([])
const loadingClassifications = ref(false)
const loadingGrading = ref(false)
const saving = ref(false)

const showClassificationDialog = ref(false)
const showGradingDialog = ref(false)
const editingClassification = ref(null)
const editingGrading = ref(null)

const classificationForm = ref({ name: '', code: '', parent_id: null, description: '', sort_order: 0 })
const gradingForm = ref({ name: '', description: '', color: '' })

// 构建树形结构
const classificationTree = computed(() => buildTree(classifications.value))

function buildTree(list, parentId = null) {
  return list
    .filter(item => (item.parent_id || null) === parentId)
    .map(item => ({ ...item, children: buildTree(list, item.id) }))
}

const loadClassifications = async () => {
  loadingClassifications.value = true
  try {
    const res = await classificationAPI.list()
    classifications.value = res || []
  } finally {
    loadingClassifications.value = false
  }
}

const loadGradingLevels = async () => {
  loadingGrading.value = true
  try {
    const res = await gradingLevelAPI.list()
    gradingLevels.value = res || []
  } finally {
    loadingGrading.value = false
  }
}

const openAddClassification = (parentId) => {
  editingClassification.value = null
  classificationForm.value = { name: '', code: '', parent_id: parentId, description: '', sort_order: 0 }
  showClassificationDialog.value = true
}

const editClassification = (data) => {
  editingClassification.value = data
  classificationForm.value = { name: data.name, code: data.code, parent_id: data.parent_id, description: data.description, sort_order: data.sort_order }
  showClassificationDialog.value = true
}

const saveClassification = async () => {
  saving.value = true
  try {
    if (editingClassification.value) {
      await classificationAPI.update(editingClassification.value.id, classificationForm.value)
      ElMessage.success(t('standard.common.updateSuccess'))
    } else {
      await classificationAPI.create(classificationForm.value)
      ElMessage.success(t('standard.common.createSuccess'))
    }
    showClassificationDialog.value = false
    await loadClassifications()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('standard.common.operationFailed'))
  } finally {
    saving.value = false
  }
}

const deleteClassification = async (data) => {
  try {
    await ElMessageBox.confirm(t('standard.classification.confirmDelete', { name: data.name }), t('standard.common.hint'), { type: 'warning' })
    await classificationAPI.delete(data.id)
    ElMessage.success(t('standard.common.deleteSuccess'))
    await loadClassifications()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.response?.data?.error || t('standard.common.deleteFailed'))
  }
}

const editGradingLevel = (level) => {
  editingGrading.value = level
  gradingForm.value = { name: level.name, description: level.description, color: level.color }
  showGradingDialog.value = true
}

const saveGradingLevel = async () => {
  saving.value = true
  try {
    await gradingLevelAPI.update(editingGrading.value.id, gradingForm.value)
    ElMessage.success(t('standard.common.updateSuccess'))
    showGradingDialog.value = false
    await loadGradingLevels()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('standard.common.operationFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  loadClassifications()
  loadGradingLevels()
})
</script>

<style scoped>
.classification-list { padding: 20px; }
.page-header { margin-bottom: 20px; }
.page-header h2 { margin: 0; font-size: 18px; color: var(--el-text-color-primary); }
.card-header { display: flex; justify-content: space-between; align-items: center; gap: 8px; }
.tree-node { display: flex; align-items: center; gap: 8px; width: 100%; }
.tree-code { font-size: 12px; color: var(--el-text-color-secondary); }
.tree-actions { margin-left: auto; }
.grading-list { display: flex; flex-direction: column; gap: 12px; }
.grading-item { display: flex; align-items: center; justify-content: space-between; padding: 12px; border: 1px solid var(--el-border-color); border-radius: 6px; }
.grading-left { display: flex; align-items: center; gap: 12px; }
.grading-name { font-weight: 500; font-size: 14px; }
.grading-desc { font-size: 12px; color: var(--el-text-color-secondary); margin-top: 2px; }
.color-hint { margin-left: 8px; font-size: 12px; color: var(--el-text-color-secondary); }
</style>
