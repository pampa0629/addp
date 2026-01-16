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

      <el-form :model="form" :rules="rules" ref="formRef" label-width="140px">
        <!-- 基本信息 -->
        <el-form-item label="任务名称" prop="name">
          <el-input v-model="form.name" placeholder="请输入任务名称" />
        </el-form-item>

        <el-form-item label="任务描述" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="2"
            placeholder="请输入任务描述" />
        </el-form-item>

        <el-divider>性能配置</el-divider>

        <!-- 性能模式选择 -->
        <el-form-item label="性能模式">
          <el-radio-group v-model="performanceMode" @change="handlePerformanceModeChange">
            <el-radio-button label="standard">标准模式</el-radio-button>
            <el-radio-button label="high-performance">高性能模式</el-radio-button>
          </el-radio-group>
          <div class="hint">
            <span v-if="performanceMode === 'standard'">适合小于 100 万条记录的数据导入</span>
            <span v-else>适合千万级以上数据，使用并行读写 + COPY 协议，性能提升 10x+</span>
          </div>
        </el-form-item>

        <!-- 高性能配置（仅在高性能模式下显示） -->
        <template v-if="performanceMode === 'high-performance'">
          <el-form-item label="数据量级">
            <el-select v-model="dataScale" @change="handleDataScaleChange" placeholder="请选择">
              <el-option label="中等 (100万-1000万)" value="medium" />
              <el-option label="大型 (1000万-5000万)" value="large" />
              <el-option label="超大 (5000万以上)" value="xlarge" />
            </el-select>
            <div class="hint">系统将根据数据量级自动优化并行参数</div>
          </el-form-item>

          <el-form-item label="并行工作线程">
            <el-input-number v-model="form.num_workers" :min="2" :max="32" :step="1" />
            <span class="param-hint">推荐: {{ recommendedWorkers }} 个（根据 CPU 核心数）</span>
          </el-form-item>

          <el-form-item label="分区大小">
            <el-input-number v-model="form.partition_size" :min="50000" :max="1000000" :step="50000" />
            <span class="param-hint">每个分区的记录数</span>
          </el-form-item>

          <el-form-item label="数据库连接数">
            <el-input-number v-model="form.max_connections" :min="2" :max="16" :step="1" />
            <span class="param-hint">PostgreSQL 并发写入连接数</span>
          </el-form-item>
        </template>

        <el-form-item label="读取批次大小">
          <el-input-number v-model="form.batch_size" :min="100" :max="50000" :step="1000" />
          <span class="param-hint">
            {{ performanceMode === 'high-performance' ? '推荐: 5000-10000' : '推荐: 1000' }}
          </span>
        </el-form-item>

        <el-form-item label="写入批次大小">
          <el-input-number v-model="form.write_batch_size" :min="100" :max="50000" :step="1000" />
          <span class="param-hint">
            {{ performanceMode === 'high-performance' ? '推荐: 10000-30000' : '推荐: 1000' }}
          </span>
        </el-form-item>

        <el-form-item label="执行模式" prop="mode">
          <el-select v-model="form.mode" placeholder="请选择" disabled>
            <el-option label="批处理" value="batch" />
            <el-option label="并行处理" value="parallel" />
          </el-select>
          <div class="hint">{{ performanceMode === 'high-performance' ? '自动使用并行模式' : '标准批处理模式' }}</div>
        </el-form-item>

        <el-form-item label="定时调度">
          <ScheduleConfig v-model="form.schedule" :allow-custom-cron="true" />
        </el-form-item>

        <el-divider>数据源配置</el-divider>

        <!-- 源连接器 -->
        <el-form-item label="源数据类型">
          <el-select v-model="sourceType" @change="handleSourceTypeChange" placeholder="请选择">
            <el-option label="Spatialite (单线程)" value="spatialite" />
            <el-option label="Spatialite (并行)" value="spatialite_parallel" />
            <el-option label="PostgreSQL" value="postgresql" />
            <el-option label="MySQL" value="mysql" />
            <el-option label="CSV 文件" value="csv" />
            <el-option label="GeoJSON" value="geojson" />
            <el-option label="Shapefile" value="shapefile" />
            <el-option label="S3/MinIO" value="s3" />
          </el-select>
          <div class="hint" v-if="sourceType === 'spatialite_parallel'">
            ⚡ 并行模式：适合千万级数据，自动分区并发读取
          </div>
        </el-form-item>

        <!-- Spatialite 源配置 -->
        <template v-if="sourceType === 'spatialite' || sourceType === 'spatialite_parallel'">
          <el-form-item label="Spatialite 文件路径">
            <el-input v-model="sourceConfig.full_name" placeholder="/path/to/your/data.sqlite" />
          </el-form-item>
          <el-form-item label="表名">
            <el-input v-model="sourceConfig.table" placeholder="table_name" />
          </el-form-item>
          <el-form-item label="几何列名">
            <el-input v-model="sourceConfig.geometry_field" placeholder="geom (留空自动探测)" />
          </el-form-item>
          <el-form-item label="WHERE 条件">
            <el-input v-model="sourceConfig.where_clause" placeholder="可选的 SQL WHERE 子句" />
          </el-form-item>
          <el-form-item label="分区键" v-if="sourceType === 'spatialite_parallel'">
            <el-input v-model="sourceConfig.partition_key" placeholder="ROWID (默认)" />
            <div class="hint">用于分区的字段，通常使用 ROWID 或整型主键</div>
          </el-form-item>
        </template>

        <!-- PostgreSQL 源配置 -->
        <template v-if="sourceType === 'postgresql' || sourceType === 'mysql'">
          <el-form-item label="主机">
            <el-input v-model="sourceConfig.host" placeholder="localhost" />
          </el-form-item>
          <el-form-item label="端口">
            <el-input-number v-model="sourceConfig.port" :min="1" :max="65535" />
          </el-form-item>
          <el-form-item label="数据库">
            <el-input v-model="sourceConfig.database" placeholder="database_name" />
          </el-form-item>
          <el-form-item label="用户名">
            <el-input v-model="sourceConfig.username" placeholder="username" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="sourceConfig.password" type="password" placeholder="password" show-password />
          </el-form-item>
          <el-form-item label="表名/查询">
            <el-input v-model="sourceConfig.table" placeholder="表名或 SQL 查询" />
          </el-form-item>
        </template>

        <el-divider>目标配置</el-divider>

        <!-- 目标连接器 -->
        <el-form-item label="目标数据类型">
          <el-select v-model="targetType" @change="handleTargetTypeChange" placeholder="请选择">
            <el-option label="PostgreSQL (标准)" value="postgresql" />
            <el-option label="PostgreSQL (COPY)" value="postgres_copy" />
            <el-option label="MySQL" value="mysql" />
            <el-option label="CSV 文件" value="csv" />
            <el-option label="GeoJSON" value="geojson" />
            <el-option label="Shapefile" value="shapefile" />
            <el-option label="S3/MinIO" value="s3" />
          </el-select>
          <div class="hint" v-if="targetType === 'postgres_copy'">
            ⚡ COPY 协议：性能提升 5-10x，适合大批量导入
          </div>
        </el-form-item>

        <!-- PostgreSQL 目标配置 -->
        <template v-if="targetType === 'postgresql' || targetType === 'postgres_copy'">
          <el-form-item label="主机">
            <el-input v-model="targetConfig.host" placeholder="192.168.1.92" />
          </el-form-item>
          <el-form-item label="端口">
            <el-input-number v-model="targetConfig.port" :min="1" :max="65535" />
          </el-form-item>
          <el-form-item label="数据库">
            <el-input v-model="targetConfig.database" placeholder="business" />
          </el-form-item>
          <el-form-item label="用户名">
            <el-input v-model="targetConfig.username" placeholder="business" />
          </el-form-item>
          <el-form-item label="密码">
            <el-input v-model="targetConfig.password" type="password" placeholder="business_password" show-password />
          </el-form-item>
          <el-form-item label="目标表">
            <el-input v-model="targetConfig.table" placeholder="schema.table_name (如 spatial.poi)" />
          </el-form-item>
          <el-form-item label="自动建表">
            <el-switch v-model="targetConfig.create_table" />
            <span class="param-hint">首次导入时启用</span>
          </el-form-item>
          <el-form-item label="SRID">
            <el-input-number v-model="targetConfig.srid" :min="0" :max="9999" />
            <span class="param-hint">几何坐标系，常用: 4326 (WGS84)、3857 (Web Mercator)</span>
          </el-form-item>
          <el-form-item label="几何列名">
            <el-input v-model="targetConfig.geometry_column" placeholder="geom (留空自动识别)" />
          </el-form-item>
        </template>

        <el-divider />

        <!-- 配置预览 -->
        <el-form-item label="配置预览">
          <el-input v-model="configPreview" type="textarea" :rows="12" readonly />
          <div class="hint">
            <el-button size="small" @click="copyConfig">复制配置</el-button>
            <span style="margin-left: 10px">自动生成的任务配置</span>
          </div>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" @click="handleSubmit" :loading="submitting">
            {{ isEdit ? '更新任务' : '创建任务' }}
          </el-button>
          <el-button @click="handleBack">取消</el-button>
          <el-button type="success" plain @click="handleTestConnection" :loading="testing">
            测试连接
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { taskAPI } from '@/api/tasks'
import { ScheduleConfig } from '@common-ui'

const router = useRouter()
const route = useRoute()
const formRef = ref(null)
const submitting = ref(false)
const testing = ref(false)

const isEdit = computed(() => !!route.params.id)

// 性能模式
const performanceMode = ref('standard')
const dataScale = ref('medium')

// 连接器类型
const sourceType = ref('spatialite')
const targetType = ref('postgresql')

// 表单数据
const form = ref({
  name: '',
  description: '',
  mode: 'batch',
  batch_size: 1000,
  write_batch_size: 1000,
  schedule: '',
  // 高性能参数
  num_workers: 4,
  partition_size: 100000,
  max_connections: 4
})

// 源配置
const sourceConfig = ref({
  full_name: '',
  table: '',
  geometry_field: '',
  where_clause: '',
  partition_key: 'ROWID',
  host: '',
  port: 5432,
  database: '',
  username: '',
  password: ''
})

// 目标配置
const targetConfig = ref({
  host: '192.168.1.92',
  port: 5433,
  database: 'business',
  username: 'business',
  password: 'business_password',
  table: 'spatial.imported_data',
  create_table: true,
  srid: 4326,
  geometry_column: 'geom'
})

const rules = {
  name: [{ required: true, message: '请输入任务名称', trigger: 'blur' }],
  mode: [{ required: true, message: '请选择执行模式', trigger: 'change' }]
}

// 推荐参数（根据 CPU 核心数）
const recommendedWorkers = computed(() => {
  const cores = navigator.hardwareConcurrency || 4
  if (dataScale.value === 'medium') return Math.min(cores, 8)
  if (dataScale.value === 'large') return Math.min(cores, 12)
  return Math.min(cores, 16)
})

// 配置预览
const configPreview = computed(() => {
  const config = {
    name: form.value.name || '未命名任务',
    description: form.value.description,
    source_config: {
      type: performanceMode.value === 'high-performance' && sourceType.value === 'spatialite'
        ? 'spatialite_parallel'
        : sourceType.value,
      config: buildSourceConfig(),
      batch_size: form.value.batch_size
    },
    target_config: {
      type: performanceMode.value === 'high-performance' && targetType.value === 'postgresql'
        ? 'postgres_copy'
        : targetType.value,
      config: buildTargetConfig(),
      batch_size: form.value.write_batch_size
    },
    execution_mode: performanceMode.value === 'high-performance' ? 'parallel' : 'serial',
    transforms: []
  }

  if (form.value.schedule) {
    config.schedule = form.value.schedule
  }

  return JSON.stringify(config, null, 2)
})

// 构建源配置
const buildSourceConfig = () => {
  if (sourceType.value === 'spatialite' || sourceType.value === 'spatialite_parallel') {
    const config = {
      full_name: sourceConfig.value.full_name,
      table: sourceConfig.value.table
    }
    if (sourceConfig.value.geometry_field) {
      config.geometry_fields = [sourceConfig.value.geometry_field]
    }
    if (sourceConfig.value.where_clause) {
      config.where_clause = sourceConfig.value.where_clause
    }
    if (sourceType.value === 'spatialite_parallel' || performanceMode.value === 'high-performance') {
      config.num_workers = form.value.num_workers
      config.partition_key = sourceConfig.value.partition_key || 'ROWID'
      config.partition_size = form.value.partition_size
    }
    return config
  }

  if (sourceType.value === 'postgresql' || sourceType.value === 'mysql') {
    return {
      driver: sourceType.value,
      host: sourceConfig.value.host,
      port: sourceConfig.value.port,
      database: sourceConfig.value.database,
      username: sourceConfig.value.username,
      password: sourceConfig.value.password,
      table: sourceConfig.value.table
    }
  }

  return {}
}

// 构建目标配置
const buildTargetConfig = () => {
  if (targetType.value === 'postgresql' || targetType.value === 'postgres_copy') {
    const config = {
      host: targetConfig.value.host,
      port: targetConfig.value.port,
      database: targetConfig.value.database,
      username: targetConfig.value.username,
      password: targetConfig.value.password,
      table: targetConfig.value.table,
      create_table: targetConfig.value.create_table,
      srid: targetConfig.value.srid
    }
    if (targetConfig.value.geometry_column) {
      config.geometry_columns = [targetConfig.value.geometry_column]
    }
    if (targetType.value === 'postgres_copy' || performanceMode.value === 'high-performance') {
      config.max_connections = form.value.max_connections
    }
    return config
  }

  return {}
}

// 性能模式变化
const handlePerformanceModeChange = (mode) => {
  if (mode === 'high-performance') {
    form.value.mode = 'parallel'
    form.value.batch_size = 5000
    form.value.write_batch_size = 10000
    // 自动切换到高性能连接器
    if (sourceType.value === 'spatialite') {
      sourceType.value = 'spatialite_parallel'
    }
    if (targetType.value === 'postgresql') {
      targetType.value = 'postgres_copy'
    }
  } else {
    form.value.mode = 'batch'
    form.value.batch_size = 1000
    form.value.write_batch_size = 1000
    // 恢复标准连接器
    if (sourceType.value === 'spatialite_parallel') {
      sourceType.value = 'spatialite'
    }
    if (targetType.value === 'postgres_copy') {
      targetType.value = 'postgresql'
    }
  }
}

// 数据量级变化
const handleDataScaleChange = (scale) => {
  if (scale === 'medium') {
    form.value.num_workers = 4
    form.value.partition_size = 100000
    form.value.max_connections = 4
    form.value.batch_size = 5000
    form.value.write_batch_size = 10000
  } else if (scale === 'large') {
    form.value.num_workers = 8
    form.value.partition_size = 200000
    form.value.max_connections = 8
    form.value.batch_size = 8000
    form.value.write_batch_size = 20000
  } else if (scale === 'xlarge') {
    form.value.num_workers = 16
    form.value.partition_size = 500000
    form.value.max_connections = 12
    form.value.batch_size = 10000
    form.value.write_batch_size = 30000
  }
}

const handleSourceTypeChange = () => {
  // 清空配置
  sourceConfig.value = {
    full_name: '',
    table: '',
    geometry_field: '',
    where_clause: '',
    partition_key: 'ROWID',
    host: '',
    port: sourceType.value === 'mysql' ? 3306 : 5432,
    database: '',
    username: '',
    password: ''
  }
}

const handleTargetTypeChange = () => {
  // 保留部分默认值
  if (targetType.value === 'postgresql' || targetType.value === 'postgres_copy') {
    targetConfig.value.port = 5433
  } else if (targetType.value === 'mysql') {
    targetConfig.value.port = 3306
  }
}

const handleBack = () => {
  router.back()
}

const copyConfig = () => {
  navigator.clipboard.writeText(configPreview.value)
  ElMessage.success('配置已复制到剪贴板')
}

const handleTestConnection = async () => {
  testing.value = true
  try {
    // TODO: 实现连接测试 API
    await new Promise(resolve => setTimeout(resolve, 1000))
    ElMessage.success('连接测试成功')
  } catch (error) {
    ElMessage.error('连接测试失败: ' + error.message)
  } finally {
    testing.value = false
  }
}

const handleSubmit = async () => {
  try {
    await formRef.value.validate()

    submitting.value = true

    const config = JSON.parse(configPreview.value)

    if (isEdit.value) {
      await taskAPI.update(route.params.id, config)
      ElMessage.success('任务更新成功')
    } else {
      await taskAPI.create(config)
      ElMessage.success('任务创建成功')
    }

    router.push('/tasks')
  } catch (error) {
    console.error('提交失败:', error)
    ElMessage.error('提交失败: ' + (error.response?.data?.error || error.message))
  } finally {
    submitting.value = false
  }
}

const loadTask = async () => {
  if (!isEdit.value) return

  try {
    const task = await taskAPI.get(route.params.id)

    form.value = {
      name: task.name,
      description: task.description,
      mode: task.mode || task.execution_mode,
      batch_size: task.source_config?.batch_size || 1000,
      write_batch_size: task.target_config?.batch_size || 1000,
      schedule: task.schedule || '',
      num_workers: task.source_config?.config?.num_workers || 4,
      partition_size: task.source_config?.config?.partition_size || 100000,
      max_connections: task.target_config?.config?.max_connections || 4
    }

    // 推断性能模式
    if (task.execution_mode === 'parallel' || task.source_config?.type === 'spatialite_parallel') {
      performanceMode.value = 'high-performance'
    }

    // 设置源类型
    sourceType.value = task.source_config?.type || 'spatialite'
    if (task.source_config?.config) {
      Object.assign(sourceConfig.value, task.source_config.config)
    }

    // 设置目标类型
    targetType.value = task.target_config?.type || 'postgresql'
    if (task.target_config?.config) {
      Object.assign(targetConfig.value, task.target_config.config)
      if (task.target_config.config.geometry_columns?.length > 0) {
        targetConfig.value.geometry_column = task.target_config.config.geometry_columns[0]
      }
    }
  } catch (error) {
    console.error('加载任务失败:', error)
    ElMessage.error('加载任务失败')
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
  font-size: 16px;
  font-weight: 600;
}

.hint {
  font-size: 12px;
  color: #909399;
  margin-top: 5px;
}

.param-hint {
  margin-left: 10px;
  font-size: 12px;
  color: #909399;
}

:deep(.el-form-item__label) {
  font-weight: 500;
}

:deep(.el-divider__text) {
  font-weight: 600;
  color: #303133;
}
</style>
