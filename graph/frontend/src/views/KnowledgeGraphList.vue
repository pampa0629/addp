<template>
  <div class="page-container">
    <div class="page-header">
      <h2>{{ t('graph.knowledgeGraph.title') }}</h2>
      <el-button v-if="authStore.hasPermission('graph.graph.create') && authStore.hasPermission('graph.ontology.read') && authStore.hasPermission('system.engine_catalog.read')" type="primary" @click="showCreateDialog = true">
        <el-icon><Plus /></el-icon> {{ t('graph.knowledgeGraph.create') }}
      </el-button>
    </div>

    <el-table v-loading="loading" :data="graphs" border style="width:100%">
      <el-table-column prop="name" :label="t('graph.common.name')" min-width="150" show-overflow-tooltip />
      <el-table-column :label="t('graph.knowledgeGraph.bindOntology')" min-width="150" show-overflow-tooltip>
        <template #default="{ row }">{{ row.ontology?.name || '-' }}</template>
      </el-table-column>
      <el-table-column :label="t('graph.knowledgeGraph.targetGraph')" min-width="180" show-overflow-tooltip><template #default="{ row }">{{ row.catalog_path?.segments.slice(1).map(segment => segment.name).join(' / ') }}</template></el-table-column>
      <el-table-column prop="status" :label="t('graph.common.status')" width="100">
        <template #default="{ row }">
          <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('graph.knowledgeGraph.admission')" min-width="170">
        <template #default="{ row }">
          <span>{{ t(row.admission?.has_unresolved_operations ? 'graph.knowledgeGraph.admissionBlocked' : row.admission?.closed ? 'graph.knowledgeGraph.admissionClosed' : 'graph.knowledgeGraph.admissionOpen') }}</span>
          <div v-if="row.admission?.has_unresolved_operations">{{ t('graph.knowledgeGraph.unresolvedOperations') }}</div>
          <template v-if="row.admission?.closed && authStore.hasPermission('graph.graph.reconcile')">
            <el-button link @click="handleControl(row, 'reconcile')">{{ t('graph.knowledgeGraph.reconcile') }}</el-button>
            <el-button v-if="authStore.hasPermission('graph.graph.update')" link @click="handleControl(row, 'reopen')">{{ t('graph.knowledgeGraph.reopen') }}</el-button>
          </template>
        </template>
      </el-table-column>
      <el-table-column prop="description" :label="t('graph.common.description')" show-overflow-tooltip />
      <el-table-column prop="created_at" :label="t('graph.common.createdAt')" width="180">
        <template #default="{ row }">{{ formatDate(row.created_at) }}</template>
      </el-table-column>
      <el-table-column :label="t('graph.common.actions')" width="320" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" size="small" @click="handleBrowse(row)">{{ t('graph.knowledgeGraph.explore') }}</el-button>
          <el-button v-if="authStore.hasPermission('graph.build_task.read')" link type="success" size="small" @click="handleBuild(row)">{{ t('graph.knowledgeGraph.build') }}</el-button>
          <el-button v-if="authStore.hasPermission('graph.review.read')" link type="warning" size="small" @click="handleReview(row)">
            {{ t('graph.knowledgeGraph.review') }}<template v-if="pendingCounts[row.id]">（{{ pendingCounts[row.id] }}）</template>
          </el-button>
          <el-button v-if="authStore.hasPermission('graph.graph.update')" link type="primary" size="small" @click="handleEdit(row)">{{ t('graph.common.edit') }}</el-button>
          <el-button v-if="authStore.hasPermission('graph.ontology.read') && authStore.hasPermission('graph.ontology.update')" link type="warning" size="small" @click="handleInferSchema(row)">{{ t('graph.knowledgeGraph.inferOntology') }}</el-button>
          <el-button v-if="authStore.hasPermission('graph.graph.delete')" link type="danger" size="small" @click="handleDelete(row)">{{ t('graph.common.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <!-- 创建图谱对话框 -->
    <el-dialog v-model="showCreateDialog" :title="t('graph.knowledgeGraph.createTitle')" width="500px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item :label="t('graph.knowledgeGraph.graphName')" prop="name">
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item :label="t('graph.knowledgeGraph.bindOntology')" prop="ontology_id">
          <el-select v-model="form.ontology_id" style="width:100%">
            <el-option v-for="o in ontologies" :key="o.id" :label="o.name" :value="o.id" />
          </el-select>
        </el-form-item>
        <GraphTargetFields v-if="showCreateDialog" v-model="form" />
        <el-form-item :label="t('graph.knowledgeGraph.indexManagement')" prop="index_management">
          <el-radio-group v-model="form.index_management">
            <el-radio value="managed">{{ t('graph.knowledgeGraph.indexManaged') }}</el-radio>
            <el-radio value="external">{{ t('graph.knowledgeGraph.indexExternal') }}</el-radio>
          </el-radio-group>
          <div class="index-help">{{ t(form.index_management === 'managed' ? 'graph.knowledgeGraph.indexManagedHelp' : 'graph.knowledgeGraph.indexExternalHelp') }}</div>
        </el-form-item>
        <el-form-item :label="t('graph.common.description')">
          <el-input v-model="form.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showCreateDialog = false">{{ t('graph.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" :disabled="!form.catalog_path" @click="handleCreate">{{ t('graph.common.create') }}</el-button>
      </template>
    </el-dialog>

    <!-- 编辑图谱对话框（只允许改名称和描述） -->
    <el-dialog v-model="showEditDialog" :title="t('graph.knowledgeGraph.edit')" width="500px">
      <el-form ref="editFormRef" :model="editForm" :rules="editRules" label-width="100px">
        <el-form-item :label="t('graph.knowledgeGraph.graphName')" prop="name">
          <el-input v-model="editForm.name" />
        </el-form-item>
        <el-form-item :label="t('graph.common.description')">
          <el-input v-model="editForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="showEditDialog = false">{{ t('graph.common.cancel') }}</el-button>
        <el-button type="primary" :loading="saving" @click="handleUpdate">{{ t('graph.common.save') }}</el-button>
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
import { ref, onMounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus } from '@element-plus/icons-vue'
import { ontologyAPI, knowledgeGraphAPI } from '../api/ontology'
import { buildAPI } from '../api/graphBuild'
import SchemaInferenceDialog from '../components/SchemaInferenceDialog.vue'
import { useI18n } from 'vue-i18n'
import { navigateGraphRoute } from '@/utils/moduleNavigation'
import GraphTargetFields from '../components/GraphTargetFields.vue'
import { useAuthStore } from '../store/auth'

const { t } = useI18n()
const loading = ref(false)
const saving = ref(false)
const graphs = ref([])
const ontologies = ref([])
const showCreateDialog = ref(false)
const showEditDialog = ref(false)
const formRef = ref(null)
const editFormRef = ref(null)
const form = ref({ name: '', ontology_id: null, engine_id: null, catalog_path: null, index_management: 'external', description: '' })
const editForm = ref({ id: null, name: '', description: '' })
const pendingCounts = ref({})
const editRules = computed(() => ({
  name: [{ required: true, message: t('graph.knowledgeGraph.nameRequired'), trigger: 'change' }]
}))
const router = useRouter()
const authStore = useAuthStore()
const rules = computed(() => ({
  name: [{ required: true, message: t('graph.knowledgeGraph.nameRequired'), trigger: 'change' }],
  ontology_id: [{ required: true, message: t('graph.knowledgeGraph.ontologyRequired'), trigger: 'change' }],
  engine_id: [{ required: true, message: t('graph.knowledgeGraph.engineRequired'), trigger: 'change' }],
  catalog_path: [{ required: true, message: t('graph.knowledgeGraph.graphRequired'), trigger: 'change' }],
  index_management: [{ required: true, trigger: 'change' }]
}))

const load = async () => {
  loading.value = true
  try {
    const [gr, or] = await Promise.all([
      knowledgeGraphAPI.list(),
      authStore.hasPermission('graph.ontology.read') ? ontologyAPI.list() : Promise.resolve([])
    ])
    graphs.value = gr || []
    ontologies.value = or || []
    for (const g of authStore.hasPermission('graph.review.read') ? graphs.value : []) {
      buildAPI.getPendingCount(g.id).then(r => {
        if (r.data.count > 0) pendingCounts.value[g.id] = r.data.count
      }).catch(() => {})
    }
  } catch {
    ElMessage.error(t('graph.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

const handleCreate = async () => {
  await formRef.value.validate()
  saving.value = true
  try {
    await knowledgeGraphAPI.create(form.value)
    ElMessage.success(t('graph.common.createSuccess'))
    showCreateDialog.value = false
    load()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.common.createFailed'))
  } finally {
    saving.value = false
  }
}

const handleDelete = async (row) => {
  await ElMessageBox.confirm(t('graph.knowledgeGraph.confirmDelete', { name: row.name }), t('graph.common.confirmDelete'), { type: 'warning' })
  try {
    await knowledgeGraphAPI.delete(row.id, row.version)
    ElMessage.success(t('graph.common.deleteSuccess'))
    load()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.common.deleteFailed'))
  }
}

const handleControl = async (row, action) => {
  try {
    await knowledgeGraphAPI[action](row.id, row.version)
    await load()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.common.saveFailed'))
  }
}

const handleBrowse = (row) => {
  navigateGraphRoute(router, { name: 'GraphBrowser', params: { id: row.id } })
}

const handleBuild = (row) => {
  navigateGraphRoute(router, { name: 'BuildManager', params: { id: row.id } })
}

const handleReview = (row) => {
  navigateGraphRoute(router, { name: 'ReviewQueue', params: { id: row.id } })
}

const handleEdit = (row) => {
  editForm.value = { id: row.id, version: row.version, name: row.name, description: row.description || '' }
  showEditDialog.value = true
}

const handleUpdate = async () => {
  await editFormRef.value.validate()
  saving.value = true
  try {
    await knowledgeGraphAPI.update(editForm.value.id, { version: editForm.value.version, name: editForm.value.name, description: editForm.value.description })
    ElMessage.success(t('graph.common.saveSuccess'))
    showEditDialog.value = false
    load()
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

const inferGraphId = ref(null)
const schemaInferenceRef = ref(null)
const handleInferSchema = async (row) => {
  inferGraphId.value = row.id
  await new Promise(r => setTimeout(r, 0))
  schemaInferenceRef.value?.open()
}

const statusType = (s) => ({ active: 'success', building: 'warning', error: 'danger', archived: 'info' }[s] || 'info')
const statusLabel = (s) => ({
  active: t('graph.knowledgeGraph.statusActive'),
  building: t('graph.knowledgeGraph.statusBuilding'),
  error: t('graph.knowledgeGraph.statusError'),
  archived: t('graph.knowledgeGraph.statusArchived')
}[s] || s)
const formatDate = (str) => str ? new Date(str).toLocaleString() : '-'

onMounted(load)
</script>

<style scoped>
.index-help { color: var(--addp-text-secondary); font-size: 12px; }
.page-container { padding: 20px; }
.page-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 16px; }
.page-header h2 { margin: 0; font-size: 18px; }
</style>
