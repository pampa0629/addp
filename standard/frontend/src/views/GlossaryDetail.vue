<template>
  <div class="glossary-detail" v-loading="loading">
    <div class="page-header">
      <div class="header-left">
        <el-button :icon="ArrowLeft" @click="goBack">{{ $t('standard.common.back') }}</el-button>
        <h2>{{ $t('standard.glossary.detailTitle') }}</h2>
        <el-tag :type="statusType(glossary.status)" size="small" v-if="glossary.status">
          {{ statusLabel(glossary.status) }}
        </el-tag>
        <el-tag v-if="isDirty" type="warning" size="small">{{ $t('standard.common.unsaved') }}</el-tag>
      </div>
      <div class="header-right">
        <el-button v-if="canUpdate" type="primary" @click="saveChanges" :loading="saving">{{ $t('standard.common.save') }}</el-button>
        <el-button v-if="canApprove && glossary.status === 'draft'" type="success" @click="handleApprove" :loading="isActionLocked(actionKey)" :disabled="saving">{{ $t('standard.common.approve') }}</el-button>
        <el-button v-if="canOffline && glossary.status === 'approved'" type="warning" @click="handleDeprecate" :loading="isActionLocked(actionKey)" :disabled="saving">{{ $t('standard.common.deprecate') }}</el-button>
      </div>
    </div>

    <el-row :gutter="20">
      <el-col :span="16">
        <!-- 基本信息 -->
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.glossary.basicInfo') }}</h3></template>
          <el-form :model="glossary" label-width="100px" size="default" :disabled="!canUpdate">
            <el-form-item :label="$t('standard.glossary.nameLabel')">
              <el-input v-model="glossary.name" :placeholder="$t('standard.glossary.namePlaceholder')" />
            </el-form-item>
            <el-form-item :label="$t('standard.glossary.aliasLabel')">
              <el-select
                v-model="glossary.alias"
                multiple
                filterable
                allow-create
                default-first-option
                :placeholder="$t('standard.glossary.aliasPlaceholder')"
                style="width: 100%"
              />
            </el-form-item>
            <el-form-item :label="$t('standard.glossary.domainLabel')">
              <el-select v-model="glossary.domain_id" clearable filterable style="width: 100%">
                <el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" />
              </el-select>
            </el-form-item>
            <el-form-item :label="$t('standard.glossary.definitionLabel')">
              <el-input v-model="glossary.definition" type="textarea" :rows="4" :placeholder="$t('standard.glossary.definitionPlaceholder')" />
            </el-form-item>
            <el-form-item :label="$t('standard.glossary.exampleLabel')">
              <el-input v-model="glossary.example" type="textarea" :rows="2" />
            </el-form-item>
            <el-form-item :label="$t('standard.glossary.noteLabel')">
              <el-input v-model="glossary.note" type="textarea" :rows="2" />
            </el-form-item>
            <el-form-item :label="$t('standard.common.tags')">
              <el-select
                v-model="glossary.tags"
                multiple
                filterable
                allow-create
                default-first-option
                :placeholder="$t('standard.common.tagsPlaceholder')"
                style="width: 100%"
              />
            </el-form-item>
          </el-form>
        </el-card>

        <!-- 关联的数据元 -->
        <el-card class="section-card">
          <template #header>
            <div class="card-header">
              <h3>{{ $t('standard.glossary.relatedElements') }}</h3>
              <el-button v-if="canUpdate" size="small" @click="openAddElementDialog">{{ $t('standard.glossary.addElement') }}</el-button>
            </div>
          </template>
          <div v-if="mappedElements.length === 0" class="empty-tip">
            <el-empty :description="$t('standard.glossary.noElements')" />
          </div>
          <el-table v-else :data="mappedElements" size="small">
            <el-table-column :label="$t('standard.common.name')" prop="name" min-width="120" />
            <el-table-column :label="$t('standard.common.code')" prop="code" width="150" />
            <el-table-column :label="$t('standard.element.dataTypeLabel')" prop="data_type" width="110">
              <template #default="{ row }">
                <el-tag size="small" type="info">{{ row.data_type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.common.status')" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column :label="$t('standard.element.definitionLabel')" prop="definition" show-overflow-tooltip />
            <el-table-column v-if="canUpdate" :label="$t('standard.common.actions')" width="80" align="center" fixed="right">
              <template #default="{ row }">
                <el-button link type="danger" @click="removeElement(row.id)">{{ $t('standard.common.remove') }}</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>

        <!-- 关联文档 -->
        <DocumentPanel v-if="glossary.id" entity-type="glossary" :entity-id="glossary.id" v-model:entity-version="glossary.version" />
      </el-col>

      <el-col :span="8">
        <!-- 元数据 -->
        <el-card class="section-card">
          <template #header><h3>{{ $t('standard.common.metadata') }}</h3></template>
          <el-descriptions :column="1" size="small">
            <el-descriptions-item :label="$t('standard.common.id')">{{ glossary.id }}</el-descriptions-item>
            <el-descriptions-item :label="$t('standard.common.createdAt')">{{ formatTime(glossary.created_at) }}</el-descriptions-item>
            <el-descriptions-item :label="$t('standard.common.updatedAt')">{{ formatTime(glossary.updated_at) }}</el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>
    </el-row>

    <!-- 添加数据元对话框 -->
    <el-dialog v-model="addElementDialogVisible" :title="$t('standard.glossary.addElement')" width="560px" @open="onAddDialogOpen">
      <div class="add-element-tip">{{ $t('standard.glossary.addElementTip') }}</div>
      <el-select
        v-model="selectedElementIds"
        multiple
        filterable
        remote
        :remote-method="searchElements"
        :loading="elementSearchLoading"
        :placeholder="$t('standard.glossary.searchElementPlaceholder')"
        style="width: 100%; margin-top: 12px"
        size="large"
      >
        <el-option
          v-for="el in searchedElements"
          :key="el.id"
          :label="`${el.name}（${el.code}）`"
          :value="el.id"
          :disabled="isAlreadyMapped(el.id)"
        />
      </el-select>
      <template #footer>
        <el-button @click="addElementDialogVisible = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" @click="confirmAddElements" :disabled="selectedElementIds.length === 0">{{ $t('standard.glossary.confirmAdd') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, computed, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useConsolePageDescriptor } from '@common-ui'
import { ArrowLeft } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { domainAPI, glossaryAPI, elementAPI } from '../api/standard'
import DocumentPanel from '../components/DocumentPanel.vue'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { formatStandardDateTime } from '../utils/dateTime'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { useActionLock } from '../composables/useActionLock'
import { useUnsavedChanges } from '../composables/useUnsavedChanges'

const { t, locale } = useI18n()
const { canUpdate, canApprove, canOffline } = useStandardPermissions('glossary')
const { isLocked: isActionLocked, runLocked } = useActionLock()
const router = useRouter()
const route = useRoute()
const loading = ref(false)
const saving = ref(false)
const actionKey = computed(() => `glossary:${route.params.id}`)
const glossary = ref({})
useConsolePageDescriptor(router, 'standard', {
  title: computed(() => t('standard.glossary.recentVisitTitle')),
  subject: computed(() => glossary.value?.name || ''),
  ready: computed(() => Boolean(glossary.value?.name))
})
const domains = ref([])
const mappedElements = ref([])
const addElementDialogVisible = ref(false)
const elementSearchLoading = ref(false)
const searchedElements = ref([])
const selectedElementIds = ref([])
const editableState = computed(() => ({
  name: glossary.value.name || '',
  alias: glossary.value.alias || [],
  domain_id: glossary.value.domain_id || null,
  definition: glossary.value.definition || '',
  example: glossary.value.example || '',
  note: glossary.value.note || '',
  tags: glossary.value.tags || [],
  element_ids: mappedElements.value.map(element => element.id).sort((a, b) => a - b)
}))
const { isDirty, markSaved } = useUnsavedChanges({ state: editableState, t })

const statusType = (s) => ({ draft: 'info', approved: 'success', deprecated: 'warning' }[s] || 'info')
const statusLabel = (s) => ({ draft: t('standard.common.draft'), approved: t('standard.common.approved'), deprecated: t('standard.common.deprecated') }[s] || s)

const formatTime = (time) => {
  return formatStandardDateTime(time, locale.value)
}

const goBack = () => {
  const query = {}
  for (const key of ['keyword', 'domain_id', 'status', 'page', 'page_size']) {
    if (route.query[key] !== undefined) query[key] = route.query[key]
  }
  return navigateStandardRoute(router, { path: '/glossaries', query }, { history: 'replace' })
}

const flattenDomains = (nodes) => {
  const result = []
  const traverse = (list) => {
    for (const n of list) {
      result.push(n)
      if (n.children) traverse(n.children)
    }
  }
  traverse(nodes)
  return result
}

const loadGlossary = async () => {
  loading.value = true
  try {
    const res = await glossaryAPI.get(route.params.id)
    glossary.value = res || {}
    if (!glossary.value.alias) glossary.value.alias = []
    if (!glossary.value.tags) glossary.value.tags = []
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.loadFailed'))
    goBack()
  } finally {
    loading.value = false
  }
}

const loadMappedElements = async () => {
  try {
    const res = await glossaryAPI.getElements(route.params.id)
    mappedElements.value = res || []
  } catch (e) {
    mappedElements.value = []
  }
}

const loadDomains = async () => {
  try {
    const res = await domainAPI.list()
    domains.value = flattenDomains(res || [])
  } catch (e) {
    domains.value = []
  }
}

const saveChanges = async () => {
  if (saving.value) return
  saving.value = true
  try {
    const elementIds = mappedElements.value.map(e => e.id)
    await glossaryAPI.update(route.params.id, { ...glossary.value, element_ids: elementIds })
    ElMessage.success(t('standard.common.saveSuccess'))
    await loadGlossary()
    await loadMappedElements()
    markSaved()
  } catch (e) {
    ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

const handleApprove = async () => {
  if (isDirty.value) {
    ElMessage.warning(t('standard.common.saveBeforeAction'))
    return
  }
  await runLocked(actionKey.value, async () => {
    try {
      await ElMessageBox.confirm(t('standard.glossary.confirmApprove'), t('standard.common.hint'), { type: 'info' })
      await glossaryAPI.approve(route.params.id, glossary.value.version)
      ElMessage.success(t('standard.common.approveSuccess'))
      await loadGlossary()
    } catch (e) {
      if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t, 'standard.common.approveFailed'))
    }
  })
}

const handleDeprecate = async () => {
  if (isDirty.value) {
    ElMessage.warning(t('standard.common.saveBeforeAction'))
    return
  }
  await runLocked(actionKey.value, async () => {
    try {
      await ElMessageBox.confirm(t('standard.glossary.confirmDeprecate', { name: glossary.value.name }), t('standard.common.hint'), { type: 'warning' })
      await glossaryAPI.deprecate(route.params.id, glossary.value.version)
      ElMessage.success(t('standard.common.deprecated'))
      await loadGlossary()
    } catch (e) {
      if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e, t))
    }
  })
}

const isAlreadyMapped = (elementId) => {
  return mappedElements.value.some(e => e.id === elementId)
}

const searchElements = async (keyword) => {
  elementSearchLoading.value = true
  try {
    const res = await elementAPI.list({ keyword, page_size: 50 })
    searchedElements.value = res.data || []
  } catch (e) {
    searchedElements.value = []
  } finally {
    elementSearchLoading.value = false
  }
}

const onAddDialogOpen = () => {
  selectedElementIds.value = []
  searchedElements.value = []
  searchElements('')
}

const confirmAddElements = () => {
  const toAdd = searchedElements.value.filter(
    el => selectedElementIds.value.includes(el.id) && !isAlreadyMapped(el.id)
  )
  mappedElements.value = [...mappedElements.value, ...toAdd]
  addElementDialogVisible.value = false
  selectedElementIds.value = []
}

const openAddElementDialog = () => {
  addElementDialogVisible.value = true
}

const removeElement = (elementId) => {
  mappedElements.value = mappedElements.value.filter(e => e.id !== elementId)
}

watch(() => route.params.id, async () => {
  await Promise.all([loadGlossary(), loadMappedElements()])
  markSaved()
}, { immediate: true })

onMounted(loadDomains)
</script>

<style scoped>
.glossary-detail {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

.header-left h2 {
  margin: 0;
  font-size: 18px;
  color: var(--el-text-color-primary);
}

.section-card {
  margin-bottom: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header h3 {
  margin: 0;
  font-size: 15px;
  color: var(--el-text-color-primary);
}

.empty-tip {
  padding: 20px 0;
}

.add-element-tip {
  font-size: 13px;
  color: var(--el-text-color-secondary);
}

@media (max-width: 768px) {
  .glossary-detail { padding: 12px; }
  .page-header { align-items: flex-start; flex-wrap: wrap; gap: 10px; }
  .header-left, .header-right { flex-wrap: wrap; }
  .glossary-detail :deep(.el-row) { margin-left: 0 !important; margin-right: 0 !important; }
  .glossary-detail :deep(.el-col) { max-width: 100%; flex: 0 0 100%; }
  .glossary-detail :deep(.el-col + .el-col) { margin-top: 12px; }
}
</style>
