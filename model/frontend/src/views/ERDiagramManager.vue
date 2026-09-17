<template>
  <div class="er-diagram-manager">
    <el-card>
      <template #header>
        <div class="card-header">
          <div class="header-left">
            <h2>{{ t('model.er_diagram.title') }}</h2>
            <BusinessDomainSelect
              v-model="selectedDomainId"
              class="domain-filter"
              :placeholder="t('model.er_diagram.domain_filter')"
              clearable
              @change="handleDomainChange"
              :options="domains"
            >
              <el-option :label="t('model.er_diagram.all_domains')" value="all" />
            </BusinessDomainSelect>
            <el-checkbox v-if="selectedDomainId && selectedDomainId !== 'all'" v-model="includeRelated" @change="handleDomainChange">{{ t('model.er_diagram.include_related') }}</el-checkbox>
          </div>
          <div class="toolbar">
            <el-button v-if="canImport" type="primary" @click="showImportDialog">
              <el-icon><DocumentAdd /></el-icon> {{ t('model.er_diagram.import_mermaid') }}
            </el-button>
            <el-button v-if="canExport" :disabled="!selectedDomainId" @click="exportMermaid">
              <el-icon><Download /></el-icon> {{ t('model.er_diagram.export_mermaid') }}
            </el-button>
            <el-button @click="refreshDiagram">
              <el-icon><Refresh /></el-icon> {{ t('model.er_diagram.refresh') }}
            </el-button>
          </div>
        </div>
      </template>

      <el-alert
        v-if="loadError"
        class="load-error"
        type="error"
        :title="loadError"
        show-icon
        :closable="false"
      >
        <el-button link type="danger" @click="reload">{{ t('model.common.retry') }}</el-button>
      </el-alert>

      <el-alert
        v-if="!loadError && referenceError"
        class="load-error"
        type="warning"
        :title="referenceError"
        show-icon
        :closable="false"
      />

      <el-alert :title="t('model.er_diagram.transfer_scope')" type="info" :closable="false" show-icon />
      <el-empty v-if="!loadError && !selectedDomainId" :description="t('model.er_diagram.choose_domain')" />
      <div v-if="!loadError && selectedDomainId" class="diagram-info">
        <el-alert type="info" :closable="false">
          {{ t('model.er_diagram.entity_count', { count: entities.length, relations: relations.length }) }}
        </el-alert>
      </div>

      <div v-if="selectedDomainId && !loadError" class="domain-legend">
        <el-tag v-for="entity in entities" :key="entity.id" :type="selectedDomainId !== 'all' && entity.domain_id !== selectedDomainId ? 'warning' : 'info'">
          {{ entity.code }} · {{ domainLabel(entity.domain_id) }}
        </el-tag>
      </div>
      <!-- ER图渲染区域 -->
      <div v-if="!loadError && selectedDomainId" ref="diagramContainer" class="diagram-container" v-loading="diagramLoading">
        <el-empty
          v-if="!diagramLoading && entities.length === 0"
          :description="t('model.er_diagram.no_entities')"
        />
        <pre v-else class="mermaid">{{ globalMermaidCode }}</pre>
      </div>
    </el-card>

    <!-- 导入对话框 -->
    <el-dialog
      v-model="importDialogVisible"
      class="addp-dialog"
      :title="t('model.er_diagram.import_dialog_title')"
      width="min(800px, calc(100vw - 32px))"
    >
      <el-alert :title="t('model.er_diagram.transfer_scope')" type="warning" :closable="false" show-icon />
      <el-tabs v-model="importTab">
        <el-tab-pane :label="t('model.er_diagram.paste_code')" name="paste">
          <el-input
            v-model="importMarkdown"
            type="textarea"
            :rows="20"
            :placeholder="t('model.er_diagram.paste_placeholder')"
          />
          <div class="import-tips">
            <el-alert type="info" :closable="false">
              <template #title>
                <p><strong>{{ t('model.er_diagram.format_example') }}</strong></p>
                <pre style="margin-top: 10px; font-size: 12px;"># ADDP Entity Relationship Diagram

```mermaid
erDiagram
  %% addp:document {"format":"addp.model.er/v2","scope":"domain","domain_code":"sales"}
  %% addp:entity {"code":"customer","name":"客户","domain_code":"sales","description":""}
  customer {
    bigint id PK
  }
```</pre>
              </template>
            </el-alert>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="t('model.er_diagram.upload_file')" name="file">
          <el-upload
            drag
            accept=".md,.markdown"
            :on-change="handleFileChange"
            :auto-upload="false"
            :show-file-list="false"
          >
            <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
            <div class="el-upload__text">
              {{ t('model.er_diagram.drag_upload') }} <em>{{ t('model.er_diagram.click_upload') }}</em>
            </div>
            <template #tip>
              <div class="el-upload__tip">
                {{ t('model.er_diagram.upload_tip') }}
              </div>
            </template>
          </el-upload>
        </el-tab-pane>
      </el-tabs>

      <el-alert
        v-if="importPreview"
        class="import-preview"
        :type="importPreview.conflicts.length ? 'error' : 'success'"
        :closable="false"
        show-icon
        :title="t('model.er_diagram.preview_summary', {
          created: importPreview.created_entities,
          unchanged: importPreview.unchanged_entities,
          relations: importPreview.created_relations,
          unchangedRelations: importPreview.unchanged_relations
        })"
      >
        <div v-if="importPreview.resolved_domains.length" class="resolved-domains">
          <span>{{ t('model.er_diagram.resolved_domains') }}</span>
          <el-tag v-for="domain in importPreview.resolved_domains" :key="domain.code" type="info">
            {{ domain.name }}（{{ domain.code }}）
          </el-tag>
        </div>
        <ul v-if="importPreview.conflicts.length" class="conflict-list">
          <li v-for="conflict in importPreview.conflicts" :key="`${conflict.resource_type}:${conflict.key}`">
            {{ t(`model.er_diagram.conflict_${conflict.reason}`, {
              type: t(`model.er_diagram.resource_${conflict.resource_type}`),
              key: conflict.key
            }) }}
          </li>
        </ul>
      </el-alert>

      <template #footer>
        <el-button @click="importDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button @click="previewImport" :loading="previewing" :disabled="importing">
          {{ t('model.er_diagram.preview_import') }}
        </el-button>
        <el-button type="primary" @click="executeImport" :loading="importing" :disabled="previewing || !canApplyImport">
          {{ t('model.er_diagram.confirm_import') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { BusinessDomainSelect, buildBusinessDomainOptions } from '@common-ui'
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { DocumentAdd, Download, Refresh, UploadFilled } from '@element-plus/icons-vue'
import mermaid from 'mermaid'
import { entityAPI, entityRelationAPI, domainAPI } from '../api/model'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { getModelErrorMessage } from '../utils/apiError'
import { initializeMermaidTheme, observeThemeChange } from '../utils/mermaidTheme'
import { buildERDiagramRouteQuery, resolveERDiagramRouteState } from '../utils/routeState'
import { navigateModelRoute } from '../utils/moduleNavigation'
import { filterERDiagramByDomain } from '../utils/erDiagramState'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const hasPermissions = permissions => permissions.every(permission => authStore.hasPermission(permission))
const canImport = computed(() => hasPermissions([
  'model.entity.create',
  'model.entity_relation.create'
]))
const canExport = computed(() => hasPermissions([
  'model.entity.read',
  'model.entity_relation.read'
]))

const entities = ref([])
const relations = ref([])
const domains = ref([])
const referenceError = ref('')
const selectedDomainId = ref(null)
const includeRelated = ref(false)
const domainLabel = id => domains.value.find(domain => domain.id === id)?.name || t('model.er_diagram.unassigned_domain')
let diagramSequence = 0
const globalMermaidCode = ref('')
const diagramContainer = ref(null)
const diagramLoading = ref(false)
const loadError = ref('')

const importDialogVisible = ref(false)
const importTab = ref('paste')
const importMarkdown = ref('')
const importPreview = ref(null)
const previewedMarkdown = ref('')
const previewing = ref(false)
const importing = ref(false)
const canApplyImport = computed(() => importPreview.value &&
  previewedMarkdown.value === importMarkdown.value && importPreview.value.conflicts.length === 0)
let stopThemeObserver = null

const applyRouteState = query => {
  const routeState = resolveERDiagramRouteState(query)
  selectedDomainId.value = routeState.domainId
  includeRelated.value = routeState.includeRelated
  return routeState
}

const syncRoute = () => navigateModelRoute(router, {
  path: '/er-diagram',
  query: buildERDiagramRouteQuery({ domainId: selectedDomainId.value, includeRelated: includeRelated.value })
}, { history: 'replace' })

const loadDomains = async () => {
  try {
    domains.value = buildBusinessDomainOptions(await domainAPI.list() || [])
  } catch (err) {
    domains.value = []
    referenceError.value = t('model.common.reference_data_unavailable')
  }
}

const reload = async () => {
  loadError.value = ''
  referenceError.value = ''
  await loadDomains()
  await refreshDiagram()
}

// 加载数据并生成ER图
const refreshDiagram = async () => {
  const sequence = ++diagramSequence
  diagramLoading.value = true
  loadError.value = ''
  if (!canExport.value) {
    entities.value = []
    relations.value = []
    loadError.value = t('model.common.permission_denied')
    diagramLoading.value = false
    return
  }
  try {
    // 加载所有实体和关系
    const [entitiesRes, relationsRes] = await Promise.all([
      entityAPI.listAll(),
      entityRelationAPI.list()
    ])
    if (sequence !== diagramSequence) return
    const allEntities = entitiesRes || []
    const filteredDiagram = filterERDiagramByDomain(
      allEntities,
      relationsRes || [],
      selectedDomainId.value, includeRelated.value
    )
    entities.value = filteredDiagram.entities
    relations.value = filteredDiagram.relations

    // 生成Mermaid代码
    const code = await generateGlobalMermaidCode(filteredDiagram)
    if (sequence !== diagramSequence) return
    globalMermaidCode.value = code

    // 渲染
    await nextTick()
    if (entities.value.length > 0) await renderMermaid()
  } catch (err) {
    if (sequence !== diagramSequence) return
    console.error('加载ER图失败:', err)
    loadError.value = getModelErrorMessage(err, t, 'model.er_diagram.load_failed')
  } finally {
    if (sequence === diagramSequence) diagramLoading.value = false
  }
}

// 生成全局Mermaid代码
const generateGlobalMermaidCode = async ({ entities: diagramEntities, relations: diagramRelations }) => {
  let code = 'erDiagram\n'

  // 所有实体定义
  for (const entity of diagramEntities) {
    code += `  ${entity.code} {\n`

    // 查询属性
    try {
      const attrsRes = await entityAPI.getAttributes(entity.id)
      const attributes = attrsRes || []
      for (const attr of attributes) {
        const type = attr.data_type || 'string'
        const pk = attr.is_pk ? ' PK' : ''
        code += `    ${type} ${attr.column_name}${pk}\n`
      }
    } catch (err) {
      console.error(`获取实体${entity.id}的属性失败:`, err)
    }

    code += `  }\n`
  }

  // 所有关系
  diagramRelations.forEach(relation => {
    const sourceEntity = diagramEntities.find(e => e.id === relation.source_entity)
    const targetEntity = diagramEntities.find(e => e.id === relation.target_entity)

    if (sourceEntity && targetEntity) {
      const symbol = convertToMermaidSymbol(relation.relation_type)
      const label = relation.name || 'relates'

      code += `  ${sourceEntity.code} ${symbol} ${targetEntity.code} : "${label}"\n`
    }
  })

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

const handleDomainChange = async () => {
  if (!selectedDomainId.value || selectedDomainId.value === 'all') includeRelated.value = false
  await syncRoute()
}

// 渲染Mermaid图
const renderMermaid = async () => {
  await nextTick()
  if (diagramContainer.value) {
    const mermaidEl = diagramContainer.value.querySelector('.mermaid')
    if (mermaidEl) {
      // 关键修复：恢复原始Mermaid代码文本，清除之前的渲染结果
      mermaidEl.removeAttribute('data-processed')
      mermaidEl.textContent = globalMermaidCode.value

      try {
        await mermaid.run({ nodes: [mermaidEl] })
      } catch (err) {
        console.error('Mermaid渲染错误:', err)
        ElMessage.error(t('model.er_diagram.render_failed'))
      }
    }
  }
}

// 导出Mermaid
const exportMermaid = async () => {
  if (!selectedDomainId.value) return
  try {
    const domainID = selectedDomainId.value === 'all' ? null : selectedDomainId.value
    const snapshot = await entityAPI.exportMermaid(domainID ? { domain_id: domainID } : undefined)
    if (domainID && !snapshot.domain_code) throw new Error('missing domain_code in Mermaid export response')
    const blob = new Blob([snapshot.markdown || ''], { type: 'text/markdown;charset=utf-8' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = domainID ? `er-diagram-${snapshot.domain_code}.md` : 'er-diagram-all.md'
    link.click()
    URL.revokeObjectURL(url)

    ElMessage.success(t('model.er_diagram.export_success'))
  } catch (err) {
    console.error('导出失败:', err)
    ElMessage.error(t('model.er_diagram.export_failed'))
  }
}

// 显示导入对话框
const showImportDialog = () => {
  importMarkdown.value = ''
  importPreview.value = null
  previewedMarkdown.value = ''
  importTab.value = 'paste'
  importDialogVisible.value = true
}

// 文件上传处理
const handleFileChange = (uploadFile) => {
  const file = uploadFile.raw
  if (!file) return

  const reader = new FileReader()
  reader.onload = (e) => {
    importMarkdown.value = e.target.result
    importTab.value = 'paste'
  }
  reader.readAsText(file)
}

const previewImport = async () => {
  if (!canImport.value) {
    ElMessage.error(t('model.common.permission_denied'))
    return
  }
  if (!importMarkdown.value.trim()) {
    ElMessage.warning(t('model.er_diagram.import_empty'))
    return
  }

  try {
    previewing.value = true
    importPreview.value = null
    previewedMarkdown.value = ''
    const markdown = importMarkdown.value
    const result = await entityAPI.previewMermaidImport({ markdown })
    if (importMarkdown.value === markdown) {
      importPreview.value = result
      previewedMarkdown.value = markdown
    }
  } catch (error) {
    const errorMsg = getModelErrorMessage(error, t, 'model.common.op_failed')
    ElMessage.error(t('model.er_diagram.preview_failed', { msg: errorMsg }))
  } finally {
    previewing.value = false
  }
}

// 确认增量导入
const executeImport = async () => {
  if (!canApplyImport.value) return
  try {
    importing.value = true
    const result = await entityAPI.importMermaid({
      markdown: importMarkdown.value,
      revision: importPreview.value.revision
    })

    ElMessage.success(t('model.er_diagram.import_success', {
      created: result.created_entities,
      unchanged: result.unchanged_entities,
      relations: result.created_relations,
      unchangedRelations: result.unchanged_relations
    }))
    importDialogVisible.value = false
    await refreshDiagram()
  } catch (error) {
    const errorMsg = getModelErrorMessage(error, t, 'model.common.op_failed')
    ElMessage.error(t('model.er_diagram.import_failed', { msg: errorMsg }))
  } finally {
    importing.value = false
  }
}

const restoreDiagramFromRoute = async query => {
  const routeState = applyRouteState(query)
  if (routeState.changed) {
    await navigateModelRoute(router, { path: '/er-diagram', query: routeState.query }, { history: 'replace' })
    return
  }
  await refreshDiagram()
}

watch(() => route.query, restoreDiagramFromRoute, { deep: true })
watch(importMarkdown, () => {
  importPreview.value = null
  previewedMarkdown.value = ''
})

onMounted(async () => {
  initializeMermaidTheme(mermaid, {
    er: {
      useMaxWidth: true
    }
  })
  stopThemeObserver = observeThemeChange(async () => {
    initializeMermaidTheme(mermaid, { er: { useMaxWidth: true } })
    if (entities.value.length > 0) await renderMermaid()
  })

  const routeState = applyRouteState(route.query)
  if (routeState.changed) {
    await navigateModelRoute(router, { path: '/er-diagram', query: routeState.query }, { history: 'replace' })
  }
  await reload()
})

onBeforeUnmount(() => stopThemeObserver?.())
</script>

<style scoped>
.domain-legend { display:flex; flex-wrap:wrap; gap:8px; margin:12px 0; }
.import-preview { margin-top: 16px; }
.resolved-domains { display:flex; flex-wrap:wrap; align-items:center; gap:8px; margin-top:8px; }
.conflict-list { margin: 8px 0 0; padding-left: 20px; }
.er-diagram-manager {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 16px;
}

.card-header h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
}

.header-left {
  display: flex;
  align-items: center;
  flex: 1 1 360px;
  flex-wrap: wrap;
  gap: 24px;
  min-width: 0;
}

.domain-filter {
  width: 220px;
}

.toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-left: auto;
}

.load-error {
  margin-bottom: 20px;
}

.diagram-info {
  margin-bottom: 20px;
}

.diagram-container {
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  padding: 30px;
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
  min-height: 500px;
  overflow: auto;
}

.diagram-container .mermaid {
  background: transparent;
}

.import-tips {
  margin-top: 15px;
}

.el-icon--upload {
  font-size: 67px;
  color: var(--el-color-primary);
  margin-bottom: 16px;
}

@media (max-width: 767px) {
  .er-diagram-manager {
    padding: 12px;
  }

  .header-left,
  .domain-filter,
  .toolbar {
    width: 100%;
  }

  .toolbar {
    margin-left: 0;
  }

  .toolbar :deep(.el-button) {
    flex: 1 1 auto;
    margin-left: 0;
  }

  .diagram-container {
    min-height: 360px;
    padding: 16px;
  }
}
</style>
