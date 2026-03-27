<template>
  <div class="unit-list">
    <div class="page-header">
      <h2>计量单位管理</h2>
    </div>

    <el-row :gutter="20">
      <!-- 左侧：度量类别 -->
      <el-col :span="8">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>度量类别</span>
              <el-button size="small" type="primary" @click="showAddCategory = true">新增</el-button>
            </div>
          </template>
          <el-tree
            :data="categoryTree"
            :props="{ label: 'name', children: [] }"
            node-key="id"
            highlight-current
            @node-click="handleCategoryClick"
          >
            <template #default="{ node, data }">
              <div class="tree-node">
                <span>{{ data.name }}</span>
                <span class="tree-node-meta">{{ data.code }}</span>
                <div class="tree-node-actions">
                  <el-button link size="small" @click.stop="editCategory(data)">编辑</el-button>
                  <el-button link size="small" type="danger" @click.stop="deleteCategory(data)" :disabled="data.is_system">删除</el-button>
                </div>
              </div>
            </template>
          </el-tree>
        </el-card>
      </el-col>

      <!-- 右侧：计量单位 -->
      <el-col :span="16">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ selectedCategory ? selectedCategory.name + ' - 计量单位' : '所有计量单位' }}</span>
              <el-button size="small" type="primary" @click="showAddUnit = true">新增单位</el-button>
            </div>
          </template>
          <el-table :data="units" v-loading="loadingUnits" size="small">
            <el-table-column label="名称" prop="name" width="120" />
            <el-table-column label="符号" prop="symbol" width="80" />
            <el-table-column label="所属类别" width="100">
              <template #default="{ row }">
                {{ row.category?.name || '-' }}
              </template>
            </el-table-column>
            <el-table-column label="描述" prop="description" show-overflow-tooltip />
            <el-table-column label="系统内置" width="80">
              <template #default="{ row }">
                <el-tag size="small" :type="row.is_system ? 'info' : 'success'">
                  {{ row.is_system ? '是' : '否' }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="120" fixed="right">
              <template #default="{ row }">
                <el-button link size="small" @click="editUnit(row)" :disabled="row.is_system">编辑</el-button>
                <el-button link size="small" type="danger" @click="deleteUnit(row)" :disabled="row.is_system">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <!-- 新增/编辑度量类别对话框 -->
    <el-dialog v-model="showAddCategory" :title="editingCategory ? '编辑度量类别' : '新增度量类别'" width="400px">
      <el-form :model="categoryForm" label-width="80px">
        <el-form-item label="名称" required>
          <el-input v-model="categoryForm.name" />
        </el-form-item>
        <el-form-item label="编码" required>
          <el-input v-model="categoryForm.code" :disabled="!!editingCategory" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="categoryForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="categoryForm.sort_order" :min="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAddCategory = false">取消</el-button>
        <el-button type="primary" @click="saveCategory" :loading="saving">保存</el-button>
      </template>
    </el-dialog>

    <!-- 新增/编辑计量单位对话框 -->
    <el-dialog v-model="showAddUnit" :title="editingUnit ? '编辑计量单位' : '新增计量单位'" width="400px">
      <el-form :model="unitForm" label-width="80px">
        <el-form-item label="度量类别" required>
          <el-select v-model="unitForm.category_id" style="width:100%">
            <el-option v-for="c in categories" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="名称" required>
          <el-input v-model="unitForm.name" />
        </el-form-item>
        <el-form-item label="符号">
          <el-input v-model="unitForm.symbol" placeholder="如：kg、m²、%" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="unitForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="unitForm.sort_order" :min="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAddUnit = false">取消</el-button>
        <el-button type="primary" @click="saveUnit" :loading="saving">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { measurementCategoryAPI, unitAPI } from '../api/standard'

const categories = ref([])
const units = ref([])
const loadingUnits = ref(false)
const saving = ref(false)
const selectedCategory = ref(null)

const showAddCategory = ref(false)
const showAddUnit = ref(false)
const editingCategory = ref(null)
const editingUnit = ref(null)

const categoryForm = ref({ name: '', code: '', description: '', sort_order: 0 })
const unitForm = ref({ category_id: null, name: '', symbol: '', description: '', sort_order: 0 })

const categoryTree = computed(() => categories.value)

const loadCategories = async () => {
  const res = await measurementCategoryAPI.list()
  categories.value = res || []
}

const loadUnits = async (categoryID) => {
  loadingUnits.value = true
  try {
    const params = categoryID ? { category_id: categoryID } : {}
    const res = await unitAPI.list(params)
    units.value = res || []
  } finally {
    loadingUnits.value = false
  }
}

const handleCategoryClick = (data) => {
  selectedCategory.value = data
  loadUnits(data.id)
}

const editCategory = (data) => {
  editingCategory.value = data
  categoryForm.value = { name: data.name, code: data.code, description: data.description, sort_order: data.sort_order }
  showAddCategory.value = true
}

const saveCategory = async () => {
  saving.value = true
  try {
    if (editingCategory.value) {
      await measurementCategoryAPI.update(editingCategory.value.id, categoryForm.value)
      ElMessage.success('更新成功')
    } else {
      await measurementCategoryAPI.create(categoryForm.value)
      ElMessage.success('创建成功')
    }
    showAddCategory.value = false
    editingCategory.value = null
    categoryForm.value = { name: '', code: '', description: '', sort_order: 0 }
    await loadCategories()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  } finally {
    saving.value = false
  }
}

const deleteCategory = async (data) => {
  try {
    await ElMessageBox.confirm(`确认删除度量类别"${data.name}"？`, '提示', { type: 'warning' })
    await measurementCategoryAPI.delete(data.id)
    ElMessage.success('删除成功')
    if (selectedCategory.value?.id === data.id) selectedCategory.value = null
    await loadCategories()
    await loadUnits(null)
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.response?.data?.error || '删除失败')
  }
}

const editUnit = (data) => {
  editingUnit.value = data
  unitForm.value = { category_id: data.category_id, name: data.name, symbol: data.symbol, description: data.description, sort_order: data.sort_order }
  showAddUnit.value = true
}

const saveUnit = async () => {
  saving.value = true
  try {
    if (editingUnit.value) {
      await unitAPI.update(editingUnit.value.id, unitForm.value)
      ElMessage.success('更新成功')
    } else {
      await unitAPI.create(unitForm.value)
      ElMessage.success('创建成功')
    }
    showAddUnit.value = false
    editingUnit.value = null
    unitForm.value = { category_id: selectedCategory.value?.id || null, name: '', symbol: '', description: '', sort_order: 0 }
    await loadUnits(selectedCategory.value?.id || null)
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '操作失败')
  } finally {
    saving.value = false
  }
}

const deleteUnit = async (data) => {
  try {
    await ElMessageBox.confirm(`确认删除计量单位"${data.name}"？`, '提示', { type: 'warning' })
    await unitAPI.delete(data.id)
    ElMessage.success('删除成功')
    await loadUnits(selectedCategory.value?.id || null)
  } catch (e) {
    if (e !== 'cancel') ElMessage.error(e.response?.data?.error || '删除失败')
  }
}

onMounted(() => {
  loadCategories()
  loadUnits(null)
})
</script>

<style scoped>
.unit-list { padding: 20px; }
.page-header { margin-bottom: 20px; }
.page-header h2 { margin: 0; font-size: 18px; color: var(--el-text-color-primary); }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.tree-node { display: flex; align-items: center; gap: 8px; width: 100%; }
.tree-node-meta { font-size: 12px; color: var(--el-text-color-secondary); }
.tree-node-actions { margin-left: auto; }
</style>
