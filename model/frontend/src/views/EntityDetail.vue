<template>
  <div class="entity-detail">
    <!-- 顶部操作栏 -->
    <div class="detail-header">
      <div class="header-left">
        <el-button text @click="backToList">
          <el-icon><ArrowLeft /></el-icon>
          {{ t('model.common.back') }}
        </el-button>
        <span class="entity-name">{{ entity.name || t('model.entity.detail') }}</span>
        <el-tag v-if="entity.status" :type="entity.status === 'approved' ? 'success' : 'info'" size="small">
          {{ entity.status === 'approved' ? t('model.common.status_approved') : t('model.common.status_draft') }}
        </el-tag>
        <el-tag v-if="isDirty" type="warning" size="small">{{ t('model.common.unsaved') }}</el-tag>
      </div>
      <div v-if="!pageLoading && !pageError" class="header-right">
        <el-button :title="t('model.common.refresh')" :aria-label="t('model.common.refresh')" @click="handleRefresh">
          <el-icon><Refresh /></el-icon>
        </el-button>
        <el-button v-if="canEditEntity" type="primary" @click="handleSave" :loading="saving">{{ t('model.common.save') }}</el-button>
        <el-button
          v-if="entity.status === 'draft' && can('model.entity.approve')"
          type="success"
          @click="handleApprove"
        >{{ t('model.entity.approve') }}</el-button>
        <el-button v-if="entity.status === 'approved' && can('model.entity.update')" @click="handleReopen">
          {{ t('model.common.reopen') }}
        </el-button>
      </div>
    </div>

    <el-skeleton v-if="pageLoading" :rows="8" animated />
    <el-result
      v-else-if="pageError"
      icon="error"
      :title="t('model.common.load_failed')"
      :sub-title="pageError"
    >
      <template #extra>
        <el-button type="primary" @click="loadPage">{{ t('model.common.retry') }}</el-button>
      </template>
    </el-result>

    <template v-else>

    <el-alert
      v-if="referenceError"
      class="reference-warning"
      type="warning"
      :title="referenceError"
      show-icon
      :closable="false"
    />

    <!-- Tab 标签页 -->
    <el-tabs v-model="activeTab" type="border-card" @tab-change="handleTabChange">
      <!-- 基本信息标签页 -->
      <el-tab-pane :label="t('model.logical_table.basic_info')" name="basic">
        <el-form :model="form" label-width="90px">
          <el-row :gutter="16">
            <el-col :xs="24" :md="12">
              <el-form-item :label="t('model.entity.name')">
                <el-input v-model="form.name" maxlength="200" :disabled="!canEditEntity" />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :md="12">
              <el-form-item :label="t('model.entity.code')">
                <el-input :value="entity.code" disabled />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :md="12">
              <el-form-item :label="t('model.entity.domain')">
                <el-select v-model="form.domain_id" :disabled="!canEditEntity" clearable style="width:100%">
                  <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :span="24">
              <el-form-item :label="t('model.entity.description')">
                <el-input v-model="form.description" type="textarea" :rows="2" :disabled="!canEditEntity" />
              </el-form-item>
            </el-col>
          </el-row>
        </el-form>
      </el-tab-pane>

      <!-- 属性列表标签页 -->
      <el-tab-pane :label="t('model.attribute.title')" name="attributes">
        <div class="tab-header">
          <el-button v-if="canCreateAttribute" type="primary" size="small" @click="openAttrDialog()">
            <el-icon><Plus /></el-icon>
            {{ t('model.attribute.add') }}
          </el-button>
        </div>

        <el-table :data="attributes" v-loading="attrLoading" stripe>
          <el-table-column :label="t('model.attribute.index')" type="index" width="60" />
          <el-table-column :label="t('model.attribute.name')" prop="name" min-width="140" />
          <el-table-column :label="t('model.attribute.column_name')" prop="column_name" min-width="140" />
          <el-table-column :label="t('model.attribute.data_type')" prop="data_type" width="110" />
          <el-table-column :label="t('model.attribute.element')" min-width="190">
            <template #default="{ row }">
              <span>{{ getElementName(row.element_id) || '-' }}</span>
              <el-tag v-if="row.element_revision_id" type="info" size="small" class="revision-tag">
                {{ t('model.attribute.element_revision') }} #{{ row.element_revision_id }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.attribute.is_pk')" width="80">
            <template #default="{ row }">
              <el-tag v-if="row.is_pk" type="warning" size="small">PK</el-tag>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.attribute.nullable')" width="80">
            <template #default="{ row }">
              <el-tag :type="row.nullable ? 'info' : 'danger'" size="small">
                {{ row.nullable ? t('model.common.yes') : t('model.common.no') }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column :label="t('model.attribute.description')" prop="description" show-overflow-tooltip />
          <el-table-column :label="t('model.attribute.actions')" width="130" fixed="right">
            <template #default="{ row }">
              <el-button v-if="canEditEntity" link type="primary" @click="openAttrDialog(row)">{{ t('model.common.edit') }}</el-button>
              <el-popconfirm v-if="canDeleteAttribute" :title="t('model.attribute.delete_confirm')" @confirm="deleteAttr(row.id)">
                <template #reference>
                  <el-button link type="danger">{{ t('model.common.delete') }}</el-button>
                </template>
              </el-popconfirm>
            </template>
          </el-table-column>
        </el-table>
      </el-tab-pane>

      <!-- 实体关系标签页 -->
      <el-tab-pane :label="t('model.relation.title')" name="relations">
        <!-- 上半部分：关系管理 -->
        <div class="relations-management">
          <div class="tab-header">
            <el-button v-if="canCreateRelation" type="primary" size="small" @click="openRelationDialog()">
              <el-icon><Plus /></el-icon>
              {{ t('model.relation.add') }}
            </el-button>
          </div>

          <el-table :data="relations" v-loading="relationLoading" stripe>
            <el-table-column :label="t('model.relation.index')" type="index" width="60" />
            <el-table-column :label="t('model.relation.type')" width="120">
              <template #default="{ row }">
                <el-tag :type="getRelationTypeTag(row.relation_type)" size="small">
                  {{ formatRelationType(row.relation_type) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.relation.direction')" width="250">
              <template #default="{ row }">
                <span v-if="row.source_entity === entityId">
                  {{ entity.name }} → {{ getEntityName(row.target_entity) }}
                </span>
                <span v-else>
                  {{ getEntityName(row.source_entity) }} → {{ entity.name }}
                </span>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.relation.target')" width="150">
              <template #default="{ row }">
                <el-link type="primary" @click="navigateToEntity(getTargetEntityId(row))">
                  {{ getTargetEntityName(row) }}
                </el-link>
              </template>
            </el-table-column>
            <el-table-column :label="t('model.relation.name')" prop="name" min-width="120" />
            <el-table-column :label="t('model.relation.description')" prop="description" show-overflow-tooltip />
            <el-table-column :label="t('model.relation.actions')" width="130" fixed="right">
              <template #default="{ row }">
                <el-button v-if="canModifyRelation(row, 'model.entity_relation.update')" link type="primary" @click="openRelationDialog(row)">{{ t('model.common.edit') }}</el-button>
                <el-popconfirm v-if="canModifyRelation(row, 'model.entity_relation.delete')" :title="t('model.relation.delete_confirm')" @confirm="deleteRelation(row)">
                  <template #reference>
                    <el-button link type="danger">{{ t('model.common.delete') }}</el-button>
                  </template>
                </el-popconfirm>
              </template>
            </el-table-column>
          </el-table>
        </div>

        <!-- 分割线 -->
        <el-divider />

        <!-- 下半部分：局部ER图 -->
        <div class="local-er-diagram">
          <div class="diagram-toolbar">
            <h3>{{ t('model.er_diagram.local_er_title') }}</h3>
            <div>
              <el-button size="small" @click="refreshLocalDiagram">
                <el-icon><Refresh /></el-icon> {{ t('model.er_diagram.refresh') }}
              </el-button>
              <el-button size="small" @click="copyMermaidCode">
                <el-icon><DocumentCopy /></el-icon> {{ t('model.er_diagram.copy_mermaid') }}
              </el-button>
            </div>
          </div>

          <!-- Mermaid渲染区域 -->
          <div ref="mermaidContainer" class="mermaid-viewer" v-loading="mermaidLoading">
            <pre class="mermaid">{{ localMermaidCode }}</pre>
          </div>
        </div>
      </el-tab-pane>
    </el-tabs>

    <!-- 属性对话框 -->
    <el-dialog
      v-model="attrDialogVisible"
      class="addp-dialog"
      :title="editingAttr ? t('model.attribute.edit') : t('model.attribute.add')"
      width="min(480px, calc(100vw - 32px))"
    >
      <el-form ref="attrFormRef" :model="attrForm" :rules="attrRules" label-width="100px">
        <el-form-item :label="t('model.attribute.name')" prop="name">
          <el-input v-model="attrForm.name" maxlength="200" :placeholder="t('model.attribute.name_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.attribute.column_name')" prop="column_name">
          <el-input v-model="attrForm.column_name" maxlength="200" :placeholder="t('model.attribute.column_name_placeholder')" />
        </el-form-item>
        <el-form-item :label="t('model.attribute.data_type')" prop="data_type">
          <el-select v-model="attrForm.data_type" style="width:100%">
            <el-option v-for="type in attributeDataTypes" :key="type" :label="type" :value="type" />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.attribute.element')">
          <el-select
            v-model="attrForm.element_id"
            :placeholder="t('model.attribute.element_placeholder')"
            clearable
            filterable
            style="width:100%"
          >
            <el-option
              v-for="e in elements"
              :key="e.id"
              :label="`${e.name} (${e.code})`"
              :value="e.id"
            />
          </el-select>
        </el-form-item>
        <el-form-item :label="t('model.field.is_pk')">
          <el-switch v-model="attrForm.is_pk" />
        </el-form-item>
        <el-form-item :label="t('model.field.nullable')">
          <el-switch v-model="attrForm.nullable" />
        </el-form-item>
        <el-form-item :label="t('model.attribute.description')">
          <el-input v-model="attrForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="attrDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleAttrSubmit" :loading="attrSubmitting">
          {{ editingAttr ? t('model.common.save') : t('model.common.add') }}
        </el-button>
      </template>
    </el-dialog>

    <!-- 关系对话框 -->
    <el-dialog
      v-model="relationDialogVisible"
      class="addp-dialog"
      :title="editingRelation ? t('model.relation.edit') : t('model.relation.add')"
      width="min(600px, calc(100vw - 32px))"
    >
      <el-form ref="relationFormRef" :model="relationForm" :rules="relationRules" label-width="100px">
        <el-form-item :label="t('model.relation.direction')">
          <el-radio-group v-model="relationForm.direction">
            <el-radio value="outgoing">
              {{ entity.name }} {{ t('model.relation.direction_outgoing', { name: '' }) }}
            </el-radio>
            <el-radio value="incoming">
              {{ t('model.relation.direction_incoming', { name: entity.name }) }}
            </el-radio>
          </el-radio-group>
        </el-form-item>

        <el-form-item :label="t('model.relation.target')" prop="targetEntityId">
          <el-select
            v-model="relationForm.targetEntityId"
            filterable
            :placeholder="t('model.relation.target_placeholder')"
            style="width:100%"
          >
            <el-option
              v-for="ent in otherEntities"
              :key="ent.id"
              :label="`${ent.name} (${ent.code})`"
              :value="ent.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('model.relation.type')" prop="relationType">
          <el-select v-model="relationForm.relationType" style="width:100%">
            <el-option :label="t('model.relation.one_to_one')" value="one_to_one" />
            <el-option :label="t('model.relation.one_to_many')" value="one_to_many" />
            <el-option :label="t('model.relation.many_to_many')" value="many_to_many" />
          </el-select>
        </el-form-item>

        <el-form-item :label="t('model.relation.name')">
          <el-input
            v-model="relationForm.name"
            maxlength="200"
            :placeholder="t('model.relation.name_placeholder')"
          />
        </el-form-item>

        <el-form-item :label="t('model.relation.description')">
          <el-input
            v-model="relationForm.description"
            type="textarea"
            :rows="3"
          />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="relationDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="handleRelationSubmit" :loading="relationSubmitting">
          {{ t('model.common.save') }}
        </el-button>
      </template>
    </el-dialog>
    </template>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onBeforeUnmount, nextTick, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useConsolePageDescriptor } from '@common-ui'
import { ElMessage } from 'element-plus'
import { ArrowLeft, Plus, Refresh, DocumentCopy } from '@element-plus/icons-vue'
import { entityAPI, entityRelationAPI, domainAPI, elementAPI } from '../api/model'
import mermaid from 'mermaid'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { resolveCanonicalTabRouteState } from '@common-ui'
import { navigateModelRoute } from '../utils/moduleNavigation'
import { resolveEntityListRouteState } from '../utils/routeState'
import { initializeMermaidTheme, observeThemeChange } from '../utils/mermaidTheme'
import { getModelErrorMessage } from '../utils/apiError'
import { buildEntityAttributeUpdateRequest, canPerformDraftAction, isEditableDraft, resolvePositiveRouteId } from '../utils/modelDetailState'
import { useUnsavedChanges } from '../composables/useUnsavedChanges'

const { t } = useI18n()
const authStore = useAuthStore()
const can = permission => authStore.hasPermission(permission)

const route = useRoute()
const router = useRouter()
const entityId = computed(() => resolvePositiveRouteId(route.params.id))

const ENTITY_TABS = ['basic', 'attributes', 'relations']
const resolveRouteState = routeQuery => {
  const listRouteState = resolveEntityListRouteState(routeQuery)
  return resolveCanonicalTabRouteState({
    allowedTabs: ENTITY_TABS,
    defaultTab: 'basic',
    routeQuery,
    preservedQuery: listRouteState.query
  })
}
const activeTab = ref(resolveRouteState(route.query).tab)
let routeDataReady = false
const saving = ref(false)
const pageLoading = ref(false)
const pageError = ref('')
const referenceError = ref('')
const attrLoading = ref(false)
const relationLoading = ref(false)
const mermaidLoading = ref(false)
const attrSubmitting = ref(false)
const relationSubmitting = ref(false)
const attrDialogVisible = ref(false)
const relationDialogVisible = ref(false)
const editingAttr = ref(null)
const editingRelation = ref(null)
const attrFormRef = ref(null)
const relationFormRef = ref(null)
const mermaidContainer = ref(null)

const entity = ref({})
useConsolePageDescriptor(router, 'modeling', {
  title: computed(() => t('model.entity.recentVisitTitle')),
  subject: computed(() => entity.value?.name || ''),
  ready: computed(() => Boolean(entity.value?.name))
})
const entityIsDraft = computed(() => entity.value.status === 'draft')
const canEditEntity = computed(() => isEditableDraft(entity.value.status, can('model.entity.update')))
const canCreateAttribute = computed(() => canPerformDraftAction(entity.value.status, can('model.entity.create')))
const canDeleteAttribute = computed(() => canPerformDraftAction(entity.value.status, can('model.entity.delete')))
const canCreateRelation = computed(() => entityIsDraft.value && can('model.entity_relation.create'))
const form = reactive({ name: '', domain_id: null, description: '' })
const attributes = ref([])
const relations = ref([])
const domains = ref([])
const elements = ref([])
const allEntities = ref([])
const localMermaidCode = ref('erDiagram\n  ENTITY {\n  }\n')
let stopThemeObserver = null

const attributeDataTypes = ['string', 'int', 'bigint', 'float', 'decimal', 'date', 'datetime', 'bool', 'json', 'text', 'geometry']
const attrForm = reactive({ name: '', column_name: '', data_type: 'string', element_id: null, is_pk: false, nullable: true, description: '', sort_order: 0 })
const attrRules = {
  name: [{ required: true, message: t('model.attribute.name_required'), trigger: 'blur' }],
  column_name: [{ required: true, message: t('model.attribute.column_name_required'), trigger: 'blur' }],
  data_type: [{ required: true, message: t('model.attribute.data_type_required'), trigger: 'change' }]
}

const relationForm = reactive({
  direction: 'outgoing',
  targetEntityId: null,
  relationType: 'one_to_many',
  name: '',
  description: ''
})
const relationRules = {
  targetEntityId: [{ required: true, message: t('model.relation.target_required'), trigger: 'change' }],
  relationType: [{ required: true, message: t('model.relation.type_required'), trigger: 'change' }]
}

const unsavedState = computed(() => ({
  form: { ...form },
  attribute_draft: attrDialogVisible.value ? { ...attrForm } : null,
  relation_draft: relationDialogVisible.value ? { ...relationForm } : null
}))
const { isDirty, markSaved, confirmDiscardChanges } = useUnsavedChanges({ state: unsavedState, t })

// 排除当前实体的其他实体列表
const otherEntities = computed(() => allEntities.value.filter(e => e.id !== entityId.value && e.status === 'draft'))
const canModifyRelation = (relation, permission) => {
  if (!entityIsDraft.value || !can(permission)) return false
  const otherEntity = allEntities.value.find(item => item.id === getTargetEntityId(relation))
  return otherEntity?.status === 'draft'
}

const getElementName = (id) => elements.value.find(e => e.id === id)?.name
const getEntityName = (id) => allEntities.value.find(e => e.id === id)?.name || `Entity#${id}`

const getRelationTypeTag = (type) => {
  const map = { one_to_one: 'success', one_to_many: 'primary', many_to_many: 'warning' }
  return map[type] || 'info'
}

const formatRelationType = (type) => {
  const map = {
    one_to_one: t('model.relation.one_to_one'),
    one_to_many: t('model.relation.one_to_many'),
    many_to_many: t('model.relation.many_to_many')
  }
  return map[type] || type
}

const getTargetEntityId = (relation) => {
  return relation.source_entity === entityId.value ? relation.target_entity : relation.source_entity
}

const getTargetEntityName = (relation) => {
  const targetId = getTargetEntityId(relation)
  return getEntityName(targetId)
}

const navigateToEntity = (id) => {
  navigateModelRoute(router, {
    path: `/entities/${id}`,
    query: resolveRouteState(route.query).query
  })
}

const backToList = () => navigateModelRoute(router, {
  path: '/entities',
  query: resolveEntityListRouteState(route.query).query
}, { history: 'replace' })

const handleTabChange = async (tab) => {
  const routeState = resolveRouteState({ ...route.query, tab })
  const location = { path: route.path, query: routeState.query }
  if (router.resolve(location).fullPath !== route.fullPath) {
    await navigateModelRoute(router, location, { history: 'replace' })
  }
}

const applyEntity = resource => {
  entity.value = resource || {}
  Object.assign(form, {
    name: entity.value.name,
    domain_id: entity.value.domain_id,
    description: entity.value.description || ''
  })
}

const loadEntity = async () => applyEntity(await entityAPI.get(entityId.value))

const loadAttributes = async () => {
  attrLoading.value = true
  try {
    const res = await entityAPI.getAttributes(entityId.value)
    attributes.value = res || []
  } finally {
    attrLoading.value = false
  }
}

const loadRelations = async () => {
  relationLoading.value = true
  try {
    const res = await entityRelationAPI.getByEntityId(entityId.value)
    relations.value = res || []
    await refreshLocalDiagram()
  } finally {
    relationLoading.value = false
  }
}

const handleRefresh = async () => {
  if (await confirmDiscardChanges()) await loadPage()
}

const handleSave = async () => {
  if (!canEditEntity.value) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  saving.value = true
  try {
    const updated = await entityAPI.update(entityId.value, { ...form, version: entity.value.version })
    applyEntity(updated)
    markSaved()
    ElMessage.success(t('model.common.save_success'))
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.save_failed'))
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  if (!can('model.entity.approve')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  if (isDirty.value) {
    ElMessage.warning(t('model.common.save_before_action'))
    return
  }
  try {
    const updated = await entityAPI.approve(entityId.value, entity.value.version)
    applyEntity(updated)
    markSaved()
    ElMessage.success(t('model.entity.approve_success'))
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.entity.approve_failed'))
  }
}

const handleReopen = async () => {
  if (!can('model.entity.update')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    const updated = await entityAPI.reopen(entityId.value, entity.value.version)
    applyEntity(updated)
    markSaved()
    ElMessage.success(t('model.common.reopen_success'))
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed'))
  }
}

const openAttrDialog = (attr = null) => {
  editingAttr.value = attr
  if (attr) {
    Object.assign(attrForm, {
      name: attr.name,
      column_name: attr.column_name,
      data_type: attr.data_type,
      element_id: attr.element_id ?? null,
      is_pk: attr.is_pk,
      nullable: attr.nullable,
      description: attr.description || '',
      sort_order: attr.sort_order ?? 0
    })
  } else {
    Object.assign(attrForm, { name: '', column_name: '', data_type: 'string', element_id: null, is_pk: false, nullable: true, description: '', sort_order: 0 })
  }
  attrDialogVisible.value = true
}

const handleAttrSubmit = async () => {
  const requiredPermission = editingAttr.value ? 'model.entity.update' : 'model.entity.create'
  if (!canPerformDraftAction(entity.value.status, can(requiredPermission))) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    await attrFormRef.value.validate()
  } catch {
    return
  }
  attrSubmitting.value = true
  try {
    if (editingAttr.value) {
      const result = await entityAPI.updateAttribute(
        entityId.value,
        editingAttr.value.id,
        buildEntityAttributeUpdateRequest(attrForm, entity.value.version)
      )
      entity.value.version = result.version
      ElMessage.success(t('model.common.update_success'))
    } else {
      const result = await entityAPI.createAttribute(entityId.value, { ...attrForm, version: entity.value.version })
      entity.value.version = result.version
      ElMessage.success(t('model.common.add_success'))
    }
    attrDialogVisible.value = false
    loadAttributes()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed'))
  } finally {
    attrSubmitting.value = false
  }
}

const deleteAttr = async (attrId) => {
  if (!canDeleteAttribute.value) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    const result = await entityAPI.deleteAttribute(entityId.value, attrId, entity.value.version)
    entity.value.version = result.version
    ElMessage.success(t('model.common.delete_success'))
    loadAttributes()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.delete_failed'))
  }
}

const openRelationDialog = (relation = null) => {
  editingRelation.value = relation
  if (relation) {
    Object.assign(relationForm, {
      direction: relation.source_entity === entityId.value ? 'outgoing' : 'incoming',
      targetEntityId: getTargetEntityId(relation),
      relationType: relation.relation_type,
      name: relation.name || '',
      description: relation.description || ''
    })
  } else {
    Object.assign(relationForm, {
      direction: 'outgoing',
      targetEntityId: null,
      relationType: 'one_to_many',
      name: '',
      description: ''
    })
  }
  relationDialogVisible.value = true
}

const handleRelationSubmit = async () => {
  const requiredPermission = editingRelation.value
    ? 'model.entity_relation.update'
    : 'model.entity_relation.create'
  const relationCanBeModified = editingRelation.value
    ? canModifyRelation(editingRelation.value, requiredPermission)
    : entityIsDraft.value && can(requiredPermission)
  if (!relationCanBeModified) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    await relationFormRef.value.validate()
  } catch {
    return
  }
  relationSubmitting.value = true
  try {
    const payload = {
      source_entity: relationForm.direction === 'outgoing' ? entityId.value : relationForm.targetEntityId,
      target_entity: relationForm.direction === 'outgoing' ? relationForm.targetEntityId : entityId.value,
      relation_type: relationForm.relationType,
      name: relationForm.name,
      description: relationForm.description
    }

    if (editingRelation.value) {
      payload.version = editingRelation.value.version
      await entityRelationAPI.update(editingRelation.value.id, payload)
      ElMessage.success(t('model.relation.updated'))
    } else {
      await entityRelationAPI.create(payload)
      ElMessage.success(t('model.relation.added'))
    }

    relationDialogVisible.value = false
    loadRelations()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.op_failed'))
  } finally {
    relationSubmitting.value = false
  }
}

const deleteRelation = async (relation) => {
  if (!canModifyRelation(relation, 'model.entity_relation.delete')) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  try {
    await entityRelationAPI.delete(relation.id, relation.version)
    ElMessage.success(t('model.common.delete_success'))
    loadRelations()
  } catch (err) {
    ElMessage.error(getModelErrorMessage(err, t, 'model.common.delete_failed'))
  }
}

// 生成局部ER图的Mermaid代码
const refreshLocalDiagram = async () => {
  if (relations.value.length === 0) {
    localMermaidCode.value = 'erDiagram\n  ' + (entity.value.code || 'ENTITY') + ' {\n  }\n'
    await renderMermaid()
    return
  }

  mermaidLoading.value = true
  try {
    // 获取相关实体的ID
    const relatedEntityIds = new Set()
    relations.value.forEach(rel => {
      if (rel.source_entity === entityId.value) {
        relatedEntityIds.add(rel.target_entity)
      } else {
        relatedEntityIds.add(rel.source_entity)
      }
    })

    // 查询相关实体的完整信息（包括属性）
    const relatedEntitiesPromises = Array.from(relatedEntityIds).map(id =>
      Promise.all([
        entityAPI.get(id),
        entityAPI.getAttributes(id)
      ]).then(([entityRes, attrsRes]) => ({
        ...entityRes,
        attributes: attrsRes || []
      }))
    )

    const relatedEntities = await Promise.all(relatedEntitiesPromises)

    // 生成Mermaid代码
    let code = 'erDiagram\n'

    // 当前实体定义
    code += generateEntityDefinition(entity.value, attributes.value)

    // 相关实体定义
    relatedEntities.forEach(ent => {
      code += generateEntityDefinition(ent, ent.attributes)
    })

    // 关系定义
    relations.value.forEach(rel => {
      const sourceEntity = rel.source_entity === entityId.value
        ? entity.value
        : relatedEntities.find(e => e.id === rel.source_entity)
      const targetEntity = rel.target_entity === entityId.value
        ? entity.value
        : relatedEntities.find(e => e.id === rel.target_entity)

      if (sourceEntity && targetEntity) {
        const symbol = convertToMermaidSymbol(rel.relation_type)
        code += `  ${sourceEntity.code} ${symbol} ${targetEntity.code} : "${rel.name || 'relates'}"\n`
      }
    })

    localMermaidCode.value = code
    await renderMermaid()
  } catch (err) {
    console.error('生成ER图失败:', err)
    ElMessage.error(t('model.er_diagram.generate_failed'))
  } finally {
    mermaidLoading.value = false
  }
}

// 生成单个实体的Mermaid定义
const generateEntityDefinition = (ent, attrs) => {
  let code = `  ${ent.code} {\n`
  attrs.forEach(attr => {
    const type = attr.data_type
    const pk = attr.is_pk ? ' PK' : ''
    code += `    ${type} ${attr.column_name}${pk}\n`
  })
  code += `  }\n`
  return code
}

// 转换关系类型为Mermaid符号
const convertToMermaidSymbol = (relationType) => {
  const map = {
    one_to_one: '||--||',
    one_to_many: '||--o{',
    many_to_many: '}o--o{'
  }
  return map[relationType] || '||--o{'
}

// 渲染Mermaid图
const renderMermaid = async () => {
  await nextTick()

  // 验证Mermaid代码
  if (!localMermaidCode.value || !localMermaidCode.value.trim()) {
    console.warn('Mermaid代码为空，跳过渲染')
    return
  }

  if (mermaidContainer.value) {
    const mermaidEl = mermaidContainer.value.querySelector('.mermaid')
    if (mermaidEl) {
      // 关键修复：恢复原始Mermaid代码文本，清除之前的渲染结果
      mermaidEl.removeAttribute('data-processed')
      mermaidEl.textContent = localMermaidCode.value

      try {
        await mermaid.run({ nodes: [mermaidEl] })
      } catch (err) {
        console.error('Mermaid渲染错误:', err)
        ElMessage.error(t('model.er_diagram.render_failed'))
      }
    }
  }
}

// 复制Mermaid代码到剪贴板
const copyMermaidCode = async () => {
  try {
    await navigator.clipboard.writeText(localMermaidCode.value)
    ElMessage.success(t('model.er_diagram.copy_mermaid_success'))
  } catch {
    ElMessage.error(t('model.common.copy_failed'))
  }
}

async function restoreTabFromRoute() {
  const routeState = resolveRouteState(route.query)
  activeTab.value = routeState.tab
  if (routeState.changed) {
    await navigateModelRoute(router, {
      path: route.path,
      query: routeState.query
    }, { history: 'replace' })
  }
  if (routeDataReady && activeTab.value === 'relations' && relations.value.length === 0) {
    loadRelations()
  }
}

watch(() => route.query, restoreTabFromRoute)

let loadGeneration = 0
const loadPage = async () => {
  const generation = ++loadGeneration
  pageLoading.value = true
  pageError.value = ''
  referenceError.value = ''
  routeDataReady = false
  attrDialogVisible.value = false
  relationDialogVisible.value = false
  editingAttr.value = null
  editingRelation.value = null
  entity.value = {}
  attributes.value = []
  relations.value = []
  if (!entityId.value) {
    pageLoading.value = false
    pageError.value = t('model.common.invalid_detail_id')
    return
  }
  try {
    await loadEntity()
    if (generation !== loadGeneration) return
    await loadAttributes()
    if (generation !== loadGeneration) return
    const [domainsResult, elementsResult, entitiesResult] = await Promise.allSettled([
      domainAPI.list(), elementAPI.listAll(), entityAPI.listAll()
    ])
    if (generation !== loadGeneration) return
    domains.value = domainsResult.status === 'fulfilled' ? domainsResult.value || [] : []
    elements.value = elementsResult.status === 'fulfilled' ? elementsResult.value || [] : []
    allEntities.value = entitiesResult.status === 'fulfilled' ? entitiesResult.value || [] : []
    if ([domainsResult, elementsResult, entitiesResult].some(result => result.status === 'rejected')) {
      referenceError.value = t('model.common.reference_data_unavailable')
    }
    routeDataReady = true
    markSaved()
  } catch (error) {
    if (generation === loadGeneration) pageError.value = getModelErrorMessage(error, t, 'model.common.load_failed')
  } finally {
    if (generation === loadGeneration) {
      pageLoading.value = false
      if (!pageError.value && activeTab.value === 'relations') {
        nextTick(() => loadRelations())
      }
    }
  }
}

watch(() => route.params.id, loadPage)

onMounted(async () => {
  await restoreTabFromRoute()

  initializeMermaidTheme(mermaid)
  stopThemeObserver = observeThemeChange(async () => {
    initializeMermaidTheme(mermaid)
    if (activeTab.value === 'relations') await renderMermaid()
  })

  await loadPage()

})

onBeforeUnmount(() => stopThemeObserver?.())
</script>

<style scoped>
.entity-detail {
  padding: 20px;
}

.detail-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 16px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.header-right {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.reference-warning {
  margin-bottom: 16px;
}

@media (max-width: 767px) {
  .entity-detail {
    padding: 12px;
  }

  .header-left,
  .header-right {
    width: 100%;
  }

  .header-right :deep(.el-button) {
    flex: 1 1 auto;
    margin-left: 0;
  }

  .diagram-toolbar {
    align-items: flex-start;
    flex-direction: column;
  }
}

.entity-name {
  font-size: 18px;
  font-weight: 600;
}

.tab-header {
  margin-bottom: 16px;
  display: flex;
  justify-content: flex-start;
}

.relations-management {
  margin-bottom: 20px;
}

.local-er-diagram {
  margin-top: 20px;
}

.diagram-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 15px;
}

.diagram-toolbar h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}

.diagram-toolbar > div {
  display: flex;
  gap: 8px;
}

.mermaid-viewer {
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  padding: 20px;
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
  min-height: 400px;
  overflow: auto;
}

.mermaid-viewer .mermaid {
  background: transparent;
}

.revision-tag {
  margin-left: 6px;
}
</style>
