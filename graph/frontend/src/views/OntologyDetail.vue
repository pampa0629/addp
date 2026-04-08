<template>
  <div class="page-container">
    <div class="page-header">
      <el-button link @click="$router.push('/ontologies')">
        <el-icon><ArrowLeft /></el-icon> 返回
      </el-button>
      <h2>{{ ontology?.name }}</h2>
      <el-tag :type="ontology?.status === 'active' ? 'success' : 'info'" size="small">
        {{ ontology?.status === 'active' ? '启用' : '归档' }}
      </el-tag>
      <el-button size="small" @click="$router.push(`/ontologies/${$route.params.id}/edit`)">编辑</el-button>
      <el-button size="small" type="success" @click="showVersionDialog = true">创建版本快照</el-button>
      <el-button size="small" type="warning" @click="openImportFromModel">从 Model 导入</el-button>
      <el-button size="small" type="info" @click="openInferFromEngine">从 Neo4j 推导</el-button>
    </div>

    <el-tabs v-model="activeTab">
      <!-- 实体类型 -->
      <el-tab-pane label="实体类型" name="entities">
        <div class="tab-toolbar">
          <el-button type="primary" size="small" @click="showEntityForm(null)">
            <el-icon><Plus /></el-icon> 添加实体类型
          </el-button>
        </div>
        <el-table :data="entityTypes" border size="small">
          <el-table-column prop="name" label="标识符" width="150" />
          <el-table-column prop="label" label="显示名" width="150" />
          <el-table-column prop="description" label="描述" show-overflow-tooltip />
          <el-table-column label="空间图层" width="120">
            <template #default="{ row }">
              <template v-if="row.is_spatial_layer">
                <el-tag type="success" size="small">
                  空间·{{ row.spatial_layer_config?.geometry_type === 'wkt' ? '线面' : '点' }}
                </el-tag>
              </template>
              <template v-else-if="getSpatialAncestor(row)">
                <el-tooltip :content="`继承 ${getSpatialAncestor(row)} 空间图层`" placement="top">
                  <el-icon style="color:var(--el-color-success);cursor:default"><Location /></el-icon>
                </el-tooltip>
              </template>
            </template>
          </el-table-column>
          <el-table-column label="颜色" width="80">
            <template #default="{ row }">
              <span class="color-dot" :style="{ background: row.color }"></span>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120">
            <template #default="{ row }">
              <el-button link size="small" @click="showEntityForm(row)">编辑</el-button>
              <el-button link type="danger" size="small" @click="deleteEntityType(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 关系类型 -->
      <el-tab-pane label="关系类型" name="relations">
        <div class="tab-toolbar">
          <el-button type="primary" size="small" @click="showRelationForm(null)">
            <el-icon><Plus /></el-icon> 添加关系类型
          </el-button>
        </div>
        <el-table :data="relationTypes" border size="small">
          <el-table-column prop="name" label="标识符" width="150" />
          <el-table-column prop="label" label="显示名" width="150" />
          <el-table-column label="来源 → 目标" width="200">
            <template #default="{ row }">
              {{ row.source_type?.label || row.source_type?.name || '任意' }}
              → {{ row.target_type?.label || row.target_type?.name || '任意' }}
            </template>
          </el-table-column>
          <el-table-column prop="directed" label="有向" width="80">
            <template #default="{ row }">
              <el-tag size="small" :type="row.directed ? 'primary' : 'info'">
                {{ row.directed ? '有向' : '无向' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="120">
            <template #default="{ row }">
              <el-button link size="small" @click="showRelationForm(row)">编辑</el-button>
              <el-button link type="danger" size="small" @click="deleteRelationType(row)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 版本历史 -->
      <el-tab-pane label="版本历史" name="versions">
        <el-table :data="versions" border size="small">
          <el-table-column prop="version" label="版本号" width="120" />
          <el-table-column prop="description" label="描述" show-overflow-tooltip />
          <el-table-column prop="created_at" label="创建时间" width="180">
            <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 图形视图 -->
      <el-tab-pane label="图形视图" name="graph">
        <div class="graph-tab-container">
          <OntologyView :entity-types="entityTypes" :relation-types="relationTypes" />
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 实体类型表单对话框 -->
    <el-dialog v-model="entityDialogVisible" :title="editingEntity ? '编辑实体类型' : '添加实体类型'" width="750px">
      <el-form ref="entityFormRef" :model="entityForm" :rules="entityRules" label-width="80px">
        <el-form-item label="标识符" prop="name">
          <el-input v-model="entityForm.name" placeholder="英文，如 Person" />
        </el-form-item>
        <el-form-item label="显示名" prop="label">
          <el-input v-model="entityForm.label" placeholder="如 人物" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="entityForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="父类型">
          <el-select v-model="entityForm.parent_id" placeholder="无（顶级类型）" clearable style="width:100%">
            <el-option
              v-for="et in entityTypes.filter(e => e.id !== editingEntity?.id)"
              :key="et.id"
              :label="`${et.label || et.name}（${et.name}）`"
              :value="et.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="颜色">
          <el-color-picker v-model="entityForm.color" />
        </el-form-item>
        <el-form-item label="空间图层">
          <el-switch v-model="entityForm.is_spatial_layer" @change="onSpatialLayerToggle" />
          <span v-if="entityForm.is_spatial_layer" style="margin-left:12px;font-size:12px;color:var(--el-text-color-secondary)">
            请继续选择几何类型
          </span>
        </el-form-item>
        <template v-if="entityForm.is_spatial_layer">
          <el-form-item label="几何类型">
            <el-radio-group v-model="entityForm.spatial_layer_config.geometry_type" @change="onGeometryTypeChange">
              <el-radio value="point">点（lon + lat）</el-radio>
              <el-radio value="wkt">线/面（WKT）</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="entityForm.spatial_layer_config.geometry_type" label="图层配置">
            <div style="display:flex;flex-direction:column;gap:8px;width:100%">
              <div style="display:flex;align-items:center;gap:8px">
                <span style="width:80px;flex-shrink:0;font-size:13px">图层名称</span>
                <el-input v-model="entityForm.spatial_layer_config.layer_name" size="small" style="flex:1" />
              </div>
              <template v-if="entityForm.spatial_layer_config.geometry_type === 'point'">
                <div style="display:flex;align-items:center;gap:8px">
                  <span style="width:80px;flex-shrink:0;font-size:13px">经度字段</span>
                  <el-input v-model="entityForm.spatial_layer_config.lon_field" size="small" style="flex:1" placeholder="默认: lon" />
                </div>
                <div style="display:flex;align-items:center;gap:8px">
                  <span style="width:80px;flex-shrink:0;font-size:13px">纬度字段</span>
                  <el-input v-model="entityForm.spatial_layer_config.lat_field" size="small" style="flex:1" placeholder="默认: lat" />
                </div>
              </template>
              <template v-if="entityForm.spatial_layer_config.geometry_type === 'wkt'">
                <div style="display:flex;align-items:center;gap:8px">
                  <span style="width:80px;flex-shrink:0;font-size:13px">几何字段</span>
                  <el-input v-model="entityForm.spatial_layer_config.geom_field" size="small" style="flex:1" placeholder="默认: wkt" />
                </div>
              </template>
            </div>
          </el-form-item>
        </template>
        <el-form-item label="属性定义">
          <div class="prop-table">
            <el-table :data="entityForm.properties" border size="small" style="width:100%">
              <el-table-column label="字段名(name)" min-width="110">
                <template #default="{ row }">
                  <el-input v-model="row.name" size="small" placeholder="英文字段名" />
                </template>
              </el-table-column>
              <el-table-column label="显示名" min-width="90">
                <template #default="{ row }">
                  <el-input v-model="row.label" size="small" placeholder="中文名" />
                </template>
              </el-table-column>
              <el-table-column label="数据类型" min-width="110">
                <template #default="{ row }">
                  <el-select v-model="row.data_type" size="small">
                    <el-option v-for="t in dataTypes" :key="t.value" :label="t.label" :value="t.value" />
                  </el-select>
                </template>
              </el-table-column>
              <el-table-column label="必填" width="60" align="center">
                <template #default="{ row }">
                  <el-checkbox v-model="row.required" />
                </template>
              </el-table-column>
              <el-table-column label="唯一" width="60" align="center">
                <template #default="{ row }">
                  <el-checkbox v-model="row.unique" />
                </template>
              </el-table-column>
              <el-table-column label="操作" width="60" align="center">
                <template #default="{ $index }">
                  <el-button link type="danger" size="small" @click="entityForm.properties.splice($index, 1)">删</el-button>
                </template>
              </el-table-column>
            </el-table>
            <el-button size="small" style="margin-top:8px" @click="addEntityProp">+ 添加属性</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="entityDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitEntityType">保存</el-button>
      </template>
    </el-dialog>

    <!-- 关系类型表单对话框 -->
    <el-dialog v-model="relationDialogVisible" :title="editingRelation ? '编辑关系类型' : '添加关系类型'" width="750px">
      <el-form ref="relationFormRef" :model="relationForm" :rules="relationRules" label-width="80px">
        <el-form-item label="标识符" prop="name">
          <el-input v-model="relationForm.name" placeholder="英文，如 KNOWS" />
        </el-form-item>
        <el-form-item label="显示名" prop="label">
          <el-input v-model="relationForm.label" placeholder="如 认识" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="relationForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="来源类型">
          <el-select v-model="relationForm.source_type_id" placeholder="任意" clearable>
            <el-option v-for="et in entityTypes" :key="et.id" :label="et.label || et.name" :value="et.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="目标类型">
          <el-select v-model="relationForm.target_type_id" placeholder="任意" clearable>
            <el-option v-for="et in entityTypes" :key="et.id" :label="et.label || et.name" :value="et.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="有向">
          <el-switch v-model="relationForm.directed" />
        </el-form-item>
        <el-form-item label="属性定义">
          <div class="prop-table">
            <el-table :data="relationForm.properties" border size="small" style="width:100%">
              <el-table-column label="字段名(name)" min-width="110">
                <template #default="{ row }">
                  <el-input v-model="row.name" size="small" placeholder="英文字段名" />
                </template>
              </el-table-column>
              <el-table-column label="显示名" min-width="90">
                <template #default="{ row }">
                  <el-input v-model="row.label" size="small" placeholder="中文名" />
                </template>
              </el-table-column>
              <el-table-column label="数据类型" min-width="110">
                <template #default="{ row }">
                  <el-select v-model="row.data_type" size="small">
                    <el-option v-for="t in dataTypes" :key="t.value" :label="t.label" :value="t.value" />
                  </el-select>
                </template>
              </el-table-column>
              <el-table-column label="必填" width="60" align="center">
                <template #default="{ row }">
                  <el-checkbox v-model="row.required" />
                </template>
              </el-table-column>
              <el-table-column label="唯一" width="60" align="center">
                <template #default="{ row }">
                  <el-checkbox v-model="row.unique" />
                </template>
              </el-table-column>
              <el-table-column label="操作" width="60" align="center">
                <template #default="{ $index }">
                  <el-button link type="danger" size="small" @click="relationForm.properties.splice($index, 1)">删</el-button>
                </template>
              </el-table-column>
            </el-table>
            <el-button size="small" style="margin-top:8px" @click="addRelationProp">+ 添加属性</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="relationDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitRelationType">保存</el-button>
      </template>
    </el-dialog>

    <!-- 版本快照对话框 -->
    <el-dialog v-model="showVersionDialog" title="创建版本快照" width="400px">
      <el-form ref="versionFormRef" :model="versionForm" :rules="versionRules" label-width="80px">
        <el-form-item label="版本号" prop="version">
          <el-input v-model="versionForm.version" placeholder="如 1.0.0" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="versionForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showVersionDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="submitVersion">保存</el-button>
      </template>
    </el-dialog>

    <!-- F4: 从 Model 导入对话框 -->
    <ImportFromModelDialog
      ref="importFromModelRef"
      :ontology-id="Number(ontologyId)"
      @imported="handleImported"
    />

    <!-- F5b: 从 Neo4j 引擎推导对话框 -->
    <InferFromEngineDialog
      ref="inferFromEngineRef"
      :ontology-id="Number(ontologyId)"
      @applied="handleImported"
    />
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Plus, Location } from '@element-plus/icons-vue'
import { ontologyAPI } from '../api/ontology'
import { OntologyView } from '@addp/common-frontend/graph'
import ImportFromModelDialog from '../components/ImportFromModelDialog.vue'
import InferFromEngineDialog from '../components/InferFromEngineDialog.vue'

const route = useRoute()
const ontologyId = route.params.id
const ontology = ref(null)
const entityTypes = ref([])
const relationTypes = ref([])
const versions = ref([])
const activeTab = ref('entities')
const saving = ref(false)

// entity form
const entityDialogVisible = ref(false)
const editingEntity = ref(null)
const entityFormRef = ref(null)
const entityForm = ref({
  name: '', label: '', description: '', color: '#5B8FF9', properties: [],
  is_spatial_layer: false,
  spatial_layer_config: { geometry_type: '', layer_name: '', lon_field: 'lon', lat_field: 'lat', geom_field: 'wkt' }
})
const entityRules = { name: [{ required: true, message: '请输入标识符', trigger: 'blur' }] }

// relation form
const relationDialogVisible = ref(false)
const editingRelation = ref(null)
const relationFormRef = ref(null)
const relationForm = ref({ name: '', label: '', description: '', source_type_id: null, target_type_id: null, directed: true, properties: [] })
const relationRules = { name: [{ required: true, message: '请输入标识符', trigger: 'blur' }] }

const dataTypes = [
  { value: 'string', label: 'string' },
  { value: 'integer', label: 'integer' },
  { value: 'float', label: 'float' },
  { value: 'boolean', label: 'boolean' },
  { value: 'date', label: 'date' },
  { value: 'datetime', label: 'datetime' },
  { value: 'geometry', label: 'geometry (WKT WGS-84)' },
]

const addEntityProp = () => {
  entityForm.value.properties.push({ name: '', label: '', data_type: 'string', required: false, unique: false })
}

const addRelationProp = () => {
  relationForm.value.properties.push({ name: '', label: '', data_type: 'string', required: false, unique: false })
}

// version form
const showVersionDialog = ref(false)
const versionFormRef = ref(null)
const versionForm = ref({ version: '', description: '' })
const versionRules = { version: [{ required: true, message: '请输入版本号', trigger: 'blur' }] }

const loadOntology = async () => {
  const res = await ontologyAPI.get(ontologyId)
  ontology.value = res
  entityTypes.value = ontology.value.entity_types || []
  relationTypes.value = ontology.value.relation_types || []
}
const loadVersions = async () => {
  const res = await ontologyAPI.listVersions(ontologyId)
  versions.value = res || []
}
onMounted(() => { loadOntology(); loadVersions() })

// F4: 从 Model 导入
const importFromModelRef = ref(null)
const openImportFromModel = () => { importFromModelRef.value?.open() }
const handleImported = () => { loadOntology() }

// F5b: 从 Neo4j 引擎推导
const inferFromEngineRef = ref(null)
const openInferFromEngine = () => { inferFromEngineRef.value?.open() }

// entity type
const showEntityForm = (row) => {
  editingEntity.value = row
  if (row) {
    const slc = row.spatial_layer_config || {}
    entityForm.value = {
      name: row.name, label: row.label || '', description: row.description || '',
      color: row.color || '#5B8FF9',
      parent_id: row.parent_id || null,
      properties: Array.isArray(row.properties) ? row.properties.map(p => ({ ...p })) : [],
      is_spatial_layer: !!row.is_spatial_layer,
      spatial_layer_config: {
        geometry_type: slc.geometry_type || '',
        layer_name: slc.layer_name || row.name,
        lon_field: slc.lon_field || 'lon',
        lat_field: slc.lat_field || 'lat',
        geom_field: slc.geom_field || 'wkt',
      }
    }
  } else {
    entityForm.value = {
      name: '', label: '', description: '', color: '#5B8FF9',
      parent_id: null,
      properties: [],
      is_spatial_layer: false,
      spatial_layer_config: { geometry_type: '', layer_name: '', lon_field: 'lon', lat_field: 'lat', geom_field: 'wkt' }
    }
  }
  entityDialogVisible.value = true
}

// 空间图层开关切换
const onSpatialLayerToggle = (val) => {
  if (!val) {
    entityForm.value.spatial_layer_config.geometry_type = ''
  }
}

// 几何类型切换时，移除旧预填属性，追加新预填属性
const onGeometryTypeChange = (newType) => {
  const props = entityForm.value.properties
  // 移除旧预填字段（lon/lat/wkt）
  const spatialFields = ['lon', 'lat', 'wkt']
  entityForm.value.properties = props.filter(p => !spatialFields.includes(p.name))
  // 追加新预填字段
  if (newType === 'point') {
    if (!entityForm.value.properties.find(p => p.name === entityForm.value.spatial_layer_config.lon_field || p.name === 'lon')) {
      entityForm.value.properties.push({ name: entityForm.value.spatial_layer_config.lon_field || 'lon', label: '经度', data_type: 'float', required: false, unique: false })
    }
    if (!entityForm.value.properties.find(p => p.name === entityForm.value.spatial_layer_config.lat_field || p.name === 'lat')) {
      entityForm.value.properties.push({ name: entityForm.value.spatial_layer_config.lat_field || 'lat', label: '纬度', data_type: 'float', required: false, unique: false })
    }
  } else if (newType === 'wkt') {
    if (!entityForm.value.properties.find(p => p.name === entityForm.value.spatial_layer_config.geom_field || p.name === 'wkt')) {
      entityForm.value.properties.push({ name: entityForm.value.spatial_layer_config.geom_field || 'wkt', label: '几何(WKT)', data_type: 'geometry', required: false, unique: false })
    }
  }
  // 更新图层名称默认值
  if (!entityForm.value.spatial_layer_config.layer_name) {
    entityForm.value.spatial_layer_config.layer_name = entityForm.value.name
  }
}

// 获取实体类型的空间祖先名称（用于显示继承提示）
const getSpatialAncestor = (row) => {
  if (row.is_spatial_layer) return null
  if (!row.parent_id) return null
  let current = entityTypes.value.find(et => et.id === row.parent_id)
  let depth = 0
  while (current && depth < 10) {
    if (current.is_spatial_layer) return current.label || current.name
    if (!current.parent_id) break
    current = entityTypes.value.find(et => et.id === current.parent_id)
    depth++
  }
  return null
}
const submitEntityType = async () => {
  await entityFormRef.value.validate()
  saving.value = true
  try {
    const payload = { ...entityForm.value }
    if (!payload.is_spatial_layer) {
      payload.spatial_layer_config = null
    }
    if (editingEntity.value) {
      await ontologyAPI.updateEntityType(ontologyId, editingEntity.value.id, payload)
    } else {
      await ontologyAPI.createEntityType(ontologyId, payload)
    }
    ElMessage.success('保存成功')
    entityDialogVisible.value = false
    loadOntology()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}
const deleteEntityType = async (row) => {
  await ElMessageBox.confirm(`确认删除实体类型「${row.name}」？`, '提示', { type: 'warning' })
  await ontologyAPI.deleteEntityType(ontologyId, row.id)
  ElMessage.success('删除成功')
  loadOntology()
}

// relation type
const showRelationForm = (row) => {
  editingRelation.value = row
  if (row) {
    relationForm.value = {
      name: row.name, label: row.label || '', description: row.description || '',
      source_type_id: row.source_type_id || null, target_type_id: row.target_type_id || null,
      directed: row.directed,
      properties: Array.isArray(row.properties) ? row.properties.map(p => ({ ...p })) : []
    }
  } else {
    relationForm.value = { name: '', label: '', description: '', source_type_id: null, target_type_id: null, directed: true, properties: [] }
  }
  relationDialogVisible.value = true
}
const submitRelationType = async () => {
  await relationFormRef.value.validate()
  saving.value = true
  try {
    if (editingRelation.value) {
      await ontologyAPI.updateRelationType(ontologyId, editingRelation.value.id, relationForm.value)
    } else {
      await ontologyAPI.createRelationType(ontologyId, relationForm.value)
    }
    ElMessage.success('保存成功')
    relationDialogVisible.value = false
    loadOntology()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}
const deleteRelationType = async (row) => {
  await ElMessageBox.confirm(`确认删除关系类型「${row.name}」？`, '提示', { type: 'warning' })
  await ontologyAPI.deleteRelationType(ontologyId, row.id)
  ElMessage.success('删除成功')
  loadOntology()
}

// version
const submitVersion = async () => {
  await versionFormRef.value.validate()
  saving.value = true
  try {
    await ontologyAPI.createVersion(ontologyId, versionForm.value)
    ElMessage.success('版本快照创建成功')
    showVersionDialog.value = false
    versionForm.value = { version: '', description: '' }
    loadVersions()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '创建失败')
  } finally {
    saving.value = false
  }
}

const formatDate = (str) => str ? new Date(str).toLocaleString('zh-CN') : '-'
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; align-items: center; gap: 8px; margin-bottom: 20px; flex-wrap: wrap; }
.page-header h2 { margin: 0; font-size: 18px; }
.tab-toolbar { margin-bottom: 12px; }
.color-dot { display: inline-block; width: 16px; height: 16px; border-radius: 50%; }
.prop-table { width: 100%; }
.graph-tab-container { height: 560px; }
</style>
