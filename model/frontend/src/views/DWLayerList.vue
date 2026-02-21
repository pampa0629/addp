<template>
  <div class="dw-layer-list">
    <div class="page-header">
      <div class="header-left">
        <h3 class="page-title">数仓分层配置</h3>
        <el-tag type="info" size="small">{{ layers.length }} 个分层</el-tag>
      </div>
      <div class="header-actions">
        <el-button @click="handleInitDefault" :loading="initializing">
          初始化默认分层
        </el-button>
        <el-button type="primary" @click="openDialog()">
          <el-icon><Plus /></el-icon>
          新建分层
        </el-button>
      </div>
    </div>

    <el-card shadow="never">
      <el-table :data="layers" v-loading="loading" stripe>
        <el-table-column label="层级代码" prop="layer_code" width="120">
          <template #default="{ row }">
            <el-tag :type="layerTagType(row.layer_code)" size="small">
              {{ row.layer_code.toUpperCase() }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="层级名称" prop="layer_name" width="140" />
        <el-table-column label="命名规范" prop="naming_rule" show-overflow-tooltip />
        <el-table-column label="描述" prop="description" show-overflow-tooltip />
        <el-table-column label="排序" prop="sort_order" width="80" />
        <el-table-column label="操作" width="150" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openDialog(row)">编辑</el-button>
            <el-popconfirm
              title="确定删除该分层吗？"
              @confirm="handleDelete(row.id)"
            >
              <template #reference>
                <el-button link type="danger">删除</el-button>
              </template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- 新建/编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="editingLayer ? '编辑数仓分层' : '新建数仓分层'"
      width="520px"
    >
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item label="层级代码" prop="layer_code">
          <el-input
            v-model="form.layer_code"
            :disabled="!!editingLayer"
            placeholder="如：ods / dwd / dws / ads"
          />
        </el-form-item>
        <el-form-item label="层级名称" prop="layer_name">
          <el-input v-model="form.layer_name" placeholder="如：贴源层 / 明细层" />
        </el-form-item>
        <el-form-item label="命名规范">
          <el-input
            v-model="form.naming_rule"
            placeholder="如：dwd_{domain}_{entity}_d"
          />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="排序">
          <el-input-number v-model="form.sort_order" :min="0" :max="999" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">
          {{ editingLayer ? '保存' : '创建' }}
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

const loading = ref(false)
const submitting = ref(false)
const initializing = ref(false)
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
  layer_code: [{ required: true, message: '请输入层级代码', trigger: 'blur' }],
  layer_name: [{ required: true, message: '请输入层级名称', trigger: 'blur' }]
}

const layerTagType = (code) => {
  const types = { ods: '', dwd: 'success', dws: 'warning', ads: 'danger' }
  return types[code?.toLowerCase()] ?? 'info'
}

const loadLayers = async () => {
  loading.value = true
  try {
    const res = await dwLayerAPI.list()
    layers.value = res.data || []
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
  await formRef.value.validate()
  submitting.value = true
  try {
    if (editingLayer.value) {
      await dwLayerAPI.update(editingLayer.value.id, form.value)
      ElMessage.success('更新成功')
    } else {
      await dwLayerAPI.create(form.value)
      ElMessage.success('创建成功')
    }
    dialogVisible.value = false
    loadLayers()
  } catch (err) {
    ElMessage.error(err.response?.data?.error || '操作失败')
  } finally {
    submitting.value = false
  }
}

const handleDelete = async (id) => {
  try {
    await dwLayerAPI.delete(id)
    ElMessage.success('删除成功')
    loadLayers()
  } catch {
    ElMessage.error('删除失败')
  }
}

const handleInitDefault = async () => {
  const defaults = [
    { layer_code: 'ods', layer_name: '贴源层', naming_rule: 'ods_{domain}_{entity}', description: '原始数据，不做任何加工，保留业务系统原貌', sort_order: 1 },
    { layer_code: 'dwd', layer_name: '明细层', naming_rule: 'dwd_{domain}_{entity}_d', description: '清洗、标准化、建立维度关联的明细数据', sort_order: 2 },
    { layer_code: 'dws', layer_name: '汇总层', naming_rule: 'dws_{domain}_{subject}_{period}', description: '面向主题域的轻度聚合数据', sort_order: 3 },
    { layer_code: 'ads', layer_name: '应用层', naming_rule: 'ads_{subject}_{scene}', description: '面向应用的数据集市，直接支撑报表和 API', sort_order: 4 }
  ]

  initializing.value = true
  try {
    for (const d of defaults) {
      await dwLayerAPI.create(d).catch(() => {})
    }
    ElMessage.success('默认分层初始化完成')
    loadLayers()
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
</style>
