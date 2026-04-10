<template>
  <div class="orchestration-form">
    <div class="header">
      <h2>{{ isEdit ? t('orchestrator.orchestrationForm.editTitle') : t('orchestrator.orchestrationForm.createTitle') }}</h2>
      <div>
        <el-button @click="handleViewJSON">{{ t('orchestrator.orchestrationForm.viewJsonBtn') }}</el-button>
        <el-button @click="handleCancel">{{ t('orchestrator.orchestrationForm.cancelBtn') }}</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">{{ t('orchestrator.orchestrationForm.saveBtn') }}</el-button>
      </div>
    </div>

    <el-form :model="form" label-width="120px" class="form-metadata">
      <el-form-item :label="t('orchestrator.orchestrationForm.nameLabel')" required>
        <el-input v-model="form.name" :placeholder="t('orchestrator.orchestrationForm.namePlaceholder')"></el-input>
      </el-form-item>

      <el-form-item :label="t('orchestrator.orchestrationForm.descriptionLabel')">
        <el-input
          v-model="form.description"
          type="textarea"
          :rows="2"
          :placeholder="t('orchestrator.orchestrationForm.descriptionPlaceholder')"
        ></el-input>
      </el-form-item>

      <el-form-item :label="t('orchestrator.orchestrationForm.scheduleLabel')">
        <div class="schedule-row">
          <el-switch v-model="form.enabled" style="margin-right: 12px;"></el-switch>
          <span style="margin-right: 12px; color: var(--addp-text-secondary);">{{ t('orchestrator.orchestrationForm.enabledLabel') }}</span>
          <ScheduleConfig
            v-model="form.schedule"
            :allow-custom-cron="true"
          />
        </div>
      </el-form-item>
    </el-form>

    <!-- 三栏布局 -->
    <div class="three-column-layout">
      <!-- 左侧任务库 -->
      <TaskPanel class="left-panel" />

      <!-- 中央 DAG 画布 -->
      <div class="center-panel">
        <DAGEditor
          ref="dagEditor"
          :initial-steps="form.steps"
          @update:steps="handleStepsUpdate"
        />
      </div>
    </div>

    <!-- JSON 预览对话框 -->
    <el-dialog
      v-model="jsonDialogVisible"
      :title="t('orchestrator.orchestrationForm.jsonDialogTitle')"
      width="60%"
      :close-on-click-modal="false"
    >
      <div class="json-preview">
        <div class="json-actions">
          <el-button size="small" @click="copyJSON">{{ t('orchestrator.orchestrationForm.copyJsonBtn') }}</el-button>
          <el-button size="small" @click="downloadJSON">{{ t('orchestrator.orchestrationForm.downloadJsonBtn') }}</el-button>
        </div>
        <pre class="json-content">{{ formattedJSON }}</pre>
      </div>
    </el-dialog>

  </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import DAGEditor from '../components/DAGEditor.vue'
import TaskPanel from '../components/TaskPanel.vue'
import orchestrationAPI from '../api/orchestration'
import { ScheduleConfig } from '@common-ui'

const { t } = useI18n()
const router = useRouter()
const route = useRoute()
const dagEditor = ref(null)

const isEdit = ref(false)
const saving = ref(false)
const jsonDialogVisible = ref(false)

const form = reactive({
  name: '',
  description: '',
  enabled: false,
  schedule: '',
  steps: []
})

// 格式化 JSON 用于展示
const formattedJSON = computed(() => {
  return JSON.stringify(form, null, 2)
})

onMounted(async () => {
  const id = route.params.id
  if (id && id !== 'new') {
    isEdit.value = true
    await loadOrchestration(id)
  }
})

// 监听 form.steps 变化,同步到 DAGEditor (修复问题4)
watch(() => form.steps, (newSteps) => {
  if (newSteps && newSteps.length > 0 && dagEditor.value) {
    // 等待下一个 tick 确保 DAGEditor 已经初始化
    setTimeout(() => {
      dagEditor.value.loadSteps(newSteps)
    }, 100)
  }
}, { deep: true })

async function loadOrchestration(id) {
  try {
    const data = await orchestrationAPI.get(id)
    Object.assign(form, data)
  } catch (error) {
    ElMessage.error(t('orchestrator.orchestrationForm.loadFailed'))
  }
}

function handleStepsUpdate(steps) {
  form.steps = steps
}

async function handleSave() {
  if (!form.name) {
    ElMessage.warning(t('orchestrator.orchestrationForm.nameRequired'))
    return
  }

  if (!form.steps || form.steps.length === 0) {
    ElMessage.warning(t('orchestrator.orchestrationForm.stepsRequired'))
    return
  }

  saving.value = true
  try {
    if (isEdit.value) {
      await orchestrationAPI.update(route.params.id, form)
      ElMessage.success(t('orchestrator.orchestrationForm.updateSuccess'))
    } else {
      await orchestrationAPI.create(form)
      ElMessage.success(t('orchestrator.orchestrationForm.createSuccess'))
    }
    router.push('/orchestrations')
  } catch (error) {
    ElMessage.error(isEdit.value ? t('orchestrator.orchestrationForm.updateFailed') : t('orchestrator.orchestrationForm.createFailed'))
  } finally {
    saving.value = false
  }
}

function handleCancel() {
  router.back()
}

function handleViewJSON() {
  jsonDialogVisible.value = true
}

async function copyJSON() {
  try {
    await navigator.clipboard.writeText(formattedJSON.value)
    ElMessage.success(t('orchestrator.orchestrationForm.jsonCopied'))
  } catch (error) {
    ElMessage.error(t('orchestrator.orchestrationForm.copyFailed'))
  }
}

function downloadJSON() {
  const blob = new Blob([formattedJSON.value], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `orchestration-${form.name || 'unnamed'}.json`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
  ElMessage.success(t('orchestrator.orchestrationForm.jsonDownloaded'))
}
</script>

<style scoped>
.orchestration-form {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: 20px;
  overflow: hidden;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
  flex-shrink: 0;
}

.header h2 {
  margin: 0;
  color: var(--addp-text-primary);
}

.form-metadata {
  margin-bottom: 16px;
  flex-shrink: 0;
}

.schedule-row {
  display: flex;
  align-items: center;
}

.three-column-layout {
  flex: 1;
  display: flex;
  gap: 0;
  min-height: 0;
  overflow: hidden;
}

.left-panel {
  width: 280px;
  flex-shrink: 0;
}

.center-panel {
  flex: 1;
  min-width: 0;
  background: var(--addp-bg-secondary) !important;
  overflow: hidden;
}

.json-preview {
  display: flex;
  flex-direction: column;
  height: 70vh;
}

.json-actions {
  margin-bottom: 12px;
  padding: 8px 12px;
  background: var(--addp-bg-tertiary);
  border-radius: 4px;
  display: flex;
  gap: 8px;
}

.json-content {
  flex: 1;
  overflow: auto;
  background: var(--addp-bg-tertiary);
  padding: 16px;
  border-radius: 4px;
  font-family: 'Consolas', 'Monaco', 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.6;
  color: var(--addp-text-primary);
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
