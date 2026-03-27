<template>
  <div class="dim-hierarchy-detail">
    <div class="detail-header">
      <div class="header-left">
        <el-button text @click="$router.back()">
          <el-icon><ArrowLeft /></el-icon>返回
        </el-button>
        <span class="title">{{ hierarchy.name || '加载中...' }}</span>
        <el-tag type="info" size="small">{{ hierarchy.code }}</el-tag>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="handleSave" :loading="saving">保存基本信息</el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <!-- 基本信息 -->
      <el-col :span="24">
        <el-card shadow="never" class="info-card">
          <template #header><span class="card-title">基本信息</span></template>
          <el-form :model="form" label-width="90px">
            <el-row :gutter="16">
              <el-col :span="12">
                <el-form-item label="层级名称">
                  <el-input v-model="form.name" />
                </el-form-item>
              </el-col>
              <el-col :span="12">
                <el-form-item label="描述">
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
              <span class="card-title">层级定义（从粗到细排序）</span>
              <el-button type="primary" size="small" @click="openLevelDialog()">
                <el-icon><Plus /></el-icon>添加层级
              </el-button>
            </div>
          </template>

          <el-table :data="levels" v-loading="levelLoading" stripe>
            <el-table-column label="层次编号" prop="level_num" width="100">
              <template #default="{ row }">
                <el-tag size="small">L{{ row.level_num }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="层次名称" prop="name" min-width="120" />
            <el-table-column label="关联数据元" width="120">
              <template #default="{ row }">
                <span v-if="row.element_id" class="text-link">
                  Element#{{ row.element_id }}
                </span>
                <span v-else class="text-muted">—</span>
              </template>
            </el-table-column>
            <el-table-column label="描述" prop="description" min-width="160" show-overflow-tooltip />
            <el-table-column label="操作" width="120" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openLevelDialog(row)">编辑</el-button>
                <el-popconfirm title="确定删除该层级吗？" @confirm="handleDeleteLevel(row.id)">
                  <template #reference>
                    <el-button link type="danger">删除</el-button>
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
    <el-dialog v-model="levelVisible" :title="editingLevel ? '编辑层次' : '添加层次'" width="440px">
      <el-form ref="levelFormRef" :model="levelForm" :rules="levelRules" label-width="90px">
        <el-form-item label="层次编号" prop="level_num">
          <el-input-number v-model="levelForm.level_num" :min="1" :max="20" style="width:100%" />
          <div class="form-tip">数字越小粒度越粗（如：1=年，2=季，3=月，4=日）</div>
        </el-form-item>
        <el-form-item label="层次名称" prop="name">
          <el-input v-model="levelForm.name" placeholder="如：年、季度、月、日" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="levelForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="levelForm.sort_order" :min="0" style="width:100%" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="levelVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSaveLevel" :loading="levelSaving">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Plus } from '@element-plus/icons-vue'
import { dimensionHierarchyAPI } from '../api/standard'

const route = useRoute()
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
const levelRules = {
  level_num: [{ required: true, message: '请填写层次编号', trigger: 'blur' }],
  name: [{ required: true, message: '请填写层次名称', trigger: 'blur' }]
}

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
    ElMessage.error('加载失败')
  }
}

async function handleSave() {
  saving.value = true
  try {
    await dimensionHierarchyAPI.update(hierarchyId, { name: form.name, description: form.description, domain_id: form.domain_id })
    ElMessage.success('已保存')
    hierarchy.value.name = form.name
  } catch {
    ElMessage.error('保存失败')
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
    ElMessage.success('已保存')
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    levelSaving.value = false
  }
}

async function handleDeleteLevel(levelId) {
  try {
    await dimensionHierarchyAPI.deleteLevel(hierarchyId, levelId)
    levels.value = levels.value.filter(l => l.id !== levelId)
    ElMessage.success('已删除')
  } catch {
    ElMessage.error('删除失败')
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
