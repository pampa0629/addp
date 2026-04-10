<template>
  <div class="er-diagram-manager">
    <el-card>
      <template #header>
        <div class="card-header">
          <h2>{{ t('model.er_diagram.title') }}</h2>
          <div class="toolbar">
            <el-button type="primary" @click="showImportDialog">
              <el-icon><Upload /></el-icon> {{ t('model.er_diagram.import_mermaid') }}
            </el-button>
            <el-button @click="exportMermaid">
              <el-icon><Download /></el-icon> {{ t('model.er_diagram.export_mermaid') }}
            </el-button>
            <el-button @click="refreshDiagram">
              <el-icon><Refresh /></el-icon> {{ t('model.er_diagram.refresh') }}
            </el-button>
          </div>
        </div>
      </template>

      <div class="diagram-info">
        <el-alert type="info" :closable="false">
          {{ t('model.er_diagram.entity_count', { count: entities.length, relations: relations.length }) }}
        </el-alert>
      </div>

      <!-- ER图渲染区域 -->
      <div ref="diagramContainer" class="diagram-container" v-loading="diagramLoading">
        <pre class="mermaid">{{ globalMermaidCode }}</pre>
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
          {{ t('model.er_diagram.import_merge') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, onMounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Upload, Download, Refresh, UploadFilled } from '@element-plus/icons-vue'
import mermaid from 'mermaid'
import { entityAPI, entityRelationAPI } from '../api/model'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const entities = ref([])
const relations = ref([])
const globalMermaidCode = ref('')
const diagramContainer = ref(null)
const diagramLoading = ref(false)

const importDialogVisible = ref(false)
const importTab = ref('paste')
const importMermaidCode = ref('')
const importing = ref(false)

// 加载数据并生成ER图
const refreshDiagram = async () => {
  diagramLoading.value = true
  try {
    // 加载所有实体和关系
    const [entitiesRes, relationsRes] = await Promise.all([
      entityAPI.list({ page_size: 1000 }),
      entityRelationAPI.list()
    ])
    entities.value = entitiesRes.data || []
    relations.value = relationsRes.data || []

    // 生成Mermaid代码
    await generateGlobalMermaidCode()

    // 渲染
    await nextTick()
    await renderMermaid()
  } catch (err) {
    console.error('加载ER图失败:', err)
    ElMessage.error(t('model.er_diagram.load_failed'))
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
      const attributes = attrsRes.data || []
      for (const attr of attributes) {
        const type = 'string' // 默认类型
        const pk = attr.is_pk ? ' PK' : ''
        code += `    ${type} ${attr.name}${pk}\n`
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
      mermaid_code: importMermaidCode.value,
      mode: 'merge'
    })

    ElMessage.success(t('model.er_diagram.import_success', {
      created: result.data.created_entities,
      updated: result.data.updated_entities,
      relations: result.data.created_relations
    }))
    importDialogVisible.value = false
    refreshDiagram()
  } catch (error) {
    if (error !== 'cancel') {
      const errorMsg = error.response?.data?.error || error.message || t('model.er_diagram.export_failed')
      ElMessage.error(t('model.er_diagram.import_failed', { msg: errorMsg }))
    }
  } finally {
    importing.value = false
  }
}

onMounted(() => {
  // 初始化Mermaid
  mermaid.initialize({
    startOnLoad: false,
    theme: 'default',
    er: {
      useMaxWidth: true
    }
  })

  refreshDiagram()
})
</script>

<style scoped>
.er-diagram-manager {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-header h2 {
  margin: 0;
  font-size: 18px;
  font-weight: 600;
}

.toolbar {
  display: flex;
  gap: 10px;
}

.diagram-info {
  margin-bottom: 20px;
}

.diagram-container {
  border: 1px solid #dcdfe6;
  border-radius: 4px;
  padding: 30px;
  background: #fafafa;
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
  color: #409eff;
  margin-bottom: 16px;
}
</style>
