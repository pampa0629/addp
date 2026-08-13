<template>
  <div class="unit-list">
    <div class="page-header">
      <h2>{{ $t('standard.unit.title') }}</h2>
    </div>

    <el-row :gutter="20">
      <!-- 左侧：度量类别 -->
      <el-col :span="8">
        <el-card>
          <template #header>
            <div class="card-header">
              <span>{{ $t('standard.unit.category') }}</span>
              <el-button v-if="canCreate" size="small" type="primary" @click="showAddCategory = true">{{ $t('standard.unit.addCategory') }}</el-button>
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
                <span class="tree-node-name">{{ data.name }}</span>
                <span class="tree-node-meta">{{ data.code }}</span>
                <div class="tree-node-actions">
                  <el-button v-if="canUpdate" link size="small" @click.stop="editCategory(data)">{{ $t('standard.common.edit') }}</el-button>
                  <el-button v-if="canDelete" link size="small" type="danger" :loading="isActionLocked(`unit-category:${data.id}`)" @click.stop="deleteCategory(data)" :disabled="data.is_system">{{ $t('standard.common.delete') }}</el-button>
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
              <span>{{ selectedCategory ? $t('standard.unit.unitsOfCategory', { name: selectedCategory.name }) : $t('standard.unit.allUnits') }}</span>
              <el-button v-if="canCreate" size="small" type="primary" @click="openAddUnit">{{ $t('standard.unit.addUnit') }}</el-button>
            </div>
          </template>
          <el-table :data="units" v-loading="loadingUnits" size="small">
            <el-table-column :label="$t('standard.common.name')" prop="name" width="120" />
            <el-table-column :label="$t('standard.unit.symbolLabel')" prop="symbol" width="80" />
            <el-table-column :label="$t('standard.unit.belongsTo')" width="100">
              <template #default="{ row }">
                {{ row.category?.name || '-' }}
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.common.description')" prop="description" show-overflow-tooltip />
            <el-table-column :label="$t('standard.unit.systemBuiltin')" width="80">
              <template #default="{ row }">
                <el-tag size="small" :type="row.is_system ? 'info' : 'success'">
                  {{ row.is_system ? $t('standard.unit.yes') : $t('standard.unit.no') }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.common.actions')" width="150" fixed="right">
              <template #default="{ row }">
                <div class="table-actions">
                <el-button v-if="canUpdate" link size="small" @click="editUnit(row)" :disabled="row.is_system">{{ $t('standard.common.edit') }}</el-button>
                <el-button v-if="canDelete" link size="small" type="danger" :loading="isActionLocked(`unit:${row.id}`)" @click="deleteUnit(row)" :disabled="row.is_system">{{ $t('standard.common.delete') }}</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>
    </el-row>

    <!-- 新增/编辑度量类别对话框 -->
    <el-dialog v-model="showAddCategory" :title="editingCategory ? $t('standard.unit.editCategory') : $t('standard.unit.createCategory')" width="400px">
      <el-form :model="categoryForm" label-width="80px">
        <el-form-item :label="$t('standard.common.name')" required>
          <el-input v-model="categoryForm.name" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.code')" required>
          <el-input v-model="categoryForm.code" :disabled="!!editingCategory" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.description')">
          <el-input v-model="categoryForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.sort')">
          <el-input-number v-model="categoryForm.sort_order" :min="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAddCategory = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="saveCategory" :loading="saving">{{ $t('standard.common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 新增/编辑计量单位对话框 -->
    <el-dialog v-model="showAddUnit" :title="editingUnit ? $t('standard.unit.editUnit') : $t('standard.unit.createUnit')" width="400px">
      <el-form :model="unitForm" label-width="80px">
        <el-form-item :label="$t('standard.unit.categoryLabel')" required>
          <el-select v-model="unitForm.category_id" style="width:100%">
            <el-option v-for="c in categories" :key="c.id" :label="c.name" :value="c.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="$t('standard.common.name')" required>
          <el-input v-model="unitForm.name" />
        </el-form-item>
        <el-form-item :label="$t('standard.unit.symbolLabel')">
          <el-input v-model="unitForm.symbol" :placeholder="$t('standard.unit.symbolPlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.description')">
          <el-input v-model="unitForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.sort')">
          <el-input-number v-model="unitForm.sort_order" :min="0" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showAddUnit = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="saveUnit" :loading="saving">{{ $t('standard.common.save') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { measurementCategoryAPI, unitAPI } from '../api/standard'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { createLatestRequestCoordinator } from '@common-ui'
import { useActionLock } from '../composables/useActionLock'

const { t } = useI18n()
const { canCreate, canUpdate, canDelete } = useStandardPermissions('unit')
const { isLocked: isActionLocked, runLocked } = useActionLock()
const route = useRoute()
const router = useRouter()
const categories = ref([])
const units = ref([])
const loadingUnits = ref(false)
const saving = ref(false)
const selectedCategory = ref(null)
const unitRequests = createLatestRequestCoordinator()

const showAddCategory = ref(false)
const showAddUnit = ref(false)
const editingCategory = ref(null)
const editingUnit = ref(null)

const categoryForm = ref({ name: '', code: '', description: '', sort_order: 0 })
const unitForm = ref({ category_id: null, name: '', symbol: '', description: '', sort_order: 0 })

const categoryTree = computed(() => categories.value)

const loadCategories = async () => {
  try {
    const res = await measurementCategoryAPI.list()
    categories.value = res || []
  } catch (e) {
    categories.value = []
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.loadFailed'))
  }
}

const loadUnits = async (categoryID) => {
  const params = categoryID ? { category_id: categoryID } : {}
  const request = unitRequests.begin(JSON.stringify(params))
  loadingUnits.value = true
  try {
    const res = await unitAPI.list(params)
    if (!unitRequests.isCurrent(request, JSON.stringify(params))) return
    units.value = res || []
  } catch (e) {
    if (!unitRequests.isCurrent(request, JSON.stringify(params))) return
    units.value = []
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.loadFailed'))
  } finally {
    if (unitRequests.isCurrent(request, JSON.stringify(params))) loadingUnits.value = false
  }
}

const handleCategoryClick = (data) => {
  selectedCategory.value = data
  navigateStandardRoute(router, {
    path: '/units',
    query: { category_id: String(data.id) }
  }, { history: 'replace' })
  loadUnits(data.id)
}

const editCategory = (data) => {
  editingCategory.value = data
  categoryForm.value = { name: data.name, code: data.code, description: data.description, sort_order: data.sort_order, version: data.version }
  showAddCategory.value = true
}

const saveCategory = async () => {
  if (saving.value) return
  saving.value = true
  try {
    if (editingCategory.value) {
      await measurementCategoryAPI.update(editingCategory.value.id, categoryForm.value)
      ElMessage.success(t('standard.common.updateSuccess'))
    } else {
      await measurementCategoryAPI.create(categoryForm.value)
      ElMessage.success(t('standard.common.createSuccess'))
    }
    showAddCategory.value = false
    editingCategory.value = null
    categoryForm.value = { name: '', code: '', description: '', sort_order: 0 }
    await loadCategories()
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t))
  } finally {
    saving.value = false
  }
}

const deleteCategory = async (data) => {
  await runLocked(`unit-category:${data.id}`, async () => {
    try {
      await ElMessageBox.confirm(t('standard.unit.confirmDeleteCategory', { name: data.name }), t('standard.common.hint'), { type: 'warning' })
      await measurementCategoryAPI.delete(data.id)
      ElMessage.success(t('standard.common.deleteSuccess'))
      if (selectedCategory.value?.id === data.id) {
        selectedCategory.value = null
        navigateStandardRoute(router, '/units', { history: 'replace' })
      }
      await loadCategories()
      await loadUnits(null)
    } catch (e) {
      if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.deleteFailed'))
    }
  })
}

const editUnit = (data) => {
  editingUnit.value = data
  unitForm.value = { category_id: data.category_id, name: data.name, symbol: data.symbol, description: data.description, sort_order: data.sort_order, version: data.version }
  showAddUnit.value = true
}

const openAddUnit = () => {
  editingUnit.value = null
  unitForm.value = { category_id: selectedCategory.value?.id || null, name: '', symbol: '', description: '', sort_order: 0 }
  showAddUnit.value = true
}

const saveUnit = async () => {
  if (saving.value) return
  saving.value = true
  try {
    if (editingUnit.value) {
      await unitAPI.update(editingUnit.value.id, unitForm.value)
      ElMessage.success(t('standard.common.updateSuccess'))
    } else {
      await unitAPI.create(unitForm.value)
      ElMessage.success(t('standard.common.createSuccess'))
    }
    showAddUnit.value = false
    editingUnit.value = null
    unitForm.value = { category_id: selectedCategory.value?.id || null, name: '', symbol: '', description: '', sort_order: 0 }
    await loadUnits(selectedCategory.value?.id || null)
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t))
  } finally {
    saving.value = false
  }
}

const deleteUnit = async (data) => {
  await runLocked(`unit:${data.id}`, async () => {
    try {
      await ElMessageBox.confirm(t('standard.unit.confirmDeleteUnit', { name: data.name }), t('standard.common.hint'), { type: 'warning' })
      await unitAPI.delete(data.id)
      ElMessage.success(t('standard.common.deleteSuccess'))
      await loadUnits(selectedCategory.value?.id || null)
    } catch (e) {
      if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.deleteFailed'))
    }
  })
}

onMounted(async () => {
  await loadCategories()
  const categoryID = route.query.category_id ? Number(route.query.category_id) : null
  selectedCategory.value = categories.value.find(category => category.id === categoryID) || null
  loadUnits(selectedCategory.value?.id || null)
})
</script>

<style scoped>
.unit-list { min-height: 100%; padding: 20px; color: var(--addp-text-primary); background: var(--addp-bg-secondary); }
.page-header { margin-bottom: 20px; }
.page-header h2 { margin: 0; font-size: 18px; color: var(--addp-text-primary); }
.card-header { display: flex; justify-content: space-between; align-items: center; }
.unit-list :deep(.el-card) { background: var(--addp-bg-primary); border-color: var(--addp-border-color); box-shadow: var(--addp-shadow-card); }
.tree-node { display: flex; align-items: center; gap: 8px; min-width: 0; width: 100%; }
.tree-node-name, .tree-node-meta { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tree-node-name { flex: 1 1 auto; }
.tree-node-meta { flex: 0 1 auto; font-size: 12px; color: var(--addp-text-secondary); }
.tree-node-actions { display: inline-flex; align-items: center; gap: 4px; margin-left: auto; min-width: max-content; white-space: nowrap; }
.tree-node-actions :deep(.el-button) { white-space: nowrap; }
.table-actions { display: inline-flex; align-items: center; gap: 8px; min-width: max-content; white-space: nowrap; }
.table-actions :deep(.el-button) { white-space: nowrap; }

@media (max-width: 768px) {
  .unit-list { padding: 12px; }
  .unit-list :deep(.el-row) { margin-left: 0 !important; margin-right: 0 !important; }
  .unit-list :deep(.el-col) { max-width: 100%; flex: 0 0 100%; }
  .unit-list :deep(.el-col + .el-col) { margin-top: 12px; }
}
</style>
