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
        <el-form-item label="颜色">
          <el-color-picker v-model="entityForm.color" />
        </el-form-item>
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
                    <el-option v-for="t in dataTypes" :key="t" :label="t" :value="t" />
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
        <el-button
          v-if="editingEntity"
          :loading="syncing"
          @click="showSyncDialog = true"
        >同步约束到 Neo4j</el-button>
        <el-button type="primary" :loading="saving" @click="submitEntityType">保存</el-button>
      </template>
    </el-dialog>

    <!-- 同步约束对话框 -->
    <el-dialog v-model="showSyncDialog" title="同步约束到 Neo4j" width="400px" append-to-body>
      <el-form label-width="100px">
        <el-form-item label="选择图谱">
          <el-select v-model="syncGraphId" placeholder="选择要同步的知识图谱" style="width:100%">
            <el-option v-for="g in linkedGraphs" :key="g.id" :label="g.name" :value="g.id" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-text type="info" size="small">
            将为属性中"唯一"勾选的字段在 Neo4j 创建 UNIQUE 约束（IF NOT EXISTS，幂等操作）
          </el-text>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showSyncDialog = false">取消</el-button>
        <el-button type="primary" :loading="syncing" :disabled="!syncGraphId" @click="syncConstraints">
          执行同步
        </el-button>
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
                    <el-option v-for="t in dataTypes" :key="t" :label="t" :value="t" />
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
import { ref, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Plus } from '@element-plus/icons-vue'
import { ontologyAPI, knowledgeGraphAPI } from '../api/ontology'
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
const entityForm = ref({ name: '', label: '', description: '', color: '#5B8FF9', properties: [] })
const entityRules = { name: [{ required: true, message: '请输入标识符', trigger: 'blur' }] }

// relation form
const relationDialogVisible = ref(false)
const editingRelation = ref(null)
const relationFormRef = ref(null)
const relationForm = ref({ name: '', label: '', description: '', source_type_id: null, target_type_id: null, directed: true, properties: [] })
const relationRules = { name: [{ required: true, message: '请输入标识符', trigger: 'blur' }] }

const dataTypes = ['string', 'integer', 'float', 'boolean', 'date', 'datetime']

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

// sync constraints
const showSyncDialog = ref(false)
const syncGraphId = ref(null)
const syncing = ref(false)
const linkedGraphs = ref([])

const loadLinkedGraphs = async () => {
  try {
    const res = await knowledgeGraphAPI.list()
    const all = Array.isArray(res) ? res : (res?.data?.data || res?.data || [])
    linkedGraphs.value = all.filter(g => g.ontology_id === Number(ontologyId))
  } catch (e) {
    // 非关键，忽略错误
  }
}

const syncConstraints = async () => {
  if (!syncGraphId.value || !editingEntity.value) return
  syncing.value = true
  try {
    await ontologyAPI.syncEntityTypeConstraints(ontologyId, editingEntity.value.id, syncGraphId.value)
    ElMessage.success('约束同步成功')
    showSyncDialog.value = false
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '同步失败')
  } finally {
    syncing.value = false
  }
}

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
onMounted(() => { loadOntology(); loadVersions(); loadLinkedGraphs() })

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
    entityForm.value = {
      name: row.name, label: row.label || '', description: row.description || '',
      color: row.color || '#5B8FF9',
      properties: Array.isArray(row.properties) ? row.properties.map(p => ({ ...p })) : []
    }
  } else {
    entityForm.value = { name: '', label: '', description: '', color: '#5B8FF9', properties: [] }
  }
  entityDialogVisible.value = true
}
const submitEntityType = async () => {
  await entityFormRef.value.validate()
  saving.value = true
  try {
    if (editingEntity.value) {
      await ontologyAPI.updateEntityType(ontologyId, editingEntity.value.id, entityForm.value)
    } else {
      await ontologyAPI.createEntityType(ontologyId, entityForm.value)
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
