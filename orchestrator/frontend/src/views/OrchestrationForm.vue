<template>
  <div class="orchestration-form">
    <div class="header">
      <h2>{{ isEdit ? '编辑编排' : '创建编排' }}</h2>
      <div>
        <el-button @click="handleCancel">取消</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </div>
    </div>

    <el-form :model="form" label-width="120px" class="form-metadata">
      <el-form-item label="编排名称" required>
        <el-input v-model="form.name" placeholder="请输入编排名称"></el-input>
      </el-form-item>

      <el-form-item label="描述">
        <el-input
          v-model="form.description"
          type="textarea"
          :rows="2"
          placeholder="请输入描述信息"
        ></el-input>
      </el-form-item>

      <el-form-item label="启用">
        <el-switch v-model="form.enabled"></el-switch>
      </el-form-item>

      <el-form-item label="定时调度">
        <ScheduleConfig
          v-model="form.cron_expr"
          :allow-custom-cron="true"
        />
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

  </div>
</template>

<script setup>
import { ref, reactive, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import DAGEditor from '../components/DAGEditor.vue'
import TaskPanel from '../components/TaskPanel.vue'
import orchestrationAPI from '../api/orchestration'
import { ScheduleConfig } from '@common-ui'

const router = useRouter()
const route = useRoute()
const dagEditor = ref(null)

const isEdit = ref(false)
const saving = ref(false)

const form = reactive({
  name: '',
  description: '',
  enabled: false,
  cron_expr: '',
  steps: []
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
    ElMessage.error('加载编排失败')
  }
}

function handleStepsUpdate(steps) {
  form.steps = steps
}

async function handleSave() {
  if (!form.name) {
    ElMessage.warning('请输入编排名称')
    return
  }

  if (!form.steps || form.steps.length === 0) {
    ElMessage.warning('请至少添加一个步骤')
    return
  }

  saving.value = true
  try {
    if (isEdit.value) {
      await orchestrationAPI.update(route.params.id, form)
      ElMessage.success('更新成功')
    } else {
      await orchestrationAPI.create(form)
      ElMessage.success('创建成功')
    }
    router.push('/orchestrations')
  } catch (error) {
    ElMessage.error(isEdit.value ? '更新失败' : '创建失败')
  } finally {
    saving.value = false
  }
}

function handleCancel() {
  router.back()
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
}

.form-metadata {
  margin-bottom: 16px;
  flex-shrink: 0;
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
  background: #fafbfc;
  overflow: hidden;
}
</style>
