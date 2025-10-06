<template>
  <div class="metadata-scan">
    <el-card>
      <template #header>
        <div style="display: flex; justify-content: space-between; align-items: center;">
          <span>元数据扫描</span>
        </div>
      </template>

      <!-- 步骤1: 选择数据源 -->
      <el-steps :active="currentStep" align-center style="margin-bottom: 30px">
        <el-step title="选择数据源" description="从系统管理中选择存储引擎" />
        <el-step title="选择扫描范围" description="选择要扫描的Schema和表" />
        <el-step title="配置扫描策略" description="设置扫描方式和触发策略" />
        <el-step title="执行扫描" description="开始扫描并查看结果" />
      </el-steps>

      <!-- 步骤内容 -->
      <div class="step-content">
        <!-- 步骤1: 选择数据源 -->
        <div v-if="currentStep === 0">
          <el-alert
            title="提示"
            type="info"
            description="请从系统管理中已配置的存储引擎中选择一个数据源进行元数据扫描"
            :closable="false"
            style="margin-bottom: 20px"
          />

          <el-table
            :data="resources"
            border
            v-loading="loading"
            @row-click="handleSelectResource"
            highlight-current-row
            style="cursor: pointer"
          >
            <el-table-column type="index" label="#" width="60" />
            <el-table-column prop="name" label="数据源名称" width="200" />
            <el-table-column prop="resource_type" label="类型" width="120">
              <template #default="{ row }">
                <el-tag>{{ row.resource_type }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="connection_info.host" label="主机" width="150" />
            <el-table-column prop="connection_info.database" label="数据库" width="150" />
            <el-table-column prop="description" label="描述" show-overflow-tooltip />
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag v-if="row.id === selectedResource?.id" type="success">已选择</el-tag>
                <el-tag v-else type="info">未选择</el-tag>
              </template>
            </el-table-column>
          </el-table>

          <div style="margin-top: 20px; text-align: right">
            <el-button type="primary" @click="nextStep" :disabled="!selectedResource">
              下一步
            </el-button>
          </div>
        </div>

        <!-- 步骤2: 选择扫描范围 -->
        <div v-if="currentStep === 1">
          <el-alert
            title="正在加载Schema列表..."
            type="info"
            v-if="loadingSchemas"
            :closable="false"
            style="margin-bottom: 20px"
          />

          <div v-else>
            <el-alert
              :title="`已连接到数据源: ${selectedResource.name}`"
              type="success"
              :closable="false"
              style="margin-bottom: 20px"
            />

            <div style="margin-bottom: 20px">
              <el-checkbox v-model="selectAllSchemas" @change="handleSelectAllSchemas">
                全选所有Schema
              </el-checkbox>
            </div>

            <el-table :data="schemas" border max-height="400">
              <el-table-column width="60">
                <template #default="{ row }">
                  <el-checkbox
                    v-model="row.selected"
                    @change="handleSchemaSelectionChange"
                  />
                </template>
              </el-table-column>
              <el-table-column prop="name" label="Schema名称" width="200" />
              <el-table-column label="表数量" width="120">
                <template #default="{ row }">
                  <el-tag>{{ row.tables?.length || 0 }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column label="扫描设置">
                <template #default="{ row }">
                  <div v-if="row.selected">
                    <el-radio-group v-model="row.scanMode" size="small">
                      <el-radio label="all">扫描所有表</el-radio>
                      <el-radio label="select">选择特定表</el-radio>
                    </el-radio-group>

                    <!-- 表选择器 -->
                    <div v-if="row.scanMode === 'select'" style="margin-top: 10px">
                      <el-select
                        v-model="row.selectedTables"
                        multiple
                        collapse-tags
                        placeholder="选择要扫描的表"
                        style="width: 100%"
                      >
                        <el-option
                          v-for="table in row.tables"
                          :key="table"
                          :label="table"
                          :value="table"
                        />
                      </el-select>
                    </div>
                  </div>
                  <span v-else style="color: #999">未选择</span>
                </template>
              </el-table-column>
            </el-table>
          </div>

          <div style="margin-top: 20px; text-align: right">
            <el-button @click="prevStep">上一步</el-button>
            <el-button
              type="primary"
              @click="nextStep"
              :disabled="!selectedSchemas.length"
            >
              下一步
            </el-button>
          </div>
        </div>

        <!-- 步骤3: 配置扫描策略 -->
        <div v-if="currentStep === 2">
          <el-form :model="scanConfig" label-width="140px">
            <el-form-item label="扫描深度">
              <el-radio-group v-model="scanConfig.depth">
                <el-radio label="basic">基础扫描(Schema + 表列表)</el-radio>
                <el-radio label="deep">深度扫描(表结构 + 字段信息)</el-radio>
                <el-radio label="full">完全扫描(包含统计信息)</el-radio>
              </el-radio-group>
              <div style="color: #999; font-size: 12px; margin-top: 5px">
                <div v-if="scanConfig.depth === 'basic'">快速扫描,仅获取Schema和表名</div>
                <div v-else-if="scanConfig.depth === 'deep'">扫描表结构、字段类型、索引等</div>
                <div v-else>完整扫描,包含行数、数据量等统计信息(较慢)</div>
              </div>
            </el-form-item>

            <el-form-item label="扫描触发方式">
              <el-radio-group v-model="scanConfig.trigger">
                <el-radio label="manual">立即执行(手动扫描)</el-radio>
                <el-radio label="scheduled">定时执行(每日零点)</el-radio>
                <el-radio label="both">立即执行 + 启用定时</el-radio>
              </el-radio-group>
            </el-form-item>

            <el-form-item label="定时扫描设置" v-if="scanConfig.trigger !== 'manual'">
              <el-input
                v-model="scanConfig.cron"
                placeholder="Cron表达式"
                style="width: 300px"
              />
              <div style="color: #999; font-size: 12px; margin-top: 5px">
                默认: 0 0 * * * (每日零点执行)
              </div>
            </el-form-item>

            <el-form-item label="增量更新">
              <el-switch v-model="scanConfig.incremental" />
              <span style="margin-left: 10px; color: #999; font-size: 12px">
                仅扫描新增或修改的表
              </span>
            </el-form-item>
          </el-form>

          <el-divider />

          <div style="background: #f5f7fa; padding: 15px; border-radius: 4px">
            <h4>扫描摘要</h4>
            <div style="margin-top: 10px">
              <p><strong>数据源:</strong> {{ selectedResource.name }}</p>
              <p><strong>选择的Schema:</strong> {{ selectedSchemas.length }} 个</p>
              <p><strong>预计扫描表数:</strong> {{ totalTables }} 个</p>
              <p><strong>扫描深度:</strong> {{ scanConfig.depth === 'basic' ? '基础扫描' : scanConfig.depth === 'deep' ? '深度扫描' : '完全扫描' }}</p>
              <p><strong>触发方式:</strong> {{ scanConfig.trigger === 'manual' ? '立即执行' : scanConfig.trigger === 'scheduled' ? '定时执行' : '立即执行+定时' }}</p>
            </div>
          </div>

          <div style="margin-top: 20px; text-align: right">
            <el-button @click="prevStep">上一步</el-button>
            <el-button type="primary" @click="nextStep">下一步</el-button>
          </div>
        </div>

        <!-- 步骤4: 执行扫描 -->
        <div v-if="currentStep === 3">
          <div v-if="!scanning && !scanResult">
            <el-alert
              title="确认扫描配置"
              type="warning"
              description="请确认扫描配置无误后,点击'开始扫描'按钮启动元数据扫描"
              :closable="false"
              style="margin-bottom: 20px"
            />

            <el-descriptions title="扫描配置" border :column="2">
              <el-descriptions-item label="数据源">{{ selectedResource.name }}</el-descriptions-item>
              <el-descriptions-item label="数据源类型">{{ selectedResource.resource_type }}</el-descriptions-item>
              <el-descriptions-item label="Schema数量">{{ selectedSchemas.length }}</el-descriptions-item>
              <el-descriptions-item label="表数量">{{ totalTables }}</el-descriptions-item>
              <el-descriptions-item label="扫描深度">{{ scanConfig.depth }}</el-descriptions-item>
              <el-descriptions-item label="触发方式">{{ scanConfig.trigger }}</el-descriptions-item>
              <el-descriptions-item label="增量更新" :span="2">
                {{ scanConfig.incremental ? '是' : '否' }}
              </el-descriptions-item>
            </el-descriptions>

            <div style="margin-top: 30px; text-align: center">
              <el-button @click="prevStep">上一步</el-button>
              <el-button type="primary" size="large" @click="startScan" :loading="scanning">
                <el-icon><Search /></el-icon> 开始扫描
              </el-button>
            </div>
          </div>

          <div v-else-if="scanning">
            <el-result icon="info" title="正在扫描..." sub-title="请稍候,正在扫描元数据...">
              <template #extra>
                <el-progress :percentage="scanProgress" :status="scanProgress === 100 ? 'success' : undefined" />
                <div style="margin-top: 20px; color: #999">
                  {{ scanStatus }}
                </div>
              </template>
            </el-result>
          </div>

          <div v-else-if="scanResult">
            <el-result
              :icon="scanResult.status === 'success' ? 'success' : 'error'"
              :title="scanResult.status === 'success' ? '扫描完成!' : '扫描失败'"
              :sub-title="scanResult.message"
            >
              <template #extra>
                <el-descriptions border :column="3">
                  <el-descriptions-item label="扫描的Schema">{{ scanResult.schemasScanned }}</el-descriptions-item>
                  <el-descriptions-item label="扫描的表">{{ scanResult.tablesScanned }}</el-descriptions-item>
                  <el-descriptions-item label="扫描的字段">{{ scanResult.fieldsScanned }}</el-descriptions-item>
                  <el-descriptions-item label="耗时(秒)">{{ scanResult.durationSeconds }}</el-descriptions-item>
                  <el-descriptions-item label="开始时间" :span="2">{{ scanResult.startedAt }}</el-descriptions-item>
                </el-descriptions>

                <div style="margin-top: 20px">
                  <el-button type="primary" @click="viewMetadata">查看元数据</el-button>
                  <el-button @click="resetScan">重新扫描</el-button>
                </div>
              </template>
            </el-result>
          </div>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Search } from '@element-plus/icons-vue'
import systemApi from '../api/system'
import metaApi from '../api/meta'

const router = useRouter()

// 步骤控制
const currentStep = ref(0)
const nextStep = () => {
  if (currentStep.value === 0 && selectedResource.value) {
    loadSchemas()
  }
  currentStep.value++
}
const prevStep = () => currentStep.value--

// 步骤1: 数据源列表
const resources = ref([])
const selectedResource = ref(null)
const loading = ref(false)

const loadResources = async () => {
  loading.value = true
  try {
    const res = await systemApi.getResources({ resource_type: 'postgresql' })
    resources.value = res.data || []
  } catch (error) {
    ElMessage.error('加载数据源列表失败')
  } finally {
    loading.value = false
  }
}

const handleSelectResource = (row) => {
  selectedResource.value = row
}

// 步骤2: Schema列表
const schemas = ref([])
const loadingSchemas = ref(false)
const selectAllSchemas = ref(false)

const loadSchemas = async () => {
  loadingSchemas.value = true
  try {
    // 调用后端API获取Schema列表
    const res = await metaApi.getSchemas(selectedResource.value.id)
    schemas.value = (res.data || []).map(schema => ({
      name: schema.name,
      tables: schema.tables || [],
      selected: false,
      scanMode: 'all',
      selectedTables: []
    }))
  } catch (error) {
    ElMessage.error('加载Schema列表失败')
    schemas.value = []
  } finally {
    loadingSchemas.value = false
  }
}

const handleSelectAllSchemas = (val) => {
  schemas.value.forEach(schema => {
    schema.selected = val
  })
}

const handleSchemaSelectionChange = () => {
  selectAllSchemas.value = schemas.value.every(s => s.selected)
}

const selectedSchemas = computed(() => {
  return schemas.value.filter(s => s.selected)
})

const totalTables = computed(() => {
  return selectedSchemas.value.reduce((sum, schema) => {
    if (schema.scanMode === 'all') {
      return sum + (schema.tables?.length || 0)
    } else {
      return sum + (schema.selectedTables?.length || 0)
    }
  }, 0)
})

// 步骤3: 扫描配置
const scanConfig = ref({
  depth: 'deep',
  trigger: 'manual',
  cron: '0 0 * * *',
  incremental: false
})

// 步骤4: 执行扫描
const scanning = ref(false)
const scanProgress = ref(0)
const scanStatus = ref('')
const scanResult = ref(null)

const startScan = async () => {
  scanning.value = true
  scanProgress.value = 0
  scanStatus.value = '准备扫描...'

  try {
    // 构建扫描请求
    const scanRequest = {
      resource_id: selectedResource.value.id,
      depth: scanConfig.value.depth,
      trigger: scanConfig.value.trigger,
      cron: scanConfig.value.cron,
      incremental: scanConfig.value.incremental,
      schemas: selectedSchemas.value.map(s => ({
        name: s.name,
        scan_mode: s.scanMode,
        tables: s.scanMode === 'select' ? s.selectedTables : null
      }))
    }

    // 模拟扫描进度
    const progressInterval = setInterval(() => {
      if (scanProgress.value < 90) {
        scanProgress.value += 10
        scanStatus.value = `正在扫描... ${scanProgress.value}%`
      }
    }, 500)

    // 调用扫描API
    const res = await metaApi.scanMetadata(scanRequest)

    clearInterval(progressInterval)
    scanProgress.value = 100
    scanStatus.value = '扫描完成'

    // 显示扫描结果
    scanResult.value = {
      status: res.status || 'success',
      message: res.message || '元数据扫描成功',
      schemasScanned: res.schemas_scanned || selectedSchemas.value.length,
      tablesScanned: res.tables_scanned || totalTables.value,
      fieldsScanned: res.fields_scanned || 0,
      durationSeconds: res.duration_seconds || 0,
      startedAt: res.started_at || new Date().toLocaleString()
    }

    ElMessage.success('元数据扫描完成')
  } catch (error) {
    scanResult.value = {
      status: 'error',
      message: error.response?.data?.error || '扫描失败',
      schemasScanned: 0,
      tablesScanned: 0,
      fieldsScanned: 0,
      durationSeconds: 0,
      startedAt: new Date().toLocaleString()
    }
    ElMessage.error('扫描失败')
  } finally {
    scanning.value = false
  }
}

const viewMetadata = () => {
  router.push('/metadata')
}

const resetScan = () => {
  currentStep.value = 0
  selectedResource.value = null
  schemas.value = []
  scanResult.value = null
  scanProgress.value = 0
}

onMounted(() => {
  loadResources()
})
</script>

<style scoped>
.metadata-scan {
  padding: 20px;
}

.step-content {
  min-height: 400px;
  padding: 20px 0;
}
</style>
