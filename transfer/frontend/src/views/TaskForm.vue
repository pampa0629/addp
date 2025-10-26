<template>
  <div class="task-form">
    <el-card>
      <template #header>
        <div class="card-header">
          <el-button @click="handleBack">
            <el-icon><ArrowLeft /></el-icon>
            返回
          </el-button>
          <span>{{ isEdit ? '编辑任务' : '创建任务' }}</span>
        </div>
      </template>

      <el-form :model="form" :rules="rules" ref="formRef" label-width="120px">
        <el-form-item label="任务名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入任务名称" />
        </el-form-item>

        <el-form-item label="任务描述" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="3"
            placeholder="请输入任务描述" />
        </el-form-item>

        <el-form-item label="执行模式" prop="mode">
          <el-select v-model="form.mode" placeholder="请选择">
            <el-option label="批处理" value="batch" />
            <el-option label="流式处理" value="stream" />
            <el-option label="微批处理" value="micro-batch" />
          </el-select>
        </el-form-item>

        <el-form-item label="批量大小" prop="batch_size">
          <el-input-number v-model="form.batch_size" :min="100" :max="10000" :step="100" />
        </el-form-item>

        <el-form-item label="定时调度">
          <el-input v-model="form.schedule" placeholder="Cron 表达式，如：0 0 2 * * *" />
          <div class="hint">留空则不启用定时调度，只能手动触发</div>
        </el-form-item>

        <el-divider>数据源配置</el-divider>

        <el-form-item label="源连接器类型">
          <el-select v-model="sourceType" placeholder="请选择">
            <el-option label="MySQL" value="mysql" />
            <el-option label="PostgreSQL" value="postgresql" />
            <el-option label="CSV 文件" value="csv" />
            <el-option label="JSON 文件" value="json" />
            <el-option label="S3/MinIO" value="s3" />
          </el-select>
        </el-form-item>

        <el-form-item label="目标连接器类型">
          <el-select v-model="targetType" placeholder="请选择">
            <el-option label="MySQL" value="mysql" />
            <el-option label="PostgreSQL" value="postgresql" />
            <el-option label="CSV 文件" value="csv" />
            <el-option label="JSON 文件" value="json" />
            <el-option label="S3/MinIO" value="s3" />
          </el-select>
        </el-form-item>

        <el-form-item label="配置JSON">
          <el-input v-model="configJson" type="textarea" :rows="15"
            placeholder='{"source": {...}, "target": {...}}' />
          <div class="hint">请参考文档配置数据源连接信息</div>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="handleSubmit">
            {{ isEdit ? '更新' : '创建' }}
          </el-button>
          <el-button @click="handleBack">取消</el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { taskAPI } from '@/api/tasks'

const router = useRouter()
const route = useRoute()
const formRef = ref(null)

const isEdit = computed(() => !!route.params.id)
const sourceType = ref('')
const targetType = ref('')
const configJson = ref('')

const form = ref({
  name: '',
  description: '',
  type: 'sync',
  mode: 'batch',
  batch_size: 1000,
  schedule: '',
  config: {}
})

const rules = {
  name: [{ required: true, message: '请输入任务名称', trigger: 'blur' }],
  mode: [{ required: true, message: '请选择执行模式', trigger: 'change' }]
}

const handleBack = () => {
  router.back()
}

const handleSubmit = async () => {
  try {
    await formRef.value.validate()

    // 解析配置 JSON
    let config = {}
    if (configJson.value) {
      try {
        config = JSON.parse(configJson.value)
      } catch (error) {
        ElMessage.error('配置JSON格式错误')
        return
      }
    }

    const data = {
      ...form.value,
      config
    }

    if (isEdit.value) {
      await taskAPI.update(route.params.id, data)
      ElMessage.success('任务更新成功')
    } else {
      await taskAPI.create(data)
      ElMessage.success('任务创建成功')
    }

    router.push('/tasks')
  } catch (error) {
    console.error('提交失败:', error)
  }
}

const loadTask = async () => {
  if (!isEdit.value) return

  try {
    const task = await taskAPI.get(route.params.id)
    form.value = {
      name: task.name,
      description: task.description,
      type: task.type,
      mode: task.mode,
      batch_size: task.batch_size,
      schedule: task.schedule || ''
    }
    configJson.value = JSON.stringify(task.config || {}, null, 2)
  } catch (error) {
    console.error('加载任务失败:', error)
  }
}

onMounted(() => {
  loadTask()
})
</script>

<style scoped>
.task-form {
  padding: 20px;
}

.card-header {
  display: flex;
  align-items: center;
  gap: 20px;
}

.hint {
  font-size: 12px;
  color: #909399;
  margin-top: 5px;
}
</style>
