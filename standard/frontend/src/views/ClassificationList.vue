<template>
  <div class="classification-list">
    <div class="page-header">
      <h2>数据分类与分级</h2>
    </div>

    <el-row :gutter="20">
      <!-- 左侧：数据分类树 -->
      <el-col :span="12">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>数据分类</span>
              <el-button size="small" type="primary" @click="openAddClassification(null)">新增</el-button>
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
                  <el-button link size="small" @click.stop="openAddClassification(data.id)">子级</el-button>
                  <el-button link size="small" @click.stop="editClassification(data)">编辑</el-button>
                  <el-button link size="small" type="danger" @click.stop="deleteClassification(data)">删除</el-button>
                </div>
              </div>
            </template>
          </el-tree>
          <el-empty v-if="!loadingClassifications && classificationTree.length === 0" description="暂无数据分类" />
        </el-card>
      </el-col>

      <!-- 右侧：数据分级 L1-L4 -->
      <el-col :span="12">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>数据分级标准</span>
              <el-tooltip content="分级采用固定的 L1-L4 四级结构，可修改名称、描述和颜色标签">
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
              <el-button size="small" @click="editGradingLevel(level)">编辑</el-button>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <!-- 新增/编辑数据分类对话框 -->
    <el-dialog v-model="showClassificationDialog" :title="editingClassification ? '编辑数据分类' : '新增数据分类'" width="420px">
      <el-form :model="classificationForm" label-width="80px">
        <el-form-item label="名称" required>
          <el-input v-model="classificationForm.name" />
        </el-form-item>
        <el-form-item label="编码" required>
          <el-input v-model="classificationForm.code" :disabled="!!editingClassification" />
        </el-form-item>
        <el-form-item label="上级分类">
          <el-tree-select
            v-model="classificationForm.parent_id"
            :data="classificationTree"
            :props="{ label: 'name', value: 'id', children: 'children' }"
            clearable
            placeholder="根节点（不选则为顶级）"
            style="width:100%"
          />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="classificationForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="classificationForm.sort_order" :min="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showClassificationDialog = false">取消</el-button>
        <el-button type="primary" @click="saveClassification" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <!-- 编辑分级对话框 -->
    <el-dialog v-model="showGradingDialog" title="编辑数据分级" width="400px">
      <el-form :model="gradingForm" label-width="80px">
        <el-form-item label="级别">
          <el-input :value="editingGrading?.level" disabled />
        </el-form-item>
        <el-form-item label="名称">
          <el-input v-model="gradingForm.name" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="gradingForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="颜色标签">
          <el-color-picker v-model="gradingForm.color" />
          <span class="color-hint">{{ gradingForm.color }}</span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showGradingDialog = false">取消</el-button>
        <el-button type="primary" @click="saveGradingLevel" :loading="saving">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { QuestionFilled } from '@element-plus/icons-vue'
import { classificationAPI, gradingLevelAPI } from '../api/standard'

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
      ElMessage.success('更新成功')
    } else {
      await classificationAPI.create(classificationForm.value)
      ElMessage.success('创建成功')
    }
    showClassificationDialog.value = false
    await loadClassifications()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  } finally {
    saving.value = false
  }
}

const deleteClassification = async (data) => {
  try {
    await ElMessageBox.confirm(`确认删除"${data.name}"？子级分类也将删除`, '提示', { type: 'warning' })
    await classificationAPI.delete(data.id)
    ElMessage.success('删除成功')
    await loadClassifications()
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.response?.data?.error || '删除失败')
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
    ElMessage.success('更新成功')
    showGradingDialog.value = false
    await loadGradingLevels()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '更新失败')
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
