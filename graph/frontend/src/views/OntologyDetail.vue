<template>
  <div class="page-container">
    <div class="page-header">
      <el-button link @click="$router.push('/ontologies')">
        <el-icon><ArrowLeft /></el-icon> {{ t('graph.common.back') }}
      </el-button>
      <h2>{{ ontology?.name }}</h2>
      <el-tag :type="ontology?.status === 'active' ? 'success' : 'info'" size="small">
        {{ ontology?.status === 'active' ? t('graph.common.active') : t('graph.common.archived') }}
      </el-tag>
      <el-button size="small" @click="$router.push(`/ontologies/${$route.params.id}/edit`)">{{ t('graph.common.edit') }}</el-button>
      <el-button size="small" type="success" @click="showVersionDialog = true">{{ t('graph.ontology.createSnapshot') }}</el-button>
      <el-button size="small" type="warning" @click="openImportFromModel">{{ t('graph.ontology.importFromModel') }}</el-button>
      <el-button size="small" type="info" @click="openInferFromEngine">{{ t('graph.ontology.inferFromNeo4j') }}</el-button>
    </div>

    <el-tabs v-model="activeTab">
      <!-- 实体类型 -->
      <el-tab-pane :label="t('graph.ontology.entityTypes')" name="entities">
        <div class="tab-toolbar">
          <el-button type="primary" size="small" @click="showEntityForm(null)">
            <el-icon><Plus /></el-icon> {{ t('graph.ontology.addEntityType') }}
          </el-button>
        </div>
        <el-table :data="entityTypes" border size="small">
          <el-table-column prop="name" :label="t('graph.ontology.identifier')" width="150" />
          <el-table-column prop="label" :label="t('graph.ontology.displayName')" width="150" />
          <el-table-column prop="description" :label="t('graph.common.description')" show-overflow-tooltip />
          <el-table-column :label="t('graph.ontology.spatialLayer')" width="120">
            <template #default="{ row }">
              <template v-if="row.is_spatial_layer">
                <el-tag type="success" size="small">
                  {{ t('graph.ontology.spatial') }}·{{ row.spatial_layer_config?.geometry_type === 'wkt' ? t('graph.ontology.linePolygon') : t('graph.ontology.point') }}
                </el-tag>
              </template>
              <template v-else-if="getSpatialAncestor(row)">
                <el-tooltip :content="t('graph.ontology.inheritSpatial', { name: getSpatialAncestor(row) })" placement="top">
                  <el-icon style="color:var(--el-color-success);cursor:default"><Location /></el-icon>
                </el-tooltip>
              </template>
            </template>
          </el-table-column>
          <el-table-column :label="t('graph.ontology.color')" width="80">
            <template #default="{ row }">
              <span class="color-dot" :style="{ background: row.color }"></span>
            </template>
          </el-table-column>
          <el-table-column :label="t('graph.common.actions')" width="120">
            <template #default="{ row }">
              <el-button link size="small" @click="showEntityForm(row)">{{ t('graph.common.edit') }}</el-button>
              <el-button link type="danger" size="small" @click="deleteEntityType(row)">{{ t('graph.common.delete') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 关系类型 -->
      <el-tab-pane :label="t('graph.ontology.relationTypes')" name="relations">
        <div class="tab-toolbar">
          <el-button type="primary" size="small" @click="showRelationForm(null)">
            <el-icon><Plus /></el-icon> {{ t('graph.ontology.addRelationType') }}
          </el-button>
        </div>
        <el-table :data="relationTypes" border size="small">
          <el-table-column prop="name" :label="t('graph.ontology.identifier')" width="150" />
          <el-table-column prop="label" :label="t('graph.ontology.displayName')" width="150" />
          <el-table-column :label="t('graph.ontology.sourceTarget')" width="200">
            <template #default="{ row }">
              {{ row.source_type?.label || row.source_type?.name || t('graph.ontology.any') }}
              → {{ row.target_type?.label || row.target_type?.name || t('graph.ontology.any') }}
            </template>
          </el-table-column>
          <el-table-column prop="directed" :label="t('graph.ontology.directed')" width="80">
            <template #default="{ row }">
              <el-tag size="small" :type="row.directed ? 'primary' : 'info'">
                {{ row.directed ? t('graph.ontology.directed') : t('graph.ontology.undirected') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('graph.common.actions')" width="120">
            <template #default="{ row }">
              <el-button link size="small" @click="showRelationForm(row)">{{ t('graph.common.edit') }}</el-button>
              <el-button link type="danger" size="small" @click="deleteRelationType(row)">{{ t('graph.common.delete') }}</el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 版本历史 -->
      <el-tab-pane :label="t('graph.ontology.versionHistory')" name="versions">
        <el-table :data="versions" border size="small">
          <el-table-column prop="version" :label="t('graph.ontology.versionNumber')" width="120" />
          <el-table-column prop="description" :label="t('graph.common.description')" show-overflow-tooltip />
          <el-table-column prop="created_at" :label="t('graph.common.createdAt')" width="180">
            <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 图形视图 -->
      <el-tab-pane :label="t('graph.ontology.graphView')" name="graph">
        <div class="graph-tab-container">
          <OntologyView :entity-types="entityTypes" :relation-types="relationTypes" />
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 实体类型表单对话框 -->
    <el-dialog v-model="entityDialogVisible" :title="editingEntity ? t('graph.ontology.editEntityType') : t('graph.ontology.addEntityType')" width="750px">
      <el-form ref="entityFormRef" :model="entityForm" :rules="entityRules" label-width="80px">
        <el-form-item :label="t('graph.ontology.identifier')" prop="name">
          <el-input v-model="entityForm.name" :placeholder="t('graph.ontology.identifierPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('graph.ontology.displayName')" prop="label">
          <el-input v-model="entityForm.label" :placeholder="t('graph.ontology.displayNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('graph.common.description')">
          <el-input v-model="entityForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('graph.ontology.parentType')">
          <el-select v-model="entityForm.parent_id" :placeholder="t('graph.ontology.noParent')" clearable style="width:100%">
            <el-option
              v-for="et in entityTypes.filter(e => e.id !== editingEntity?.id)"
              :key="et.id"
              :label="`${et.label || et.name}（${et.name}）`"
              :value="et.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('graph.ontology.color')">
          <el-color-picker v-model="entityForm.color" />
        </el-form-item>
        <el-form-item :label="t('graph.ontology.spatialLayer')">
          <el-switch v-model="entityForm.is_spatial_layer" @change="onSpatialLayerToggle" />
          <span v-if="entityForm.is_spatial_layer" style="margin-left:12px;font-size:12px;color:var(--el-text-color-secondary)">
            {{ t('graph.ontology.selectGeometryType') }}
          </span>
        </el-form-item>
        <template v-if="entityForm.is_spatial_layer">
          <el-form-item :label="t('graph.ontology.geometryType')">
            <el-radio-group v-model="entityForm.spatial_layer_config.geometry_type" @change="onGeometryTypeChange">
              <el-radio value="point">{{ t('graph.ontology.pointLonLat') }}</el-radio>
              <el-radio value="wkt">{{ t('graph.ontology.linePolygonWkt') }}</el-radio>
            </el-radio-group>
          </el-form-item>
          <el-form-item v-if="entityForm.spatial_layer_config.geometry_type" :label="t('graph.ontology.layerConfig')">
            <div style="display:flex;flex-direction:column;gap:8px;width:100%">
              <div style="display:flex;align-items:center;gap:8px">
                <span style="width:80px;flex-shrink:0;font-size:13px">{{ t('graph.ontology.layerName') }}</span>
                <el-input v-model="entityForm.spatial_layer_config.layer_name" size="small" style="flex:1" />
              </div>
              <template v-if="entityForm.spatial_layer_config.geometry_type === 'point'">
                <div style="display:flex;align-items:center;gap:8px">
                  <span style="width:80px;flex-shrink:0;font-size:13px">{{ t('graph.ontology.lonField') }}</span>
                  <el-input v-model="entityForm.spatial_layer_config.lon_field" size="small" style="flex:1" :placeholder="t('graph.ontology.defaultLon')" />
                </div>
                <div style="display:flex;align-items:center;gap:8px">
                  <span style="width:80px;flex-shrink:0;font-size:13px">{{ t('graph.ontology.latField') }}</span>
                  <el-input v-model="entityForm.spatial_layer_config.lat_field" size="small" style="flex:1" :placeholder="t('graph.ontology.defaultLat')" />
                </div>
              </template>
              <template v-if="entityForm.spatial_layer_config.geometry_type === 'wkt'">
                <div style="display:flex;align-items:center;gap:8px">
                  <span style="width:80px;flex-shrink:0;font-size:13px">{{ t('graph.ontology.geomField') }}</span>
                  <el-input v-model="entityForm.spatial_layer_config.geom_field" size="small" style="flex:1" :placeholder="t('graph.ontology.defaultWkt')" />
                </div>
              </template>
            </div>
          </el-form-item>
        </template>
        <el-form-item :label="t('graph.ontology.propertyDefs')">
          <div class="prop-table">
            <el-table :data="entityForm.properties" border size="small" style="width:100%">
              <el-table-column :label="t('graph.ontology.fieldName')" min-width="110">
                <template #default="{ row }">
                  <el-input v-model="row.name" size="small" :placeholder="t('graph.ontology.fieldNamePlaceholder')" />
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.ontology.displayName')" min-width="90">
                <template #default="{ row }">
                  <el-input v-model="row.label" size="small" :placeholder="t('graph.ontology.displayNamePlaceholder2')" />
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.ontology.dataType')" min-width="110">
                <template #default="{ row }">
                  <el-select v-model="row.data_type" size="small">
                    <el-option v-for="t in dataTypes" :key="t.value" :label="t.label" :value="t.value" />
                  </el-select>
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.ontology.required')" width="60" align="center">
                <template #default="{ row }">
                  <el-checkbox v-model="row.required" />
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.ontology.unique')" width="60" align="center">
                <template #default="{ row }">
                  <el-checkbox v-model="row.unique" />
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.common.actions')" width="60" align="center">
                <template #default="{ $index }">
                  <el-button link type="danger" size="small" @click="entityForm.properties.splice($index, 1)">{{ t('graph.common.del') }}</el-button>
                </template>
              </el-table-column>
            </el-table>
            <el-button size="small" style="margin-top:8px" @click="addEntityProp">+ {{ t('graph.ontology.addProperty') }}</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="entityDialogVisible = false">{{ t('graph.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitEntityType">{{ t('graph.common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 关系类型表单对话框 -->
    <el-dialog v-model="relationDialogVisible" :title="editingRelation ? t('graph.ontology.editRelationType') : t('graph.ontology.addRelationType')" width="750px">
      <el-form ref="relationFormRef" :model="relationForm" :rules="relationRules" label-width="80px">
        <el-form-item :label="t('graph.ontology.identifier')" prop="name">
          <el-input v-model="relationForm.name" :placeholder="t('graph.ontology.relationIdentifierPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('graph.ontology.displayName')" prop="label">
          <el-input v-model="relationForm.label" :placeholder="t('graph.ontology.relationDisplayNamePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('graph.common.description')">
          <el-input v-model="relationForm.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item :label="t('graph.ontology.sourceType')">
          <el-select v-model="relationForm.source_type_id" :placeholder="t('graph.ontology.any')" clearable>
            <el-option v-for="et in entityTypes" :key="et.id" :label="et.label || et.name" :value="et.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('graph.ontology.targetType')">
          <el-select v-model="relationForm.target_type_id" :placeholder="t('graph.ontology.any')" clearable>
            <el-option v-for="et in entityTypes" :key="et.id" :label="et.label || et.name" :value="et.id" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('graph.ontology.directed')">
          <el-switch v-model="relationForm.directed" />
        </el-form-item>
        <el-form-item :label="t('graph.ontology.propertyDefs')">
          <div class="prop-table">
            <el-table :data="relationForm.properties" border size="small" style="width:100%">
              <el-table-column :label="t('graph.ontology.fieldName')" min-width="110">
                <template #default="{ row }">
                  <el-input v-model="row.name" size="small" :placeholder="t('graph.ontology.fieldNamePlaceholder')" />
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.ontology.displayName')" min-width="90">
                <template #default="{ row }">
                  <el-input v-model="row.label" size="small" :placeholder="t('graph.ontology.displayNamePlaceholder2')" />
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.ontology.dataType')" min-width="110">
                <template #default="{ row }">
                  <el-select v-model="row.data_type" size="small">
                    <el-option v-for="t in dataTypes" :key="t.value" :label="t.label" :value="t.value" />
                  </el-select>
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.ontology.required')" width="60" align="center">
                <template #default="{ row }">
                  <el-checkbox v-model="row.required" />
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.ontology.unique')" width="60" align="center">
                <template #default="{ row }">
                  <el-checkbox v-model="row.unique" />
                </template>
              </el-table-column>
              <el-table-column :label="t('graph.common.actions')" width="60" align="center">
                <template #default="{ $index }">
                  <el-button link type="danger" size="small" @click="relationForm.properties.splice($index, 1)">{{ t('graph.common.del') }}</el-button>
                </template>
              </el-table-column>
            </el-table>
            <el-button size="small" style="margin-top:8px" @click="addRelationProp">+ {{ t('graph.ontology.addProperty') }}</el-button>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="relationDialogVisible = false">{{ t('graph.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitRelationType">{{ t('graph.common.save') }}</el-button>
      </template>
    </el-dialog>

    <!-- 版本快照对话框 -->
    <el-dialog v-model="showVersionDialog" :title="t('graph.ontology.createSnapshot')" width="400px">
      <el-form ref="versionFormRef" :model="versionForm" :rules="versionRules" label-width="80px">
        <el-form-item :label="t('graph.ontology.versionNumber')" prop="version">
          <el-input v-model="versionForm.version" :placeholder="t('graph.ontology.versionPlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('graph.common.description')">
          <el-input v-model="versionForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showVersionDialog = false">{{ t('graph.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="submitVersion">{{ t('graph.common.save') }}</el-button>
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
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

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
const entityRules = computed(() => ({ name: [{ required: true, message: t('graph.ontology.identifierRequired'), trigger: 'blur' }] }))

// relation form
const relationDialogVisible = ref(false)
const editingRelation = ref(null)
const relationFormRef = ref(null)
const relationForm = ref({ name: '', label: '', description: '', source_type_id: null, target_type_id: null, directed: true, properties: [] })
const relationRules = computed(() => ({ name: [{ required: true, message: t('graph.ontology.identifierRequired'), trigger: 'blur' }] }))

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
const versionRules = computed(() => ({ version: [{ required: true, message: t('graph.ontology.versionRequired'), trigger: 'blur' }] }))

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
      entityForm.value.properties.push({ name: entityForm.value.spatial_layer_config.lon_field || 'lon', label: t('graph.ontology.longitude'), data_type: 'float', required: false, unique: false })
    }
    if (!entityForm.value.properties.find(p => p.name === entityForm.value.spatial_layer_config.lat_field || p.name === 'lat')) {
      entityForm.value.properties.push({ name: entityForm.value.spatial_layer_config.lat_field || 'lat', label: t('graph.ontology.latitude'), data_type: 'float', required: false, unique: false })
    }
  } else if (newType === 'wkt') {
    if (!entityForm.value.properties.find(p => p.name === entityForm.value.spatial_layer_config.geom_field || p.name === 'wkt')) {
      entityForm.value.properties.push({ name: entityForm.value.spatial_layer_config.geom_field || 'wkt', label: t('graph.ontology.geometryWkt'), data_type: 'geometry', required: false, unique: false })
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
    ElMessage.success(t('graph.common.saveSuccess'))
    entityDialogVisible.value = false
    loadOntology()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.common.saveFailed'))
  } finally {
    saving.value = false
  }
}
const deleteEntityType = async (row) => {
  await ElMessageBox.confirm(t('graph.ontology.confirmDeleteEntity', { name: row.name }), t('graph.common.confirmDelete'), { type: 'warning' })
  await ontologyAPI.deleteEntityType(ontologyId, row.id)
  ElMessage.success(t('graph.common.deleteSuccess'))
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
    ElMessage.success(t('graph.common.saveSuccess'))
    relationDialogVisible.value = false
    loadOntology()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.common.saveFailed'))
  } finally {
    saving.value = false
  }
}
const deleteRelationType = async (row) => {
  await ElMessageBox.confirm(t('graph.ontology.confirmDeleteRelation', { name: row.name }), t('graph.common.confirmDelete'), { type: 'warning' })
  await ontologyAPI.deleteRelationType(ontologyId, row.id)
  ElMessage.success(t('graph.common.deleteSuccess'))
  loadOntology()
}

// version
const submitVersion = async () => {
  await versionFormRef.value.validate()
  saving.value = true
  try {
    await ontologyAPI.createVersion(ontologyId, versionForm.value)
    ElMessage.success(t('graph.ontology.snapshotSuccess'))
    showVersionDialog.value = false
    versionForm.value = { version: '', description: '' }
    loadVersions()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.common.createFailed'))
  } finally {
    saving.value = false
  }
}

const formatDate = (str) => str ? new Date(str).toLocaleString() : '-'
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; align-items: center; gap: 8px; margin-bottom: 20px; flex-wrap: wrap; }
.page-header h2 { margin: 0; font-size: 18px; }
.tab-toolbar { margin-bottom: 12px; }
.color-dot { display: inline-block; width: 16px; height: 16px; border-radius: 50%; }
.prop-table { width: 100%; }
/* page-header ~40px + margin-bottom 20px + tabs-header ~40px + padding top+bottom 40px = ~140px */
.graph-tab-container { height: calc(100vh - 140px); }
</style>
