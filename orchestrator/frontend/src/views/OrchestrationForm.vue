<template>
  <div class="orchestration-form">
    <div class="header">
      <h2>{{ isEdit ? '编辑编排' : '创建编排' }}</h2>
      <div>
        <el-button @click="handleCancel">取消</el-button>
        <el-button type="primary" @click="handleSave" :loading="saving">保存</el-button>
      </div>
    </div>

    <el-form :model="form" label-width="120px" class="form-content">
      <el-form-item label="编排名称" required>
        <el-input v-model="form.name" placeholder="请输入编排名称"></el-input>
      </el-form-item>

      <el-form-item label="描述">
        <el-input
          v-model="form.description"
          type="textarea"
          :rows="3"
          placeholder="请输入描述信息"
        ></el-input>
      </el-form-item>

      <el-form-item label="启用">
        <el-switch v-model="form.enabled"></el-switch>
      </el-form-item>

      <el-form-item label="Cron 表达式">
        <el-input
          v-model="form.cron_expr"
          placeholder="例如: 0 0 * * * (每天零点执行)"
        >
          <template #append>
            <el-button @click="showCronHelp">帮助</el-button>
          </template>
        </el-input>
      </el-form-item>

      <el-form-item label="编排步骤" required>
        <DAGEditor
          ref="dagEditor"
          :initial-steps="form.steps"
          @update:steps="handleStepsUpdate"
          style="height: 600px; width: 100%"
        />
      </el-form-item>
    </el-form>

    <!-- Cron 帮助对话框 -->
    <el-dialog v-model="cronHelpVisible" title="Cron 表达式格式" width="600px">
      <div class="cron-help">
        <p><strong>格式:</strong> 分 时 日 月 周</p>
        <p><strong>示例:</strong></p>
        <ul>
          <li><code>0 0 * * *</code> - 每天零点执行</li>
          <li><code>0 */2 * * *</code> - 每2小时执行一次</li>
          <li><code>30 9 * * 1-5</code> - 工作日上午9:30执行</li>
          <li><code>0 12 1 * *</code> - 每月1号中午12点执行</li>
        </ul>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import DAGEditor from '../components/DAGEditor.vue'
import orchestrationAPI from '../api/orchestration'

const router = useRouter()
const route = useRoute()
const dagEditor = ref(null)

const isEdit = ref(false)
const saving = ref(false)
const cronHelpVisible = ref(false)

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

function showCronHelp() {
  cronHelpVisible.value = true
}
</script>

<style scoped>
.orchestration-form {
  padding: 20px;
  height: 100%;
  display: flex;
  flex-direction: column;
}

.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

h2 {
  margin: 0;
}

.form-content {
  flex: 1;
  overflow-y: auto;
}

.cron-help {
  line-height: 1.8;
}

.cron-help code {
  background: #f5f7fa;
  padding: 2px 8px;
  border-radius: 3px;
  font-family: monospace;
}

.cron-help ul {
  padding-left: 20px;
}
</style>
