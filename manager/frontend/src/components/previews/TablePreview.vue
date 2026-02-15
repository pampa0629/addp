<template>
  <div class="table-preview">
    <!-- 地图预览控制和地图区域 -->
    <template v-if="hasGeometry">
      <div class="map-controls">
        <!-- 左侧：地图预览开关 -->
        <div class="toggle-wrapper">
          <span class="toggle-label">地图预览</span>
          <el-switch v-model="showMap" size="small" />
        </div>

        <!-- 右侧：预缓存控制 + 模式切换 -->
        <template v-if="showMap">
          <div class="pre-cache-controls">
            <!-- 状态：none - 未预缓存 -->
            <el-button
              v-if="quickViewStatus === 'none'"
              type="primary"
              size="small"
              :loading="quickViewLoading"
              @click="handleQuickView"
            >
              🚀 启用预缓存
            </el-button>

            <!-- 状态：generating - 生成中 -->
            <template v-else-if="quickViewStatus === 'generating'">
              <!-- 横向进度条 -->
              <div class="progress-bar-wrapper">
                <el-progress
                  :percentage="quickViewProgress?.progress_percent || 0"
                  :format="() => `${(quickViewProgress?.progress_percent || 0).toFixed(1)}%`"
                  :stroke-width="20"
                />
              </div>

              <!-- 进度详情 -->
              <div v-if="quickViewProgress" class="progress-info">
                <span class="progress-text">
                  {{ quickViewProgress.tiles_processed }} / {{ quickViewProgress.tiles_total_estimate }} 瓦片
                </span>
                <span class="progress-text">
                  已用时 {{ formatTime(quickViewProgress.elapsed_seconds) }}
                </span>
                <span v-if="quickViewProgress.estimated_remaining_seconds > 0" class="progress-text">
                  预计剩余 {{ formatTime(quickViewProgress.estimated_remaining_seconds) }}
                </span>
              </div>

              <!-- 取消按钮 -->
              <el-button type="warning" size="small" @click="handleCancelQuickView">
                ⏸️ 取消
              </el-button>
            </template>

            <!-- 状态：ready - 已完成 -->
            <template v-else-if="quickViewStatus === 'ready'">
              <el-tag type="success" size="large">✅ 已预缓存</el-tag>
              <el-button type="danger" size="small" @click="handleClearQuickView">
                🗑️ 删除缓存
              </el-button>
            </template>

            <!-- 状态：failed - 失败 -->
            <template v-else-if="quickViewStatus === 'failed'">
              <el-tag type="danger" size="large">❌ 生成失败</el-tag>
              <el-button type="primary" size="small" @click="handleQuickView">
                🔄 重新生成
              </el-button>
            </template>

            <!-- 状态：preparing - 准备中 -->
            <template v-else-if="quickViewStatus === 'preparing' || quickViewStatus === 'prepared'">
              <el-tag type="warning" size="large">🔧 准备中...</el-tag>
              <el-button type="primary" size="small" @click="handleQuickView">
                📋 查看准备状态
              </el-button>
            </template>

            <!-- 状态：cancelled - 已取消 -->
            <template v-else-if="quickViewStatus === 'cancelled'">
              <el-tag type="info" size="large">⏸️ 已取消</el-tag>
              <el-button type="primary" size="small" @click="handleQuickView">
                🔄 重新生成
              </el-button>
            </template>
          </div>

          <!-- 模式切换 Toggle 开关 -->
          <div class="mode-switch-wrapper">
            <el-switch
              v-model="userSelectedMode"
              :disabled="!canUseMVT"
              active-value="mvt"
              inactive-value="geojson"
              active-text="⚡ MVT"
              inactive-text="🔵 GeoJSON"
              @change="handleModeSwitch"
            />
          </div>
        </template>
      </div>

      <!-- 动态切换地图渲染模式 -->
      <div v-if="showMap" class="map-container" :style="{ height: mapHeight + 'px' }">
        <!-- GeoJSON 模式（瓦片未就绪时使用） -->
        <GeoJSONPreview
          v-if="useGeoJSONMode"
          ref="mapRef"
          :engine-id="engineId"
          :schema="schema"
          :table="table"
          :geom="activeGeometryColumn"
          :page="currentPage"
          :page-size="pageSize"
          @featureClick="handleFeatureClick"
        />

        <!-- MVT 瓦片模式（瓦片已就绪） -->
        <VectorTilePreview
          v-else
          ref="mapRef"
          :engine-id="engineId"
          :schema="schema"
          :table="table"
          :geom="activeGeometryColumn"
          @featureClick="handleFeatureClick"
        />
      </div>

      <div v-if="showMap" class="map-splitter" @mousedown="startMapResize"></div>
    </template>

    <!-- 表格区域（保持原有分页逻辑）-->
    <div class="table-wrapper">
      <el-table
        ref="tableRef"
        :data="tableData"
        v-loading="loading"
        height="100%"
        highlight-current-row
        :row-key="getRowKey"
        :current-row-key="currentRowKey"
        @row-click="handleRowClick"
      >
        <el-table-column
          v-for="col in displayColumns"
          :key="col"
          :prop="col"
          :show-overflow-tooltip="!isGeometryColumn(col)"
        >
          <template #header>
            <div class="column-header">
              <span class="column-name">{{ col }}</span>
              <el-tooltip
                v-if="getColumnMetadata(col)"
                :content="getColumnTooltipContent(col)"
                placement="top"
                :show-after="300"
              >
                <el-icon class="column-info-icon"><InfoFilled /></el-icon>
              </el-tooltip>
            </div>
          </template>
          <template #default="scope">
            <!-- 几何列：显示截断内容 + tooltip（支持选择复制）-->
            <el-tooltip
              v-if="isGeometryColumn(col)"
              placement="top"
              :show-after="300"
              raw-content
              popper-class="geometry-tooltip"
            >
              <template #content>
                <div class="geometry-tooltip-content" v-text="getFullGeometryValue(scope.row, col)"></div>
              </template>
              <span class="geometry-cell">{{ getCellValue(scope.row, col) }}</span>
            </el-tooltip>
            <!-- 普通列：直接显示 -->
            <span v-else>{{ scope.row[col] }}</span>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 分页 -->
    <div v-if="total > 0" class="pagination">
      <el-pagination
        background
        layout="prev, pager, next"
        :total="total"
        :page-size="pageSize"
        :current-page="currentPage"
        @current-change="handlePageChange"
      />
      <div class="tip">最多展示前 50 行数据</div>
    </div>

    <!-- 准备状态检查对话框 (新增) -->
    <el-dialog
      v-model="showPreparationDialog"
      title="⚡ 预缓存配置"
      width="70%"
      :close-on-click-modal="false"
    >
      <!-- 部分1: 准备状态检查结果（非进行中时显示）-->
      <div v-if="!preparationInProgress" class="preparation-status-section">
        <!-- 加载中 ... -->
        <div v-if="preparationCheckResult === null" class="preparation-loading">
          <el-skeleton animated :rows="5" />
          <p style="text-align: center; margin-top: 12px; color: var(--addp-text-tertiary);">
            正在检查准备状态...
          </p>
        </div>

        <!-- 检查通过 ✅ -->
        <div v-else-if="preparationCheckResult?.overall_status === 'passed'">
          <el-alert
            type="success"
            title="✅ 准备已完成"
            description="所有准备条件已满足，可以开始生成瓦片"
            :closable="false"
            style="margin-bottom: 16px;"
          />

          <!-- 显示检查通过的详情（可选）-->
          <el-collapse>
            <el-collapse-item title="查看检查详情" name="1">
              <el-table :data="preparationCheckResult.checks" size="small" style="width: 100%;">
                <el-table-column prop="name" label="检查项" width="150">
                  <template #default="{ row }">
                    <span v-if="row.name === 'materialized_view'">📊 物化视图</span>
                    <span v-else-if="row.name === 'spatial_index'">🗂️ 空间索引</span>
                    <span v-else-if="row.name === 'analyze'">📈 统计信息</span>
                    <span v-else>{{ row.name }}</span>
                  </template>
                </el-table-column>
                <el-table-column prop="status" label="状态" width="100">
                  <template #default="{ row }">
                    <el-tag v-if="row.status === 'passed'" type="success">✅ 通过</el-tag>
                    <el-tag v-else-if="row.status === 'skipped'" type="info">⏭️ 跳过</el-tag>
                  </template>
                </el-table-column>
                <el-table-column prop="message" label="说明" />
              </el-table>
            </el-collapse-item>
          </el-collapse>
        </div>

        <!-- 检查失败 ❌ -->
        <div v-else-if="preparationCheckResult?.overall_status === 'failed'">
          <el-alert
            type="error"
            :title="`⚠️ 准备检查失败 (${preparationCheckResult.checks?.filter(c => c.status === 'failed').length || 0} 项未满足)`"
            description="需要执行准备工作来创建必要的数据库对象"
            :closable="false"
            style="margin-bottom: 16px;"
          />

          <!-- 失败项详情 -->
          <el-table :data="preparationCheckResult.checks" size="small" style="width: 100%; margin-bottom: 12px;">
            <el-table-column prop="name" label="检查项" width="150">
              <template #default="{ row }">
                <span v-if="row.name === 'materialized_view'">📊 物化视图</span>
                <span v-else-if="row.name === 'spatial_index'">🗂️ 空间索引</span>
                <span v-else-if="row.name === 'analyze'">📈 统计信息</span>
                <span v-else>{{ row.name }}</span>
              </template>
            </el-table-column>

            <el-table-column prop="status" label="状态" width="100">
              <template #default="{ row }">
                <el-tag v-if="row.status === 'passed'" type="success">✅ 通过</el-tag>
                <el-tag v-else-if="row.status === 'failed'" type="danger">❌ 失败</el-tag>
                <el-tag v-else type="info">⏭️ 跳过</el-tag>
              </template>
            </el-table-column>

            <el-table-column prop="message" label="说明" />
          </el-table>

          <!-- 提示信息 -->
          <el-alert
            type="info"
            title="💡 后端可自动准备"
            description="点击下方'准备预缓存'按钮，系统将自动创建物化视图、空间索引和更新统计信息"
            :closable="false"
          />
        </div>
      </div>

      <!-- 部分2: 准备工作进行中 -->
      <div v-if="preparationInProgress" class="preparation-progress-section">
        <el-alert
          type="info"
          title="🔧 准备工作进行中"
          description="后台正在创建物化视图、空间索引和更新统计信息，请耐心等待..."
          :closable="false"
          style="margin-bottom: 16px;"
        />
        <el-progress :percentage="50" style="margin-bottom: 12px;" />
        <p style="text-align: center; color: var(--addp-text-secondary); font-size: 14px;">
          处理中... 请勿关闭此对话框
        </p>
      </div>

      <!-- 底部按钮区 -->
      <template #footer>
        <el-button @click="showPreparationDialog = false">关闭</el-button>

        <!-- 诊断失败时：显示"准备"按钮 -->
        <el-button
          v-if="preparationCheckResult?.overall_status === 'failed' && !preparationInProgress"
          type="warning"
          :loading="preparationInProgress"
          @click="handlePrepareForCreateMVT"
        >
          🔧 准备预缓存
        </el-button>

        <!-- 诊断通过时：显示"生成"按钮 -->
        <el-button
          v-if="preparationCheckResult?.overall_status === 'passed' && !preparationInProgress"
          type="primary"
          @click="handleStartGeneration"
        >
          🚀 生成
        </el-button>
      </template>
    </el-dialog>

    <!-- 优化配置对话框 -->
    <el-dialog
      v-model="showOptimizationDialog"
      title="预缓存配置"
      width="650px"
      :close-on-click-modal="false"
    >
      <el-form v-if="tileConfigData" label-width="180px">
        <!-- 数据信息 -->
        <el-divider>数据信息</el-divider>
        <el-form-item label="记录数">
          <span>{{ tileConfigData.record_count?.toLocaleString() || '未知' }} 条</span>
        </el-form-item>
        <el-form-item label="空间范围">
          <span>{{ formatExtentValue(tileConfigData.extent) }}</span>
        </el-form-item>
        <el-form-item label="坐标系">
          <span>SRID {{ tileConfigData.srid || 4326 }}</span>
        </el-form-item>
        <el-form-item label="预估瓦片数">
          <span>{{ tileConfigData.estimated_tiles?.toLocaleString() || '未知' }} 个</span>
        </el-form-item>

        <!-- 缩放层级配置 -->
        <el-divider>缩放层级配置</el-divider>
        <el-form-item label="最小层级 (MinZoom)">
          <input
            id="config-min-zoom-input"
            type="number"
            :value="tileConfigData.min_zoom"
            min="0"
            max="22"
            style="width: 100%; padding: 8px 12px; border: 1px solid var(--addp-border-color); border-radius: 4px; font-size: 14px;"
          />
        </el-form-item>
        <el-form-item label="最大层级 (MaxZoom)">
          <input
            id="config-max-zoom-input"
            type="number"
            :value="tileConfigData.max_zoom"
            min="0"
            max="22"
            style="width: 100%; padding: 8px 12px; border: 1px solid var(--addp-border-color); border-radius: 4px; font-size: 14px;"
          />
        </el-form-item>

        <!-- 优化选项（可折叠） -->
        <el-divider content-position="left">
          <span style="cursor: pointer; user-select: none;" @click="optimizationExpanded = !optimizationExpanded">
            优化选项
            <el-icon style="margin-left: 4px;">
              <component :is="optimizationExpanded ? 'ArrowDown' : 'ArrowRight'" />
            </el-icon>
          </span>
        </el-divider>

        <!-- 优化选项内容（可折叠）- v3.0: 仅保留属性优化和大小限制 -->
        <template v-if="optimizationExpanded">
          <!-- Extent 分层优化配置 -->
          <el-divider>Extent 分层优化</el-divider>
          <el-form-item label="Max Zoom Extent">
            <el-input-number
              v-model="optimizationConfig.extent_optimization.max_zoom_extent"
              :min="256"
              :max="4096"
              :step="256"
            />
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 12px;">
              最大 Zoom 层的 Extent（默认 2048，通常无需修改）
            </span>
          </el-form-item>
          <el-form-item label="其他层 Extent">
            <el-input-number
              v-model="optimizationConfig.extent_optimization.base_extent"
              :min="256"
              :max="4096"
              :step="256"
            />
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 12px;">
              其他层的基础 Extent（默认 1024，可根据数据密度调整）
            </span>
          </el-form-item>
          <el-form-item label="最小 Extent">
            <el-input-number
              v-model="optimizationConfig.extent_optimization.min_extent"
              :min="64"
              :max="1024"
              :step="64"
            />
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 12px;">
              动态减半的最小 Extent（默认 256，低于此值不再减半）
            </span>
          </el-form-item>

          <!-- 属性优化 -->
          <el-divider>属性优化</el-divider>
          <el-form-item label="属性优化">
            <el-switch v-model="optimizationConfig.attribute_pruning.enabled" />
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 12px;">
              低层级仅返回主键，减少数据量
            </span>
          </el-form-item>
          <el-form-item
            v-if="optimizationConfig.attribute_pruning.enabled"
            label="属性分界层级"
          >
            <el-input-number
              v-model="optimizationConfig.attribute_pruning.zoom_threshold"
              :min="0"
              :max="18"
            />
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 12px;">
              z0-z{{ optimizationConfig.attribute_pruning.zoom_threshold }}: 仅主键 |
              z{{ optimizationConfig.attribute_pruning.zoom_threshold + 1 }}+: 全部属性
            </span>
          </el-form-item>

          <!-- 瓦片大小阈值 -->
          <el-divider>瓦片大小限制</el-divider>
          <el-form-item label="最大大小 (MB)">
            <el-input-number
              v-model="optimizationConfig.tile_size_thresholds.max_size_mb"
              :min="0.5"
              :max="20"
              :step="0.5"
              :precision="1"
            />
            <span style="margin-left: 12px; color: var(--addp-text-tertiary); font-size: 12px;">
              瓦片大小超过此值时，自动降低分辨率直到符合要求（默认 5MB）
            </span>
          </el-form-item>

          <!-- 优化说明 -->
          <el-alert type="info" style="margin-top: 20px;" :closable="false">
            <strong>v4.0 优化流程</strong>（自动执行）<br/>
            📋 准备阶段：物化视图 → 空间索引 → ANALYZE<br/>
            1️⃣ 属性优化：根据 zoom 级别过滤属性<br/>
            2️⃣ 分层 Extent：MaxZoom 层 {{ optimizationConfig.extent_optimization.max_zoom_extent }}，其他层 {{ optimizationConfig.extent_optimization.base_extent }}<br/>
            3️⃣ 动态减半：若瓦片超过 {{ optimizationConfig.tile_size_thresholds.max_size_mb }}MB，自动降低分辨率（最低 {{ optimizationConfig.extent_optimization.min_extent }}）
          </el-alert>
        </template>
      </el-form>

      <template #footer>
        <el-button @click="resetOptimizationConfig">恢复默认</el-button>
        <el-button @click="handleOptimizationConfigCancel">取消</el-button>
        <el-button type="primary" @click="handleOptimizationConfigConfirm">开始生成</el-button>
      </template>
    </el-dialog>

    <!-- 准备失败提示对话框 (v4.0 新增) -->
    <el-dialog
      v-model="showPreparationFailedDialog"
      title="⚠️ 准备阶段检查失败"
      width="600px"
      :close-on-click-modal="false"
    >
      <div v-if="preparationStatus" class="preparation-status">
        <div v-for="check in preparationStatus.checks" :key="check.name" class="check-item">
          <div v-if="check.status === 'failed'" class="failed-check">
            <span style="color: #f56c6c; font-weight: bold;">❌ {{ check.message }}</span>
            <div v-if="check.details" style="margin-top: 8px; padding-left: 20px; font-size: 12px; color: var(--addp-text-secondary);">
              <div v-for="(value, key) in check.details" :key="key">
                <strong>{{ key }}:</strong> {{ value }}
              </div>
            </div>
          </div>
          <div v-else-if="check.status === 'passed'" class="passed-check">
            <span style="color: #67c23a; font-weight: bold;">✅ {{ check.message }}</span>
          </div>
          <div v-else-if="check.status === 'skipped'" class="skipped-check">
            <span style="color: var(--addp-text-tertiary); font-weight: bold;">⏭️ {{ check.message }}</span>
          </div>
        </div>

        <el-alert type="warning" style="margin-top: 16px;" :closable="false">
          <strong>操作建议:</strong><br/>
          • 点击"重新检查"重新执行准备阶段<br/>
          • 修复问题后再次尝试<br/>
          • 或点击"完成准备"跳过失败项继续生成（可能影响性能）
        </el-alert>
      </div>

      <template #footer>
        <el-button @click="handleRecheckPreparation">重新检查</el-button>
        <el-button @click="handleCompletePreparation">完成准备</el-button>
        <el-button @click="handleCancelPreparation">取消</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { InfoFilled, ArrowDown, ArrowRight } from '@element-plus/icons-vue'
import { useResizable } from '@/composables/useResizable'
import VectorTilePreview from '@/components/map/VectorTilePreview.vue'
import GeoJSONPreview from '@/components/map/GeoJSONPreview.vue'
import { dataExplorerAPI } from '@/api/dataExplorer'
import { quickViewAPI } from '@/api/quickView'
import { ElMessage, ElMessageBox } from 'element-plus'

const props = defineProps({
  data: {
    type: Object,
    required: true
  },
  loading: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['page-change'])

const { size: mapHeight, startResize: startMapResize } = useResizable(260, 140, 520, 'vertical')

const tableRef = ref(null)
const mapRef = ref(null)
const showMap = ref(false)  // 默认不显示地图,避免初始渲染延迟
const currentRowKey = ref('')
const currentPage = ref(1)
const pageSize = ref(10)

// Pre-Cache state
const quickViewLoading = ref(false)
const quickViewStatus = ref('none') // 'none' | 'generating' | 'ready' | 'failed'
const quickViewProgress = ref(null) // 存储实时进度数据
let quickViewPollingTimer = null

// 用户偏好的显示模式（从 API 响应中初始化）
const preferredMode = ref('mvt')

// 用户当前选择的显示模式（用于 Toggle 开关双向绑定）
const userSelectedMode = ref('mvt')

// 优化配置对话框状态
const showOptimizationDialog = ref(false)
const tileConfigData = ref(null) // 存储 getTileConfig 的结果
const optimizationExpanded = ref(false) // 优化选项折叠状态（默认收起）

// 优化配置（默认值） - v4.0: 保留 Extent 优化、属性优化和大小阈值，加入准备阶段检查
const optimizationConfig = ref({
  version: '4.0',
  extent_optimization: {
    max_zoom_extent: 1024,  // Max Zoom 层 Extent
    base_extent: 512,       // 其他层 Extent
    min_extent: 256         // 最小 Extent
  },
  attribute_pruning: {
    enabled: true,
    zoom_threshold: 8
  },
  tile_size_thresholds: {
    max_size_mb: 5.0
  }
})

// 准备阶段状态 (v4.0 新增)
const preparationStatus = ref(null) // 存储准备阶段检查结果
const showPreparationFailedDialog = ref(false) // 准备失败对话框

// 准备阶段新增状态变量（支持三步工作流）
const showPreparationDialog = ref(false) // 是否显示准备对话框
const preparationCheckResult = ref(null) // 准备检查的结果
const preparationInProgress = ref(false) // 准备工作是否进行中
const canStartGeneration = ref(false) // 是否可以开始生成
const prepareTaskId = ref(null) // 准备任务ID
let prepareProgressPollingTimer = null // 轮询定时器

const columns = computed(() => props.data?.columns || [])
const rows = computed(() => props.data?.rows || [])
const total = computed(() => props.data?.total || 0)
const geometryColumns = computed(() => props.data?.geometry_columns || [])
const srid = computed(() => props.data?.srid || 0)
const extent = computed(() => props.data?.extent || [])

const hasGeometry = computed(() => geometryColumns.value.length > 0)
const activeGeometryColumn = computed(() => geometryColumns.value[0] || '')

// 地图渲染模式选择（支持用户偏好 + 自动切换）
const useGeoJSONMode = computed(() => {
  const status = quickViewStatus.value

  // 预缓存未完成，强制 GeoJSON 模式
  if (status === 'none' || status === 'generating' || status === 'cancelled' || status === 'failed') {
    return true
  }

  // 预缓存已完成，根据用户选择
  if (status === 'ready') {
    return userSelectedMode.value === 'geojson'
  }

  return true
})

// 是否允许使用 MVT 模式
const canUseMVT = computed(() => {
  return quickViewStatus.value === 'ready'
})

// 处理模式切换
const handleModeSwitch = async (newMode) => {
  if (!canUseMVT.value && newMode === 'mvt') {
    ElMessage.warning('预缓存未完成，无法切换到 MVT 模式')
    userSelectedMode.value = 'geojson'
    return
  }

  try {
    await quickViewAPI.updatePreferredMode(engineId.value, schema.value, table.value, newMode)
    preferredMode.value = newMode
    ElMessage.success(`已切换到 ${newMode === 'mvt' ? 'MVT 瓦片' : 'GeoJSON'} 模式`)
  } catch (error) {
    console.error('切换显示模式失败:', error)
    ElMessage.error('切换显示模式失败: ' + (error.response?.data?.error || error.message))
    userSelectedMode.value = newMode === 'mvt' ? 'geojson' : 'mvt'
  }
}

// 删除预缓存
const handleClearQuickView = async () => {
  try {
    await ElMessageBox.confirm(
      '删除预缓存后，地图将切换到 GeoJSON 模式，确认删除？',
      '确认删除',
      {
        confirmButtonText: '确认删除',
        cancelButtonText: '取消',
        type: 'warning',
        confirmButtonClass: 'el-button--danger'
      }
    )

    quickViewLoading.value = true
    await quickViewAPI.clearQuickView(engineId.value, schema.value, table.value)

    quickViewStatus.value = 'none'
    quickViewProgress.value = null
    preferredMode.value = 'geojson'
    userSelectedMode.value = 'geojson'

    stopQuickViewPolling()
    ElMessage.success('预缓存已删除')
  } catch (error) {
    if (error === 'cancel') return
    console.error('删除预缓存失败:', error)
    ElMessage.error('删除预缓存失败: ' + (error.response?.data?.error || error.message))
  } finally {
    quickViewLoading.value = false
  }
}

// 取消预缓存生成
const handleCancelQuickView = async () => {
  try {
    await ElMessageBox.confirm('确认取消预缓存生成任务？', '确认取消', {
      confirmButtonText: '确认',
      cancelButtonText: '取消',
      type: 'warning'
    })

    quickViewLoading.value = true
    await quickViewAPI.cancelQuickView(engineId.value, schema.value, table.value)

    quickViewStatus.value = 'cancelled'
    quickViewProgress.value = null
    stopQuickViewPolling()

    ElMessage.success('已取消预缓存生成')
  } catch (error) {
    if (error === 'cancel') return
    console.error('取消预缓存失败:', error)
    ElMessage.error('取消预缓存失败: ' + (error.response?.data?.error || error.message))
  } finally {
    quickViewLoading.value = false
  }
}

// 资源信息（用于 MVT 预览）
const engineId = computed(() => props.data?.engineId)
const schema = computed(() => props.data?.schema)
const table = computed(() => {
  const rawTable = props.data?.table || props.data?.path
  // 如果table包含schema前缀（如 "public.dltb"），去除前缀
  if (rawTable && rawTable.includes('.')) {
    return rawTable.split('.').pop()
  }
  return rawTable
})

// 显示所有列（包括几何列）
const displayColumns = computed(() => {
  return columns.value || []
})

// 列元数据相关
const columnMetadata = computed(() => props.data?.column_metadata || [])

// 获取主键列名（从 column_metadata 中查找 is_primary_key 为 true 的列）
const primaryKeyColumn = computed(() => {
  const pkColumn = columnMetadata.value.find(meta => meta.is_primary_key)
  return pkColumn ? pkColumn.column_name : null
})

// 根据列名获取列元数据
const getColumnMetadata = (columnName) => {
  return columnMetadata.value.find(meta => meta.column_name === columnName)
}

// 生成列的 Tooltip 内容
const getColumnTooltipContent = (columnName) => {
  const meta = getColumnMetadata(columnName)
  if (!meta) return ''

  const parts = [
    `列名: ${meta.column_name}`,
    `类型: ${meta.data_type}`,
    `可空: ${meta.is_nullable ? '是' : '否'}`,
    `主键: ${meta.is_primary_key ? '是' : '否'}`
  ]

  if (meta.comment) {
    parts.push(`注释: ${meta.comment}`)
  }

  return parts.join('\n')
}

// 判断列是否为几何列
const isGeometryColumn = (columnName) => {
  return geometryColumns.value.includes(columnName)
}

// 截断几何列显示（只显示前几个字符）
const truncateGeometry = (value) => {
  if (value == null) return ''
  const str = String(value)
  const maxLength = 50
  if (str.length > maxLength) {
    return str.substring(0, maxLength) + '...'
  }
  return str
}

// 获取单元格显示值
const getCellValue = (row, columnName) => {
  const value = row[columnName]
  if (isGeometryColumn(columnName)) {
    return truncateGeometry(value)
  }
  return value
}

// 获取完整的几何值（用于 tooltip）
const getFullGeometryValue = (row, columnName) => {
  return row[columnName] || ''
}

// 生成行键
const tableData = computed(() => {
  const baseKey = `${props.data?.engineId || 'res'}-${props.data?.schema || 'schema'}-${props.data?.table || 'table'}`
  return rows.value.map((row, index) => ({
    ...row,
    __rowKey: `${baseKey}-${(currentPage.value - 1) * pageSize.value + index}`
  }))
})

const getRowKey = (row) => {
  return row?.__rowKey || row?.id || row?.ID || row?._id || row?.uuid || String(Math.random())
}

const handleRowClick = async (row) => {
  currentRowKey.value = row?.__rowKey || ''
  if (tableRef.value) {
    tableRef.value.setCurrentRow(row)
  }

  // MVT 预览模式下，通过 API 查询要素完整几何并高亮显示
  if (hasGeometry.value && showMap.value && mapRef.value) {
    // 从 column_metadata 中获取真正的主键列名
    const pkColumn = primaryKeyColumn.value
    if (!pkColumn) {
      console.warn('数据表缺少主键定义，无法定位到地图')
      return
    }

    const featureId = row[pkColumn]
    if (!featureId) {
      console.warn(`表格行缺少主键值 (${pkColumn})，无法定位到地图`)
      return
    }

    try {
      // 调用后端 API 查询要素完整几何
      const response = await dataExplorerAPI.getFeatureGeometry(
        engineId.value,
        schema.value,
        table.value,
        featureId,
        activeGeometryColumn.value,
        pkColumn  // 使用真正的主键列名
      )

      // 注意：createAPIClient 默认 extractData=true，已自动提取了 response.data
      const { geojson, centroid, extent } = response
      // 调用地图组件的高亮方法，传入extent用于自适应缩放
      if (mapRef.value && typeof mapRef.value.focusFeatureById === 'function') {
        mapRef.value.focusFeatureById(featureId, geojson, centroid, extent)
      }
    } catch (error) {
      console.error('查询要素几何失败:', error)
      // 用户点击行时如果查询失败，只记录错误不弹窗，避免打断交互
    }
  }
}

// 处理地图要素点击事件（地图→表格选中）
const handleFeatureClick = (featureId) => {
  console.log('handleFeatureClick called with featureId:', featureId)
  console.log('Current tableData:', tableData.value)

  // 从 column_metadata 中获取真正的主键列名
  const pkColumn = primaryKeyColumn.value
  if (!pkColumn) {
    console.warn('数据表缺少主键定义，无法定位表格行')
    return
  }

  // 在当前页查找对应的行
  const targetRow = tableData.value.find(row => {
    const rowId = row[pkColumn]
    console.log(`Comparing row ${pkColumn}:`, rowId, 'with featureId:', featureId)
    return String(rowId) === String(featureId)
  })

  console.log('Found target row:', targetRow)

  if (targetRow) {
    currentRowKey.value = targetRow.__rowKey
    if (tableRef.value) {
      tableRef.value.setCurrentRow(targetRow)
      // 滚动到对应行
      const tableBody = document.querySelector('.el-table__body-wrapper')
      if (tableBody) {
        const rowElement = tableBody.querySelector(`[data-row-key="${targetRow.__rowKey}"]`)
        if (rowElement) {
          rowElement.scrollIntoView({ behavior: 'smooth', block: 'center' })
        }
      }
    }
    console.log('Successfully set current row to:', targetRow)
  } else {
    console.warn('当前页未找到ID为', featureId, '的行')
    console.warn('当前页的所有ID:', tableData.value.map(row => row.id || row.ID || row._id || row.uuid))
  }
}

const handlePageChange = (page) => {
  currentPage.value = page
  emit('page-change', page)
}

// Pre-Cache 处理函数 (获取配置并显示对话框)
const handleQuickView = async () => {
  if (!engineId.value || !schema.value || !table.value) {
    ElMessage.warning('缺少必要参数，无法启用预缓存')
    return
  }

  if (quickViewStatus.value === 'generating') {
    ElMessage.info('预缓存正在生成中，请稍后')
    return
  }

  try {
    // 显示准备对话框
    showPreparationDialog.value = true
    quickViewLoading.value = true
    preparationCheckResult.value = null
    preparationInProgress.value = false
    canStartGeneration.value = false

    // 执行准备检查（诊断）
    const checkResult = await quickViewAPI.checkPreparation(engineId.value, schema.value, table.value)
    preparationCheckResult.value = checkResult

    // 根据结果状态设置标志位
    if (checkResult.overall_status === 'passed') {
      canStartGeneration.value = true
    } else {
      canStartGeneration.value = false
    }
  } catch (error) {
    console.error('检查准备状态失败:', error)
    ElMessage.error(error.response?.data?.error || '检查准备状态失败')
    showPreparationDialog.value = false
  } finally {
    quickViewLoading.value = false
  }
}

// 启动准备工作（创建物化视图、索引、ANALYZE）
const handlePrepareForCreateMVT = async () => {
  try {
    preparationInProgress.value = true
    ElMessage.info('准备工作已启动，请耐心等待...')

    // 调用准备API
    const result = await quickViewAPI.prepareForCreateMVT(engineId.value, schema.value, table.value)
    prepareTaskId.value = result.fingerprint

    // 开始轮询准备进度
    startPrepareProgressPolling()
  } catch (error) {
    console.error('启动准备工作失败:', error)
    ElMessage.error(error.response?.data?.error || '启动准备工作失败')
    preparationInProgress.value = false
  }
}

// 轮询准备进度
const startPrepareProgressPolling = () => {
  // 清除旧的轮询定时器
  if (prepareProgressPollingTimer) {
    clearInterval(prepareProgressPollingTimer)
  }

  prepareProgressPollingTimer = setInterval(async () => {
    try {
      const status = await quickViewAPI.getQuickViewStatus(engineId.value, schema.value, table.value)

      // 检查准备状态字段
      if (status.preparation_status) {
        const prepStatus = status.preparation_status

        if (prepStatus.overall_status === 'passed') {
          // 准备成功
          clearInterval(prepareProgressPollingTimer)
          prepareProgressPollingTimer = null
          preparationInProgress.value = false
          canStartGeneration.value = true
          preparationCheckResult.value = prepStatus
          ElMessage.success('准备工作已完成，可以开始生成')
        } else if (prepStatus.overall_status === 'failed') {
          // 准备失败
          clearInterval(prepareProgressPollingTimer)
          prepareProgressPollingTimer = null
          preparationInProgress.value = false
          preparationCheckResult.value = prepStatus
          ElMessage.error('准备工作失败，请检查数据库权限或磁盘空间')
        }
        // 否则继续轮询
      } else if (status.status === 'prepared') {
        // 备选方案：如果 preparation_status 为空但 status='prepared'，也表示准备完成
        clearInterval(prepareProgressPollingTimer)
        prepareProgressPollingTimer = null
        preparationInProgress.value = false
        canStartGeneration.value = true
        ElMessage.success('准备工作已完成，可以开始生成')
      }
    } catch (error) {
      console.error('查询准备进度失败:', error)
    }
  }, 2000) // 每2秒轮询一次
}

// 启动生成（在准备通过后）
const handleStartGeneration = async () => {
  try {
    // 获取瓦片配置（计算 minZoom 和 maxZoom）
    const tileConfig = await quickViewAPI.getTileConfig(engineId.value, schema.value, table.value)

    // 保存配置数据
    tileConfigData.value = tileConfig

    // 关闭准备对话框
    showPreparationDialog.value = false

    // 显示优化配置对话框
    showOptimizationDialog.value = true
  } catch (error) {
    console.error('获取瓦片配置失败:', error)
    ElMessage.error(error.response?.data?.error || '获取瓦片配置失败')
  }
}

// 处理优化配置对话框的确认
const handleOptimizationConfigConfirm = async () => {
  try {
    // 获取用户输入的缩放层级
    const minZoomInput = document.getElementById('config-min-zoom-input')
    const maxZoomInput = document.getElementById('config-max-zoom-input')

    if (!minZoomInput || !maxZoomInput) {
      ElMessage.error('配置项加载失败')
      return
    }

    const minZoom = parseInt(minZoomInput.value, 10)
    const maxZoom = parseInt(maxZoomInput.value, 10)

    // 验证缩放层级
    if (isNaN(minZoom) || isNaN(maxZoom)) {
      ElMessage.error('请输入有效的缩放层级')
      return
    }

    if (minZoom < 0 || maxZoom < 0 || minZoom > 22 || maxZoom > 22) {
      ElMessage.error('缩放层级必须在 0-22 之间')
      return
    }

    if (minZoom > maxZoom) {
      ElMessage.error('最小层级不能大于最大层级')
      return
    }

    // 提交任务
    await quickViewAPI.triggerQuickView(engineId.value, schema.value, table.value, {
      min_zoom: minZoom,
      max_zoom: maxZoom,
      priority: 'default',
      optimization_config: optimizationConfig.value
    })

    quickViewStatus.value = 'generating'
    ElMessage.success(`预缓存任务已启动 (z${minZoom}-z${maxZoom})，正在后台生成`)

    // 关闭对话框
    showOptimizationDialog.value = false

    // 开始轮询状态
    startQuickViewPolling()
  } catch (error) {
    console.error('启用预缓存失败:', error)
    ElMessage.error(error.response?.data?.error || '启用预缓存失败')
  }
}

// 处理优化配置对话框的取消
const handleOptimizationConfigCancel = () => {
  showOptimizationDialog.value = false
}

// 重置优化配置到默认值
const resetOptimizationConfig = () => {
  optimizationConfig.value = {
    version: '4.0',
    extent_optimization: {
      max_zoom_extent: 1024,
      base_extent: 512,
      min_extent: 256
    },
    attribute_pruning: {
      enabled: true,
      zoom_threshold: 8
    },
    tile_size_thresholds: {
      max_size_mb: 5.0
    }
  }
  ElMessage.success('已恢复默认配置')
}

// 准备失败处理：重新检查 (v4.0 新增)
const handleRecheckPreparation = async () => {
  try {
    showPreparationFailedDialog.value = false
    quickViewLoading.value = true
    // 重新触发快显任务（会重新执行准备检查）
    const minZoomInput = document.getElementById('config-min-zoom-input')
    const maxZoomInput = document.getElementById('config-max-zoom-input')
    const minZoom = parseInt(minZoomInput?.value || 0, 10)
    const maxZoom = parseInt(maxZoomInput?.value || 18, 10)

    await quickViewAPI.triggerQuickView(engineId.value, schema.value, table.value, {
      min_zoom: minZoom,
      max_zoom: maxZoom,
      priority: 'default',
      optimization_config: optimizationConfig.value
    })

    quickViewStatus.value = 'generating'
    ElMessage.success('准备阶段检查已重新启动')
    startQuickViewPolling()
  } catch (error) {
    console.error('重新检查失败:', error)
    ElMessage.error('重新检查失败: ' + (error.response?.data?.error || error.message))
  } finally {
    quickViewLoading.value = false
  }
}

// 准备失败处理：完成准备 (v4.0 新增)
const handleCompletePreparation = async () => {
  try {
    showPreparationFailedDialog.value = false
    quickViewLoading.value = true
    ElMessage.warning('用户确认跳过失败项，继续生成（可能影响性能）')
    // 关闭优化配置对话框，继续进行
    showOptimizationDialog.value = false
    // 继续生成
    startQuickViewPolling()
  } catch (error) {
    console.error('完成准备失败:', error)
    ElMessage.error('完成准备失败')
  } finally {
    quickViewLoading.value = false
  }
}

// 准备失败处理：取消 (v4.0 新增)
const handleCancelPreparation = () => {
  showPreparationFailedDialog.value = false
  quickViewStatus.value = 'none'
  quickViewProgress.value = null
  ElMessage.info('已取消预缓存任务')
}

// 格式化空间范围
const formatExtentValue = (extent) => {
  if (!extent || extent.length !== 4) return '未知'
  return `[${extent[0].toFixed(2)}, ${extent[1].toFixed(2)}, ${extent[2].toFixed(2)}, ${extent[3].toFixed(2)}]`
}

// 获取预缓存状态
const fetchQuickViewStatus = async () => {
  if (!engineId.value || !schema.value || !table.value) {
    return
  }

  try {
    // 注意: createAPIClient 默认 extractData=true，已自动提取 response.data
    const response = await quickViewAPI.getQuickViewStatus(engineId.value, schema.value, table.value)
    const status = response?.status
    const progress = response?.progress // 提取进度数据
    const mode = response?.preferred_mode // 新增：读取用户偏好模式

    if (status) {
      quickViewStatus.value = status

      // 保存进度数据（只在 generating 状态下有效）
      if (status === 'generating' && progress) {
        quickViewProgress.value = progress
      } else {
        quickViewProgress.value = null
      }

      // 新增：初始化用户偏好模式
      if (mode) {
        preferredMode.value = mode
        userSelectedMode.value = mode
      } else {
        // 如果后端未返回，根据状态推断
        if (status === 'ready') {
          preferredMode.value = 'mvt'
          userSelectedMode.value = 'mvt'
        } else {
          preferredMode.value = 'geojson'
          userSelectedMode.value = 'geojson'
        }
      }

      // 如果状态变为完成或失败，停止轮询
      if (status === 'ready' || status === 'failed') {
        stopQuickViewPolling()

        if (status === 'ready') {
          ElMessage.success('预缓存生成完成')
        } else if (status === 'failed') {
          ElMessage.error('预缓存生成失败')
          preferredMode.value = 'geojson'
          userSelectedMode.value = 'geojson'
        }
      }
    }
  } catch (error) {
    console.error('Failed to fetch quick view status:', error)
    // 如果返回404，说明还没有预缓存记录
    if (error.response?.status === 404) {
      quickViewStatus.value = 'none'
      quickViewProgress.value = null
      preferredMode.value = 'geojson'
      userSelectedMode.value = 'geojson'
    }
    // 其他错误不处理，避免频繁弹窗
  }
}

// 开始轮询预缓存状态
const startQuickViewPolling = () => {
  stopQuickViewPolling() // 先清除旧的定时器

  quickViewPollingTimer = setInterval(() => {
    fetchQuickViewStatus()
  }, 2000) // 每2秒查询一次（用户要求）
}

// 停止轮询
const stopQuickViewPolling = () => {
  if (quickViewPollingTimer) {
    clearInterval(quickViewPollingTimer)
    quickViewPollingTimer = null
  }
}

// 获取按钮文本（根据状态和进度）
const getQuickViewButtonText = () => {
  if (quickViewStatus.value === 'ready') {
    return '预缓存已启用'
  }
  if (quickViewStatus.value === 'generating') {
    if (quickViewProgress.value && quickViewProgress.value.progress_percent !== undefined) {
      return `生成中... ${quickViewProgress.value.progress_percent.toFixed(1)}%`
    }
    return '生成中...'
  }
  return '启用预缓存'
}

// 格式化时间（秒数转为可读格式）
const formatTime = (seconds) => {
  if (!seconds || seconds < 0) return '0秒'

  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = Math.floor(seconds % 60)

  if (hours > 0) {
    return `${hours}小时${minutes}分${secs}秒`
  } else if (minutes > 0) {
    return `${minutes}分${secs}秒`
  } else {
    return `${secs}秒`
  }
}

// 监听表变化，重新获取预缓存状态
watch(
  () => [engineId.value, schema.value, table.value],
  () => {
    if (hasGeometry.value && engineId.value && schema.value && table.value) {
      fetchQuickViewStatus()
    }
  },
  { immediate: true }
)

onMounted(async () => {
  // 初始化时获取预缓存状态
  if (hasGeometry.value && engineId.value && schema.value && table.value) {
    await fetchQuickViewStatus()

    // 如果状态是 generating，自动启动轮询
    if (quickViewStatus.value === 'generating') {
      startQuickViewPolling()
    }
  }
})

onUnmounted(() => {
  stopQuickViewPolling()
})
</script>

<style scoped>
.table-preview {
  display: flex;
  flex-direction: column;
  gap: 12px;
  height: 100%;
}

.map-controls {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 12px;
  background: var(--el-fill-color);
  border-radius: 4px;
}

.toggle-wrapper {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.toggle-label {
  white-space: nowrap;
}

.map-container {
  position: relative;
  width: 100%;
  border-radius: 4px;
  overflow: hidden;
  background: var(--addp-bg-secondary);
}

.map-splitter {
  height: 8px;
  cursor: row-resize;
  position: relative;
  margin: -4px 0 4px;
}

.map-splitter::after {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  transform: translateY(-50%);
  height: 2px;
  background: var(--el-color-primary-light-8);
  border-radius: 2px;
}

.map-splitter:hover::after,
body.is-resizing .map-splitter::after {
  background: var(--el-color-primary);
}

.column-header {
  display: flex;
  align-items: center;
  gap: 6px;
  justify-content: center;
}

.column-name {
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.column-info-icon {
  font-size: 14px;
  color: var(--el-color-info);
  cursor: help;
  transition: color 0.2s;
}

.column-info-icon:hover {
  color: var(--el-color-primary);
}

/* 几何列单元格样式 */
.geometry-cell {
  display: inline-block;
  font-family: 'Courier New', monospace;
  font-size: 12px;
  color: var(--el-color-warning);
  cursor: help;
  padding: 2px 6px;
  background-color: var(--el-fill-color-light);
  border-radius: 3px;
  border: 1px solid var(--el-color-warning-light-5);
  transition: all 0.2s;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 100%;
}

.geometry-cell:hover {
  background-color: var(--el-color-warning-light-9);
  border-color: var(--el-color-warning-light-3);
}

.table-wrapper {
  flex: 1 1 auto;
  min-height: 220px;
  display: flex;
  flex-direction: column;
}

.table-wrapper :deep(.el-table) {
  flex: 1;
}

.pagination {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.pagination .tip {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.progress-info {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 12px;
  color: var(--el-text-color-regular);
}

.progress-text {
  padding: 4px 8px;
  background: var(--el-fill-color-light);
  border-radius: 4px;
  white-space: nowrap;
}

/* 预缓存控制区 */
.pre-cache-controls {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
}

/* 进度条容器 */
.progress-bar-wrapper {
  min-width: 200px;
  max-width: 300px;
  flex: 1;
}

/* 模式切换容器 */
.mode-switch-wrapper {
  display: flex;
  align-items: center;
  padding-left: 12px;
  border-left: 1px solid var(--el-border-color);
}

.mode-switch-wrapper .el-switch {
  --el-switch-on-color: var(--el-color-success);
  --el-switch-off-color: var(--el-color-primary);
}

.mode-switch-wrapper .el-switch.is-disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 准备状态对话框样式 */
.preparation-status-section {
  padding: 12px 0;
}

.preparation-loading {
  padding: 20px;
}

.preparation-progress-section {
  padding: 12px 0;
  text-align: center;
}

.check-item {
  padding: 8px 0;
  border-bottom: 1px solid var(--el-border-color);
}

.check-item:last-child {
  border-bottom: none;
}

.failed-check {
  padding: 8px 12px;
  background-color: var(--el-fill-color);
  border-left: 3px solid #f56c6c;
  border-radius: 4px;
}

.passed-check {
  padding: 8px 12px;
  background-color: var(--el-fill-color);
  border-left: 3px solid #67c23a;
  border-radius: 4px;
}

.skipped-check {
  padding: 8px 12px;
  background-color: var(--el-fill-color);
  border-left: 3px solid var(--addp-text-tertiary);
  border-radius: 4px;
}
</style>

<style>
/* 几何 tooltip 全局样式（不带 scoped，因为 tooltip 挂载在 body 上）*/
.geometry-tooltip {
  max-width: 600px !important;
}

.geometry-tooltip .el-popper__arrow {
  display: none;
}

.geometry-tooltip-content {
  font-family: 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.6;
  color: #333;
  background-color: var(--addp-bg-primary);
  padding: 12px;
  border-radius: 4px;
  max-height: 400px;
  overflow: auto;
  word-break: break-all;
  white-space: pre-wrap;
  user-select: text;
  cursor: text;
}

/* 滚动条样式 */
.geometry-tooltip-content::-webkit-scrollbar {
  width: 6px;
  height: 6px;
}

.geometry-tooltip-content::-webkit-scrollbar-thumb {
  background-color: rgba(0, 0, 0, 0.2);
  border-radius: 3px;
}

.geometry-tooltip-content::-webkit-scrollbar-track {
  background-color: rgba(0, 0, 0, 0.05);
}
</style>
