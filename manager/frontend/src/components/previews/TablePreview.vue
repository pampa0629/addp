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
            style="width: 100%; padding: 8px 12px; border: 1px solid #dcdfe6; border-radius: 4px; font-size: 14px;"
          />
        </el-form-item>
        <el-form-item label="最大层级 (MaxZoom)">
          <input
            id="config-max-zoom-input"
            type="number"
            :value="tileConfigData.max_zoom"
            min="0"
            max="22"
            style="width: 100%; padding: 8px 12px; border: 1px solid #dcdfe6; border-radius: 4px; font-size: 14px;"
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

        <!-- 优化选项内容（可折叠） -->
        <template v-if="optimizationExpanded">
          <!-- 属性优化 -->
          <el-form-item label="属性优化">
            <el-switch v-model="optimizationConfig.attribute_pruning.enabled" />
            <span style="margin-left: 12px; color: #909399; font-size: 12px;">
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
            <span style="margin-left: 12px; color: #909399; font-size: 12px;">
              z0-z{{ optimizationConfig.attribute_pruning.zoom_threshold }}: 仅主键 |
              z{{ optimizationConfig.attribute_pruning.zoom_threshold + 1 }}+: 全部属性
            </span>
          </el-form-item>

          <!-- 瓦片大小阈值 -->
          <el-divider>瓦片大小阈值</el-divider>
          <el-form-item label="不优化阈值 (MB)">
            <el-input-number
              v-model="optimizationConfig.tile_size_thresholds.no_optimization_mb"
              :min="0.5"
              :max="10"
              :step="0.5"
              :precision="1"
            />
            <span style="margin-left: 12px; color: #909399; font-size: 12px;">
              小于此值不进行任何优化（默认 2MB）
            </span>
          </el-form-item>
          <el-form-item label="停止优化阈值 (MB)">
            <el-input-number
              v-model="optimizationConfig.tile_size_thresholds.stop_optimization_mb"
              :min="1"
              :max="20"
              :step="0.5"
              :precision="1"
            />
            <span style="margin-left: 12px; color: #909399; font-size: 12px;">
              达到此值后跳过几何优化（默认 5MB）
            </span>
          </el-form-item>

          <!-- 优化参数（高级） -->
          <el-divider>优化参数（高级）</el-divider>
          <el-collapse>
          <!-- Extent 优化 -->
          <el-collapse-item title="1. 瓦片分辨率优化" name="extent">
            <el-form-item label="模糊度">
              <el-radio-group v-model="optimizationConfig.extent_optimization.blur_level">
                <el-radio :label="1">清晰（Extent: 4096）</el-radio>
                <el-radio :label="2">适中（Extent: 2048，推荐）</el-radio>
                <el-radio :label="4">模糊（Extent: 1024）</el-radio>
              </el-radio-group>
              <div style="color: #909399; font-size: 12px; margin-top: 8px;">
                模糊度越高，瓦片越小，但边界越不精细
              </div>
            </el-form-item>
          </el-collapse-item>

          <!-- 对象采样 -->
          <el-collapse-item title="2. 对象采样优化" name="sampling">
            <el-form-item label="面/线保留策略">
              <div style="margin-bottom: 16px;">
                <label style="font-size: 13px;">面积/长度占比：</label>
                <el-slider
                  v-model="optimizationConfig.sampling.polygon_line.cumulative_size_ratio"
                  :min="0.5"
                  :max="1.0"
                  :step="0.05"
                  :format-tooltip="(val) => `${(val * 100).toFixed(0)}%`"
                />
                <span style="color: #909399; font-size: 12px;">
                  保留累计占总面积/长度 {{ (optimizationConfig.sampling.polygon_line.cumulative_size_ratio * 100).toFixed(0) }}% 的对象（默认 80%）
                </span>
              </div>

              <div>
                <label style="font-size: 13px;">对象数量占比：</label>
                <el-slider
                  v-model="optimizationConfig.sampling.polygon_line.max_feature_count_ratio"
                  :min="0.3"
                  :max="1.0"
                  :step="0.05"
                  :format-tooltip="(val) => `${(val * 100).toFixed(0)}%`"
                />
                <span style="color: #909399; font-size: 12px;">
                  最多保留 {{ (optimizationConfig.sampling.polygon_line.max_feature_count_ratio * 100).toFixed(0) }}% 的对象数量（默认 60%）
                </span>
              </div>
            </el-form-item>

            <el-form-item label="点保留比例">
              <el-slider
                v-model="optimizationConfig.sampling.point.sample_ratio"
                :min="0.3"
                :max="1.0"
                :step="0.05"
                :format-tooltip="(val) => `${(val * 100).toFixed(0)}%`"
              />
              <span style="color: #909399; font-size: 12px;">
                随机保留 {{ (optimizationConfig.sampling.point.sample_ratio * 100).toFixed(0) }}% 的点对象（默认 60%）
              </span>
            </el-form-item>
          </el-collapse-item>

          <!-- 几何优化 -->
          <el-collapse-item title="3. 几何简化优化" name="simplification">
            <el-form-item label="简化倍数">
              <el-radio-group v-model="optimizationConfig.simplification.tolerance_multiplier">
                <el-radio :label="2">2倍简化</el-radio>
                <el-radio :label="4">4倍简化（推荐）</el-radio>
                <el-radio :label="8">8倍简化</el-radio>
              </el-radio-group>
              <div style="color: #909399; font-size: 12px; margin-top: 8px;">
                简化倍数越高，边界越平滑，瓦片越小，但细节越少
              </div>
            </el-form-item>

            <el-form-item label="简化算法">
              <el-radio-group v-model="optimizationConfig.simplification.algorithm">
                <el-radio label="visvalingam">Visvalingam（保留重要拐点，推荐）</el-radio>
                <el-radio label="douglas_peucker">Douglas-Peucker（保守，保证拓扑）</el-radio>
              </el-radio-group>
            </el-form-item>
          </el-collapse-item>
          </el-collapse>

          <!-- 预估信息 -->
          <el-alert type="info" style="margin-top: 20px;" :closable="false">
            <strong>优化流程</strong>（自动执行，按顺序）<br/>
            瓦片分辨率优化 → 对象采样 → 几何简化<br/>
            每步检查大小，满足条件则停止
          </el-alert>
        </template>
      </el-form>

      <template #footer>
        <el-button @click="resetOptimizationConfig">恢复默认</el-button>
        <el-button @click="handleOptimizationConfigCancel">取消</el-button>
        <el-button type="primary" @click="handleOptimizationConfigConfirm">开始生成</el-button>
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

// 优化配置（默认值）
const optimizationConfig = ref({
  version: '2.0',
  attribute_pruning: {
    enabled: true,
    zoom_threshold: 8
  },
  tile_size_thresholds: {
    no_optimization_mb: 2.0,
    stop_optimization_mb: 5.0
  },
  extent_optimization: {
    blur_level: 2 // 1: 清晰(4096), 2: 适中(2048), 4: 模糊(1024)
  },
  sampling: {
    polygon_line: {
      cumulative_size_ratio: 0.8,
      max_feature_count_ratio: 0.6
    },
    point: {
      sample_ratio: 0.6
    }
  },
  simplification: {
    tolerance_multiplier: 4.0, // 2: 保守, 4: 平衡, 8: 激进
    algorithm: 'visvalingam' // visvalingam | douglas_peucker
  }
})

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
    quickViewLoading.value = true

    // 步骤1: 获取瓦片配置（计算 minZoom 和 maxZoom）
    // 注意: createAPIClient 默认 extractData=true，已自动提取 response.data
    const tileConfig = await quickViewAPI.getTileConfig(engineId.value, schema.value, table.value)

    // 保存配置数据
    tileConfigData.value = tileConfig

    // 步骤2: 显示优化配置对话框
    showOptimizationDialog.value = true
  } catch (error) {
    console.error('获取瓦片配置失败:', error)
    ElMessage.error(error.response?.data?.error || '获取瓦片配置失败')
  } finally {
    quickViewLoading.value = false
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
      concurrency: 10,
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
    version: '2.0',
    attribute_pruning: {
      enabled: true,
      zoom_threshold: 8
    },
    tile_size_thresholds: {
      no_optimization_mb: 2.0,
      stop_optimization_mb: 5.0
    },
    extent_optimization: {
      blur_level: 2
    },
    sampling: {
      polygon_line: {
        cumulative_size_ratio: 0.8,
        max_feature_count_ratio: 0.6
      },
      point: {
        sample_ratio: 0.6
      }
    },
    simplification: {
      tolerance_multiplier: 4.0,
      algorithm: 'visvalingam'
    }
  }
  ElMessage.success('已恢复默认配置')
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
  background: #f5f5f5;
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
  background-color: #fff;
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
