<template>
  <div class="er-diagram-manager">
    <el-card>
      <template #header>
        <div class="card-header">
          <div class="header-left">
            <h2>{{ t('model.er_diagram.title') }}</h2>
            <el-select
              v-model="selectedDomainId"
              class="domain-filter"
              :placeholder="t('model.er_diagram.domain_filter')"
              clearable
              @change="handleDomainChange"
            >
              <el-option v-for="domain in domains" :key="domain.id" :label="domain.name" :value="domain.id" />
            </el-select>
          </div>
          <div class="toolbar">
            <el-button v-if="canImport" type="primary" @click="showImportDialog">
              <el-icon><Upload /></el-icon> {{ t('model.er_diagram.import_mermaid') }}
            </el-button>
            <el-button v-if="canExport" @click="exportMermaid">
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

      <div v-else class="diagram-info">
        <el-alert type="info" :closable="false">
          {{ t('model.er_diagram.entity_count', { count: entities.length, relations: relations.length }) }}
        </el-alert>
      </div>

      <!-- ER图渲染区域 -->
      <div v-if="!loadError" ref="diagramContainer" class="diagram-container" v-loading="diagramLoading">
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
      :title="t('model.er_diagram.import_dialog_title')"
      width="800px"
    >
      <el-tabs v-model="importTab">
        <el-tab-pane :label="t('model.er_diagram.paste_code')" name="paste">
          <el-input
            v-model="importMermaidCode"
            type="textarea"
            :rows="20"
            :placeholder="t('model.er_diagram.paste_placeholder')"
          />
          <div class="import-tips">
            <el-alert type="info" :closable="false">
              <template #title>
                <p><strong>{{ t('model.er_diagram.format_example') }}</strong></p>
                <pre style="margin-top: 10px; font-size: 12px;">erDiagram
    CUSTOMER {
        bigint id PK
        string name
        string email
    }
    ORDER {
        bigint id PK
        bigint customer_id FK
    }
    CUSTOMER ||--o{ ORDER : "places"</pre>
              </template>
            </el-alert>
          </div>
        </el-tab-pane>

        <el-tab-pane :label="t('model.er_diagram.upload_file')" name="file">
          <el-upload
            drag
            accept=".md,.mmd,.mermaid,.txt"
            :before-upload="handleFileUpload"
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

      <template #footer>
        <el-button @click="importDialogVisible = false">{{ t('model.common.cancel') }}</el-button>
        <el-button type="primary" @click="executeImport" :loading="importing">
          {{ t('model.er_diagram.import_replace') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Upload, Download, Refresh, UploadFilled } from '@element-plus/icons-vue'
import mermaid from 'mermaid'
import { entityAPI, entityRelationAPI, domainAPI } from '../api/model'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { getModelErrorMessage } from '../utils/apiError'
import { initializeMermaidTheme, observeThemeChange } from '../utils/mermaidTheme'
import { buildERDiagramRouteQuery, resolveERDiagramRouteState } from '../utils/routeState'
import { navigateModelRoute } from '../utils/moduleNavigation'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const hasPermissions = permissions => permissions.every(permission => authStore.hasPermission(permission))
const canImport = computed(() => hasPermissions([
  'model.entity.create',
  'model.entity.delete',
  'model.entity_relation.create',
  'model.entity_relation.delete'
]))
const canExport = computed(() => hasPermissions([
  'model.entity.read',
  'model.entity_relation.read'
]))

const entities = ref([])
const relations = ref([])
const domains = ref([])
const selectedDomainId = ref(null)
const globalMermaidCode = ref('')
const diagramContainer = ref(null)
const diagramLoading = ref(false)
const loadError = ref('')

const importDialogVisible = ref(false)
const importTab = ref('paste')
const importMermaidCode = ref('')
const importing = ref(false)
let stopThemeObserver = null

const applyRouteState = query => {
  const routeState = resolveERDiagramRouteState(query)
  selectedDomainId.value = routeState.domainId
  return routeState
}

const syncRoute = () => navigateModelRoute(router, {
  path: '/er-diagram',
  query: buildERDiagramRouteQuery({ domainId: selectedDomainId.value })
}, { history: 'replace' })

const loadDomains = async () => {
  try {
    domains.value = await domainAPI.list() || []
  } catch (err) {
    loadError.value = getModelErrorMessage(err, t, 'model.common.load_failed')
  }
}

const reload = async () => {
  loadError.value = ''
  await loadDomains()
  if (!loadError.value) await refreshDiagram()
}

// 加载数据并生成ER图
const refreshDiagram = async () => {
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
    const allEntities = entitiesRes || []
    entities.value = selectedDomainId.value
      ? allEntities.filter(entity => entity.domain_id === selectedDomainId.value)
      : allEntities
    const visibleEntityIds = new Set(entities.value.map(entity => entity.id))
    relations.value = (relationsRes || []).filter(relation =>
      visibleEntityIds.has(relation.source_entity) && visibleEntityIds.has(relation.target_entity)
    )

    // 生成Mermaid代码
    await generateGlobalMermaidCode()

    // 渲染
    await nextTick()
    if (entities.value.length > 0) await renderMermaid()
  } catch (err) {
    console.error('加载ER图失败:', err)
    loadError.value = getModelErrorMessage(err, t, 'model.er_diagram.load_failed')
  } finally {
    diagramLoading.value = false
  }
}

// 生成全局Mermaid代码
const generateGlobalMermaidCode = async () => {
  let code = 'erDiagram\n'

  // 所有实体定义
  for (const entity of entities.value) {
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
  relations.value.forEach(relation => {
    const sourceEntity = entities.value.find(e => e.id === relation.source_entity)
    const targetEntity = entities.value.find(e => e.id === relation.target_entity)

    if (sourceEntity && targetEntity) {
      const symbol = convertToMermaidSymbol(relation.relation_type)
      const label = relation.name || 'relates'

      code += `  ${sourceEntity.code} ${symbol} ${targetEntity.code} : "${label}"\n`
    }
  })

  globalMermaidCode.value = code
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
  await syncRoute()
  await refreshDiagram()
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
  try {
    const blob = new Blob([globalMermaidCode.value], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `er-diagram-${new Date().getTime()}.mmd`
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
  importMermaidCode.value = ''
  importTab.value = 'paste'
  importDialogVisible.value = true
}

// 文件上传处理
const handleFileUpload = (file) => {
  const reader = new FileReader()
  reader.onload = (e) => {
    importMermaidCode.value = e.target.result
    importTab.value = 'paste'
  }
  reader.readAsText(file)
  return false // 阻止自动上传
}

// 执行导入
const executeImport = async () => {
  if (!importMermaidCode.value.trim()) {
    ElMessage.warning(t('model.er_diagram.import_empty'))
    return
  }

  try {
    importing.value = true

    await ElMessageBox.confirm(
      t('model.er_diagram.import_confirm_msg'),
      t('model.er_diagram.import_confirm_title'),
      { type: 'warning' }
    )

    // 调用后端API导入
    const result = await entityAPI.importMermaid({
      mermaid_code: importMermaidCode.value
    })

    ElMessage.success(t('model.er_diagram.import_success', {
      created: result.created_entities,
      relations: result.created_relations
    }))
    importDialogVisible.value = false
    refreshDiagram()
  } catch (error) {
    if (error !== 'cancel') {
      const errorMsg = getModelErrorMessage(error, t, 'model.common.op_failed')
      ElMessage.error(t('model.er_diagram.import_failed', { msg: errorMsg }))
    }
  } finally {
    importing.value = false
  }
}

const restoreDiagramFromRoute = async query => {
  const previousDomainId = selectedDomainId.value
  const routeState = applyRouteState(query)
  if (routeState.changed) {
    await navigateModelRoute(router, { path: '/er-diagram', query: routeState.query }, { history: 'replace' })
  }
  if (selectedDomainId.value !== previousDomainId) await refreshDiagram()
}

watch(() => route.query, restoreDiagramFromRoute, { deep: true })

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
</style>
