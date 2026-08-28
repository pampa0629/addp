<template>
  <div class="dw-layer-list">
    <div class="page-header">
      <div class="header-left">
        <h3 class="page-title">{{ t('model.dw_layer.title') }}</h3>
        <el-tag type="info" size="small">{{ layers.length }} {{ t('model.dw_layer.layer_code') }}</el-tag>
      </div>
      <div class="header-actions">
        <el-button v-if="can('model.dw_layer.create')" @click="handleInitDefault" :loading="initializing">
          {{ t('model.dw_layer.init_default') }}
        </el-button>
        <el-button v-if="can('model.dw_layer.create')" type="primary" @click="openDialog()">
          <el-icon><Plus /></el-icon>
          {{ t('model.dw_layer.new') }}
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="loadError"
      class="load-error"
      type="error"
      :title="loadError"
      show-icon
      :closable="false"
    >
      <el-button link type="danger" @click="loadLayers">{{ t('model.common.retry') }}</el-button>
    </el-alert>

    <el-card v-else shadow="never">
      <el-table :data="layers" v-loading="loading" stripe>
        <el-table-column :label="t('model.dw_layer.layer_code')" prop="layer_code" width="120">
          <template #default="{ row }">
            <el-tag :type="layerTagType(row.layer_code)" size="small">
              {{ row.layer_code.toUpperCase() }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('model.dw_layer.layer_name')" prop="layer_name" width="140" />
        <el-table-column :label="t('model.dw_layer.naming_rule')" prop="naming_rule" show-overflow-tooltip />
        <el-table-column :label="t('model.dw_layer.description')" prop="description" show-overflow-tooltip />
        <el-table-column :label="t('model.dw_layer.sort_order')" prop="sort_order" width="80" />
        <el-table-column :label="t('model.dw_layer.actions')" width="150" fixed="right">
          <template #default="{ row }">
            <el-button v-if="can('model.dw_layer.update')" link type="primary" @click="openDialog(row)">{{ t('model.common.edit') }}</el-button>
            <el-popconfirm v-if="can('model.dw_layer.delete')"
              :title="t('model.dw_layer.delete_confirm')"
              @confirm="handleDelete(row)"
            >
              <template #reference>
                <el-button link type="danger">{{ t('model.common.delete') }}</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 新建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      class="addp-dialog"
      :title="editingLayer ? t('model.dw_layer.edit') : t('model.dw_layer.new')"
      width="min(520px, calc(100vw - 32px))"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item :label="t('model.dw_layer.layer_code')" prop="layer_code">
          <el-input
            v-model="form.layer_code"
            maxlength="20"
            :disabled="!!editingLayer"
            :placeholder="t('model.dw_layer.code_placeholder')"
          />
        </el-form-item>
        <el-form-item :label="t('model.dw_layer.layer_name')" prop="layer_name">
          <el-input v-model="form.layer_name" maxlength="100" :placeholder="t('model.dw_layer.name_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.dw_layer.naming_rule')">
          <el-input
            v-model="form.naming_rule"
            :placeholder="t('model.dw_layer.naming_placeholder')"
          />
        </el-form-item>
        <el-form-item :label="t('model.dw_layer.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('model.dw_layer.sort_order')">
          <el-input-number v-model="form.sort_order" :min="0" :max="999" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">
          {{ editingLayer ? t('model.common.save') : t('model.common.create') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { dwLayerAPI } from '../api/model'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { getModelErrorMessage } from '../utils/apiError'
import { createDefaultDWLayers } from '../utils/dwLayerDefaults'
import { buildDWLayerUpdateRequest } from '../utils/modelDetailState'

const { t } = useI18n()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)

const loading = ref(false)
const submitting = ref(false)
const initializing = ref(false)
const loadError = ref('')
const layers = ref([])
const dialogVisible = ref(false)
const editingLayer = ref(null)
const formRef = ref(null)

const defaultForm = () => ({
  layer_code: '',
  layer_name: '',
  naming_rule: '',
  description: '',
  sort_order: 0
})

const form = ref(defaultForm())

const rules = {
  layer_code: [{ required: true, message: t('model.dw_layer.code_required'), trigger: 'blur' }],
  layer_name: [{ required: true, message: t('model.dw_layer.name_required'), trigger: 'blur' }]
}

const layerTagType = (code) => {
  const types = { ods: '', dwd: 'success', dws: 'warning', ads: 'danger' }
  return types[code?.toLowerCase()] ?? 'info'
}

const loadLayers = async () => {
  loading.value = true
  loadError.value = ''
  if (!can('model.dw_layer.read')) {
    layers.value = []
    loadError.value = t('model.common.permission_denied')
    loading.value = false
    return
  }
  try {
    const res = await dwLayerAPI.list()
    layers.value = res || []
  } catch (err) {
    layers.value = []
    loadError.value = getModelErrorMessage(err, t, 'model.common.load_failed')
  } finally {
    loading.value = false
  }
}

const openDialog = (layer = null) => {
  editingLayer.value = layer
  if (layer) {
    form.value = {
      layer_code: layer.layer_code,
      layer_name: layer.layer_name,
      naming_rule: layer.naming_rule || '',
      description: layer.description || '',
      sort_order: layer.sort_order || 0
    }
  } else {
    form.value = defaultForm()
  }
  dialogVisible.value = true
}

const handleSubmit = async () => {
  const requiredPermission = editingLayer.value ? 'model.dw_layer.update' : 'model.dw_layer.create'
  if (!can(requiredPermission)) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    await formRef.value.validate()
  } catch {
    return
  }
  submitting.value = true
  try {
    if (editingLayer.value) {
      const updated = await dwLayerAPI.update(editingLayer.value.id, buildDWLayerUpdateRequest(form.value, editingLayer.value))
      editingLayer.value = updated
      ElMessage.success(t('model.common.update_success'))
    } else {
      await dwLayerAPI.create(form.value)
      ElMessage.success(t('model.common.create_success'))
    }
    dialogVisible.value = false
    loadLayers()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed'))
  } finally {
    submitting.value = false
  }
}

const handleDelete = async (layer) => {
  if (!can('model.dw_layer.delete')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    await dwLayerAPI.delete(layer.id, layer.version)
    ElMessage.success(t('model.common.delete_success'))
    loadLayers()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.delete_failed'))
  }
}

const handleInitDefault = async () => {
  if (!can('model.dw_layer.create')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  const defaults = createDefaultDWLayers(t)

  initializing.value = true
  try {
    for (const d of defaults) {
      try {
        await dwLayerAPI.create(d)
      } catch (err) {
        const errorCode = err?.response?.data?.error_code
        if (errorCode === 'dw_layer_code_conflict') continue
        throw err
      }
    }
    ElMessage.success(t('model.dw_layer.init_success'))
    loadLayers()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.dw_layer.init_failed'))
  } finally {
    initializing.value = false
  }
}

onMounted(loadLayers)
</script>

<style scoped>
.dw-layer-list {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.page-title {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.header-actions {
  display: flex;
  gap: 8px;
}

.load-error {
  margin-bottom: 16px;
}
</style>
