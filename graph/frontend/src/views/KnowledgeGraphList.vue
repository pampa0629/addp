<template>
  <div class="page-container">
    <div class="page-header">
      <h2>知识图谱</h2>
      <el-button type="primary" @click="showCreateDialog = true">
        <el-icon><Plus /></el-icon> 新建图谱
      </el-button>
    </div>

    <el-table v-loading="loading" :data="graphs" border style="width:100%">
      <el-table-column prop="name" label="名称" min-width="150" />
      <el-table-column label="绑定本体" min-width="150">
        <template #default="{ row }">{{ row.ontology?.name || '-' }}</template>
      </el-table-column>
      <el-table-column prop="database" label="Neo4j 数据库" width="160" />
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="描述" show-overflow-tooltip />
      <el-table-column prop="created_at" label="创建时间" width="180">
        <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="320" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="handleBrowse(row)">探索</el-button>
          <el-button link type="success" size="small" @click="handleBuild(row)">构建</el-button>
          <el-button link type="warning" size="small" @click="handleReview(row)">
            审核<template v-if="pendingCounts[row.id]">（{{ pendingCounts[row.id] }}）</template>
          </el-button>
          <el-button link type="primary" size="small" @click="handleEdit(row)">编辑</el-button>
          <el-button link type="warning" size="small" @click="handleInferSchema(row)">推导本体</el-button>
          <el-button link type="danger" size="small" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 创建图谱对话框 -->
    <el-dialog v-model="showCreateDialog" title="新建知识图谱" width="500px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item label="图谱名称" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="绑定本体" prop="ontology_id">
          <el-select v-model="form.ontology_id" style="width:100%">
            <el-option v-for="o in ontologies" :key="o.id" :label="o.name" :value="o.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="引擎" prop="engine_id">
          <el-select v-model="form.engine_id" style="width:100%" placeholder="请选择 Neo4j 引擎" :loading="enginesLoading" @change="onEngineChange">
            <el-option v-for="e in neo4jEngines" :key="e.id" :label="e.name" :value="e.id" />
          </el-select>
        </el-form-item>
        <el-form-item label="数据库名称" prop="database">
          <el-select v-model="form.database" style="width:100%" placeholder="请先选择引擎" :loading="dbLoading" :disabled="!form.engine_id">
            <el-option v-for="db in databases" :key="db" :label="db" :value="db" />
          </el-select>
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleCreate">创建</el-button>
      </template>
    </el-dialog>

    <!-- 编辑图谱对话框（只允许改名称和描述） -->
    <el-dialog v-model="showEditDialog" title="编辑知识图谱" width="500px">
      <el-form ref="editFormRef" :model="editForm" :rules="editRules" label-width="100px">
        <el-form-item label="图谱名称" prop="name">
          <el-input v-model="editForm.name" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="editForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEditDialog = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="handleUpdate">保存</el-button>
      </template>
    </el-dialog>

    <!-- F5: 推导本体对话框 -->
    <SchemaInferenceDialog
      v-if="inferGraphId"
      ref="schemaInferenceRef"
      :graph-id="inferGraphId"
      @applied="load"
    />
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { ontologyAPI, knowledgeGraphAPI, engineAPI } from '../api/ontology'
import { buildAPI } from '../api/graphBuild'
import SchemaInferenceDialog from '../components/SchemaInferenceDialog.vue'

const loading = ref(false)
const saving = ref(false)
const graphs = ref([])
const ontologies = ref([])
const neo4jEngines = ref([])
const databases = ref([])
const enginesLoading = ref(false)
const dbLoading = ref(false)
const showCreateDialog = ref(false)
const showEditDialog = ref(false)
const formRef = ref(null)
const editFormRef = ref(null)
const form = ref({ name: '', ontology_id: null, engine_id: null, database: '', description: '' })
const editForm = ref({ id: null, name: '', description: '' })
const pendingCounts = ref({})
const editRules = {
  name: [{ required: true, message: '请输入名称', trigger: 'change' }]
}
const router = useRouter()
const rules = {
  name: [{ required: true, message: '请输入名称', trigger: 'change' }],
  ontology_id: [{ required: true, message: '请选择本体', trigger: 'change' }],
  engine_id: [{ required: true, message: '请选择引擎', trigger: 'change' }],
  database: [{ required: true, message: '请选择数据库名称', trigger: 'change' }]
}

const loadEngines = async () => {
  enginesLoading.value = true
  try {
    neo4jEngines.value = await engineAPI.getNeo4jEngines()
  } catch {
    ElMessage.error('加载引擎列表失败')
  } finally {
    enginesLoading.value = false
  }
}

const onEngineChange = async (engineId) => {
  form.value.database = ''
  databases.value = []
  if (!engineId) return
  dbLoading.value = true
  try {
    databases.value = await engineAPI.getDatabases(engineId)
  } catch {
    ElMessage.error('加载数据库列表失败')
  } finally {
    dbLoading.value = false
  }
}

watch(showCreateDialog, (val) => {
  if (val) loadEngines()
})

const load = async () => {
  loading.value = true
  try {
    const [gr, or] = await Promise.all([knowledgeGraphAPI.list(), ontologyAPI.list()])
    graphs.value = gr || []
    ontologies.value = or || []
    // 异步加载每个图谱的 pending review 数量
    for (const g of graphs.value) {
      buildAPI.getPendingCount(g.id).then(r => {
        if (r.data.count > 0) pendingCounts.value[g.id] = r.data.count
      }).catch(() => {})
    }
  } catch {
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
}

const handleCreate = async () => {
  await formRef.value.validate()
  saving.value = true
  try {
    await knowledgeGraphAPI.create(form.value)
    ElMessage.success('创建成功')
    showCreateDialog.value = false
    load()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '创建失败')
  } finally {
    saving.value = false
  }
}

const handleDelete = async (row) => {
  await ElMessageBox.confirm(`确认删除图谱「${row.name}」？`, '提示', { type: 'warning' })
  try {
    await knowledgeGraphAPI.delete(row.id)
    ElMessage.success('删除成功')
    load()
  } catch {
    ElMessage.error('删除失败')
  }
}

const handleBrowse = (row) => {
  router.push({ name: 'GraphBrowser', params: { id: row.id } })
}

const handleBuild = (row) => {
  router.push({ name: 'BuildManager', params: { id: row.id } })
}

const handleReview = (row) => {
  router.push({ name: 'ReviewQueue', params: { id: row.id } })
}

const handleEdit = (row) => {
  editForm.value = { id: row.id, name: row.name, description: row.description || '' }
  showEditDialog.value = true
}

const handleUpdate = async () => {
  await editFormRef.value.validate()
  saving.value = true
  try {
    await knowledgeGraphAPI.update(editForm.value.id, { name: editForm.value.name, description: editForm.value.description })
    ElMessage.success('保存成功')
    showEditDialog.value = false
    load()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

const inferGraphId = ref(null)
const schemaInferenceRef = ref(null)
const handleInferSchema = async (row) => {
  inferGraphId.value = row.id
  // nextTick 确保组件已渲染
  await new Promise(r => setTimeout(r, 0))
  schemaInferenceRef.value?.open()
}

const statusType = (s) => ({ active: 'success', building: 'warning', error: 'danger', archived: 'info' }[s] || 'info')
const statusLabel = (s) => ({ active: '运行中', building: '构建中', error: '错误', archived: '归档' }[s] || s)
const formatDate = (str) => str ? new Date(str).toLocaleString('zh-CN') : '-'

onMounted(load)
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.page-header h2 { margin: 0; font-size: 18px; }
</style>
