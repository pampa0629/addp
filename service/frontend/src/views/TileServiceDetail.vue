<template>
  <div class="tile-service-detail" v-loading="loading">
    <!-- 顶部操作栏 -->
    <div class="page-header">
      <div class="header-left">
        <el-button @click="goBack" icon="ArrowLeft" circle />
        <h2>{{ service?.title }}</h2>
        <div class="service-meta">
          <el-tag type="primary" size="large">瓦片服务</el-tag>
          <div class="protocol-tags">
            <el-tag v-if="isProtocolEnabled('xyz')" size="small" type="success">
              XYZ
            </el-tag>
            <el-tag v-if="isProtocolEnabled('ogc_tiles')" size="small" type="info">
              OGC Tiles
            </el-tag>
            <el-tag v-if="isProtocolEnabled('tms')" size="small" type="warning">
              TMS
            </el-tag>
          </div>
        </div>
      </div>
      <div class="header-right">
        <el-button @click="goToEdit">编辑</el-button>
        <el-button type="danger" @click="handleDelete">删除</el-button>
      </div>
    </div>

    <!-- 服务信息卡片 -->
    <el-card header="服务信息" style="margin-bottom: 20px">
      <el-descriptions :column="2" border>
        <el-descriptions-item label="服务名称">
          <code>{{ service?.service_name }}</code>
        </el-descriptions-item>
        <el-descriptions-item label="标题">
          {{ service?.title }}
        </el-descriptions-item>
        <el-descriptions-item label="描述" :span="2">
          {{ service?.description || '无' }}
        </el-descriptions-item>
        <el-descriptions-item label="关键词" :span="2">
          <el-tag
            v-for="kw in service?.keywords"
            :key="kw"
            size="small"
            style="margin-right: 5px"
          >
            {{ kw }}
          </el-tag>
          <span v-if="!service?.keywords || service.keywords.length === 0">无</span>
        </el-descriptions-item>
        <el-descriptions-item label="默认坐标系">
          EPSG:{{ service?.default_srid || 3857 }}
        </el-descriptions-item>
        <el-descriptions-item label="公开访问">
          <el-tag :type="service?.public_access ? 'success' : 'info'" size="small">
            {{ service?.public_access ? '是' : '否' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="getStatusType(service?.status)" size="small">
            {{ getStatusText(service?.status) }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="创建时间">
          {{ formatDate(service?.created_at) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 图层管理卡片 -->
    <el-card header="图层管理" style="margin-bottom: 20px">
      <div class="layer-actions" style="margin-bottom: 16px">
        <el-button type="primary" @click="showAddLayerDialog">添加图层</el-button>
      </div>

      <el-table
        :data="service?.layers"
        border
        stripe
        style="width: 100%"
      >
        <el-table-column prop="layer_name" label="图层名称" min-width="120">
          <template #default="{ row }">
            <code>{{ row.layer_name }}</code>
          </template>
        </el-table-column>
        <el-table-column prop="title" label="标题" min-width="150" />
        <el-table-column label="图层类型" width="120">
          <template #default="{ row }">
            <el-tag :type="row.layer_type === 'dynamic' ? 'success' : 'info'" size="small">
              {{ row.layer_type === 'dynamic' ? '动态' : '静态' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="数据源" min-width="200">
          <template #default="{ row }">
            <div v-if="row.layer_type === 'dynamic'">
              <div style="font-size: 12px; color: var(--addp-text-secondary)">
                Engine #{{ row.layer_config?.source?.engine_id }}
              </div>
              <code style="font-size: 12px">
                {{ row.layer_config?.source?.schema }}.{{ row.layer_config?.source?.table }}
              </code>
            </div>
            <div v-else style="font-size: 12px">
              <code>{{ row.layer_config?.tile_path }}</code>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="缓存" width="80" align="center">
          <template #default="{ row }">
            <el-tag
              v-if="row.layer_type === 'dynamic'"
              :type="row.layer_config?.cache?.enabled ? 'success' : 'info'"
              size="small"
            >
              {{ row.layer_config?.cache?.enabled ? '已启用' : '未启用' }}
            </el-tag>
            <span v-else style="color: var(--addp-text-tertiary); font-size: 12px">N/A</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80" align="center">
          <template #default="{ row }">
            <el-tag :type="row.enabled ? 'success' : 'info'" size="small">
              {{ row.enabled ? '启用' : '禁用' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="showEditLayerDialog(row)">编辑</el-button>
            <el-button
              size="small"
              type="warning"
              @click="clearLayerCache(row)"
              :disabled="row.layer_type !== 'dynamic' || !row.layer_config?.cache?.enabled"
            >
              清理缓存
            </el-button>
            <el-button size="small" type="danger" @click="deleteLayer(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-empty
        v-if="!service?.layers || service.layers.length === 0"
        description="暂无图层"
        style="padding: 40px 0"
      />
    </el-card>

    <!-- 服务端点卡片 -->
    <el-card header="服务端点" style="margin-bottom: 20px">
      <!-- XYZ Tiles 端点 -->
      <div v-if="isProtocolEnabled('xyz')" class="endpoint-item">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          <span>XYZ Tiles</span>
        </div>
        <div class="endpoint-url">
          <el-input :value="xyzTilesEndpoint" readonly />
          <el-button @click="copyEndpoint(xyzTilesEndpoint)">复制</el-button>
          <el-button @click="testXYZTile">测试</el-button>
        </div>
        <div style="margin-top: 8px; font-size: 13px; color: var(--addp-text-tertiary)">
          示例: <code>{{ xyzTilesExample }}</code>
        </div>
      </div>

      <!-- WMTS GetCapabilities 端点 -->
      <div v-if="isProtocolEnabled('xyz')" class="endpoint-item" style="margin-top: 20px">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          <span>WMTS GetCapabilities</span>
        </div>
        <div class="endpoint-url">
          <el-input :value="wmtsEndpoint" readonly />
          <el-button @click="copyEndpoint(wmtsEndpoint)">复制</el-button>
          <el-button @click="testEndpoint(wmtsEndpoint)">测试</el-button>
        </div>
      </div>

      <!-- OGC Tiles API 端点 -->
      <div v-if="isProtocolEnabled('ogc_tiles')" class="endpoint-item" style="margin-top: 20px">
        <div class="endpoint-title">
          <el-icon><Link /></el-icon>
          <span>OGC Tiles API</span>
        </div>

        <!-- Landing Page -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: var(--addp-text-tertiary); margin-bottom: 4px">Landing Page:</div>
          <div class="endpoint-url">
            <el-input :value="ogcTilesLandingPage" readonly />
            <el-button @click="copyEndpoint(ogcTilesLandingPage)">复制</el-button>
            <el-button @click="testEndpoint(ogcTilesLandingPage)">测试</el-button>
          </div>
        </div>

        <!-- TileMatrixSets -->
        <div style="margin-bottom: 12px">
          <div style="font-size: 13px; color: var(--addp-text-tertiary); margin-bottom: 4px">TileMatrixSets:</div>
          <div class="endpoint-url">
            <el-input :value="ogcTileMatrixSets" readonly />
            <el-button @click="copyEndpoint(ogcTileMatrixSets)">复制</el-button>
            <el-button @click="testEndpoint(ogcTileMatrixSets)">测试</el-button>
          </div>
        </div>
      </div>

      <el-empty
        v-if="!isProtocolEnabled('xyz') && !isProtocolEnabled('ogc_tiles')"
        description="未启用任何瓦片协议"
      />
    </el-card>

    <!-- 地图预览卡片 -->
    <el-card header="地图预览" style="margin-bottom: 20px">
      <div class="preview-controls" style="margin-bottom: 16px">
        <el-select v-model="selectedLayerForPreview" placeholder="选择图层">
          <el-option
            v-for="layer in service?.layers"
            :key="layer.id"
            :label="layer.title"
            :value="layer.layer_name"
          />
        </el-select>
      </div>

      <div v-if="selectedLayerForPreview" style="width: 100%; height: 600px; border: 1px solid var(--addp-border-color); border-radius: 4px">
        <TilePreview
          :tile-url="previewTileUrl"
          base-map="osm"
          :center="[116.4, 39.9]"
          :zoom="10"
          @tileLoadStart="handleTileLoadStart"
          @tileLoadEnd="handleTileLoadEnd"
          @tileLoadError="handleTileLoadError"
        />
      </div>
      <div
        v-else
        style="width: 100%; height: 600px; border: 1px solid var(--addp-border-color); border-radius: 4px; display: flex; align-items: center; justify-content: center"
      >
        <el-empty description="请选择图层查看地图预览" />
      </div>
    </el-card>

    <!-- 集成示例卡片 -->
    <el-card header="集成示例">
      <el-tabs>
        <el-tab-pane label="OpenLayers">
          <pre class="code-example">{{ openLayersExample }}</pre>
          <el-button @click="copyCode(openLayersExample)" style="margin-top: 8px">复制代码</el-button>
        </el-tab-pane>
        <el-tab-pane label="Leaflet">
          <pre class="code-example">{{ leafletExample }}</pre>
          <el-button @click="copyCode(leafletExample)" style="margin-top: 8px">复制代码</el-button>
        </el-tab-pane>
        <el-tab-pane label="Mapbox GL JS">
          <pre class="code-example">{{ mapboxExample }}</pre>
          <el-button @click="copyCode(mapboxExample)" style="margin-top: 8px">复制代码</el-button>
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 添加/编辑图层对话框 -->
    <el-dialog
      :title="layerDialogMode === 'add' ? '添加图层' : '编辑图层'"
      v-model="layerDialogVisible"
      width="600px"
    >
      <el-form :model="layerForm" label-width="100px">
        <el-form-item label="图层名称">
          <el-input v-model="layerForm.layer_name" placeholder="例如: roads" />
        </el-form-item>
        <el-form-item label="图层标题">
          <el-input v-model="layerForm.title" placeholder="例如: 道路图层" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="layerForm.description" type="textarea" rows="2" />
        </el-form-item>
        <el-form-item label="图层类型">
          <el-radio-group v-model="layerForm.layer_type">
            <el-radio label="dynamic">动态图层</el-radio>
            <el-radio label="static">静态图层</el-radio>
          </el-radio-group>
        </el-form-item>

        <!-- 动态图层配置 -->
        <template v-if="layerForm.layer_type === 'dynamic'">
          <el-form-item label="选择数据表">
            <DataSourceCascader
              :api-base-url="metaApiBaseUrl"
              :engine-types="['postgresql', 'mysql', 'doris', 'clickhouse']"
              :selectable-node-types="['table']"
              :enable-geometry-detection="true"
              :require-geometry="true"
              :show-selection-info="true"
              @update:selection="handleLayerTableSelection"
            />
          </el-form-item>

          <!-- 显示检测到的空间字段信息 -->
          <div v-if="layerFormSpatialMetadata && layerFormSpatialMetadata.hasGeometry" class="geometry-info">
            <p>✅ 已自动检测到空间字段：</p>
            <ul>
              <li><strong>几何列：</strong>{{ layerFormSpatialMetadata.geometryColumn }}</li>
              <li><strong>SRID：</strong>{{ layerFormSpatialMetadata.srid }}</li>
              <li v-if="layerFormSpatialMetadata.geometryTypes && layerFormSpatialMetadata.geometryTypes.length">
                <strong>几何类型：</strong>{{ layerFormSpatialMetadata.geometryTypes.join(', ') }}
              </li>
            </ul>
          </div>

          <el-form-item label="启用缓存">
            <el-switch v-model="layerForm.layer_config.cache.enabled" />
          </el-form-item>
          <el-form-item v-if="layerForm.layer_config.cache.enabled" label="缓存TTL(秒)">
            <el-input-number v-model="layerForm.layer_config.cache.ttl" :min="60" :max="86400" />
          </el-form-item>
        </template>

        <!-- 静态图层配置 -->
        <template v-else-if="layerForm.layer_type === 'static'">
          <el-form-item label="瓦片路径">
            <el-input v-model="layerForm.layer_config.tile_path" placeholder="例如: s3://bucket/tiles" />
          </el-form-item>
          <el-form-item label="格式">
            <el-select v-model="layerForm.layer_config.format">
              <el-option label="MVT" value="mvt" />
              <el-option label="PNG" value="png" />
              <el-option label="JPEG" value="jpeg" />
            </el-select>
          </el-form-item>
          <el-form-item label="最小缩放级别">
            <el-input-number v-model="layerForm.layer_config.zoom_range[0]" :min="0" :max="22" />
          </el-form-item>
          <el-form-item label="最大缩放级别">
            <el-input-number v-model="layerForm.layer_config.zoom_range[1]" :min="0" :max="22" />
          </el-form-item>
        </template>

        <el-form-item label="启用状态">
          <el-switch v-model="layerForm.enabled" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="layerDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="saveLayer">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Link } from '@element-plus/icons-vue'
import tileServiceAPI from '@/api/tileService'
import { TilePreview } from '@common-map'
import { copyToClipboard } from '../utils/serviceHelper'
import { DataSourceCascader } from '@common-ui'

const router = useRouter()
const route = useRoute()

// 状态
const service = ref(null)
const loading = ref(false)
const selectedLayerForPreview = ref(null)
let mapInstance = null

// 图层对话框
const layerDialogVisible = ref(false)
const layerDialogMode = ref('add') // 'add' or 'edit'
const layerFormSpatialMetadata = ref(null) // 空间字段元数据
const layerForm = ref({
  layer_name: '',
  title: '',
  description: '',
  layer_type: 'dynamic',
  enabled: true,
  layer_config: {
    source: {
      engine_id: null,
      schema: '',
      table: '',
      geometry_column: 'geom',
      srid: 4326
    },
    mvt: {
      extent: 4096,
      buffer: 256
    },
    cache: {
      enabled: true,
      ttl: 3600
    },
    tile_path: '',
    format: 'mvt',
    zoom_range: [0, 18]
  }
})

const serviceId = computed(() => route.params.id)

// Service API 基础 URL（用于 DataSourceCascader）
// 开发环境：通过 Vite proxy 代理到 Gateway (localhost:8000)
// 生产环境：直接访问 Gateway
const metaApiBaseUrl = computed(() => {
  return '/api/service'
})

// 计算服务端点
const baseURL = computed(() => {
  // 开发环境：使用 Service 后端地址（8190）或 Gateway（8000）
  // 生产环境：使用 Gateway（8000）
  if (import.meta.env.DEV) {
    // 开发环境优先使用 Gateway，因为它有完整的路由
    return 'http://localhost:8000'
  } else {
    // 生产环境通过 Gateway 访问
    return window.location.origin
  }
})

const xyzTilesEndpoint = computed(() => {
  if (!service.value) return ''
  return `${baseURL.value}/tiles/${service.value.service_name}/{layerName}/{z}/{x}/{y}.mvt`
})

// 地图预览的瓦片 URL
const previewTileUrl = computed(() => {
  if (!service.value || !selectedLayerForPreview.value) return ''
  return `${baseURL.value}/tiles/${service.value.service_name}/${selectedLayerForPreview.value}/{z}/{x}/{y}.mvt`
})

const xyzTilesExample = computed(() => {
  if (!service.value || !service.value.layers || service.value.layers.length === 0) return ''
  const firstLayer = service.value.layers[0]
  return `${baseURL.value}/tiles/${service.value.service_name}/${firstLayer.layer_name}/12/3421/1532.mvt`
})

const wmtsEndpoint = computed(() => {
  if (!service.value) return ''
  return `${baseURL.value}/wmts/${service.value.service_name}?request=GetCapabilities`
})

const ogcTilesLandingPage = computed(() => {
  if (!service.value) return ''
  return `${baseURL.value}/ogc/tiles/${service.value.service_name}`
})

const ogcTileMatrixSets = computed(() => {
  if (!service.value) return ''
  return `${baseURL.value}/ogc/tiles/${service.value.service_name}/tileMatrixSets`
})

// 集成示例代码
const openLayersExample = computed(() => {
  if (!service.value || !service.value.layers || service.value.layers.length === 0) return ''
  const firstLayer = service.value.layers[0]
  return `import VectorTileLayer from 'ol/layer/VectorTile'
import VectorTileSource from 'ol/source/VectorTile'
import MVT from 'ol/format/MVT'
import { Map, View } from 'ol'
import { fromLonLat } from 'ol/proj'

const layer = new VectorTileLayer({
  source: new VectorTileSource({
    format: new MVT(),
    url: '${baseURL.value}/tiles/${service.value.service_name}/${firstLayer.layer_name}/{z}/{x}/{y}.mvt'
  })
})

const map = new Map({
  target: 'map',
  layers: [layer],
  view: new View({
    center: fromLonLat([116.4, 39.9]),
    zoom: 10
  })
})`
})

const leafletExample = computed(() => {
  if (!service.value || !service.value.layers || service.value.layers.length === 0) return ''
  const firstLayer = service.value.layers[0]
  return `// 使用 Leaflet.VectorGrid 插件
import L from 'leaflet'
import 'leaflet.vectorgrid'

const map = L.map('map').setView([39.9, 116.4], 10)

const vectorTileOptions = {
  rendererFactory: L.canvas.tile,
  vectorTileLayerStyles: {
    // 定义样式
  }
}

const layer = L.vectorGrid.protobuf(
  '${baseURL.value}/tiles/${service.value.service_name}/${firstLayer.layer_name}/{z}/{x}/{y}.mvt',
  vectorTileOptions
).addTo(map)`
})

const mapboxExample = computed(() => {
  if (!service.value || !service.value.layers || service.value.layers.length === 0) return ''
  const firstLayer = service.value.layers[0]
  return `import mapboxgl from 'mapbox-gl'

const map = new mapboxgl.Map({
  container: 'map',
  center: [116.4, 39.9],
  zoom: 10
})

map.on('load', () => {
  map.addSource('${firstLayer.layer_name}', {
    type: 'vector',
    tiles: ['${baseURL.value}/tiles/${service.value.service_name}/${firstLayer.layer_name}/{z}/{x}/{y}.mvt'],
    minzoom: 0,
    maxzoom: 22
  })

  map.addLayer({
    id: '${firstLayer.layer_name}',
    type: 'line', // 或 'fill', 'circle' 等
    source: '${firstLayer.layer_name}',
    'source-layer': '${firstLayer.layer_name}'
  })
})`
})

// 方法：检查协议是否启用
const isProtocolEnabled = (protocolName) => {
  if (!service.value?.protocols || !service.value.protocols[protocolName]) {
    return false
  }
  return service.value.protocols[protocolName].enabled === true
}

// 方法：获取状态文本
const getStatusText = (status) => {
  const statusMap = {
    active: '正常',
    inactive: '未激活',
    error: '错误'
  }
  return statusMap[status] || status
}

const getStatusType = (status) => {
  const typeMap = {
    active: 'success',
    inactive: 'warning',
    error: 'danger'
  }
  return typeMap[status] || 'info'
}

// 方法：格式化日期
const formatDate = (dateString) => {
  if (!dateString) return '-'
  return new Date(dateString).toLocaleString('zh-CN')
}

// 方法：复制端点
const copyEndpoint = async (url) => {
  const success = await copyToClipboard(url)
  if (success) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败，请手动复制')
  }
}

// 方法：复制代码
const copyCode = async (code) => {
  const success = await copyToClipboard(code)
  if (success) {
    ElMessage.success('已复制到剪贴板')
  } else {
    ElMessage.error('复制失败')
  }
}

// 方法：测试端点
const testEndpoint = (url) => {
  window.open(url, '_blank')
}

// 方法：测试 XYZ Tile
const testXYZTile = () => {
  if (xyzTilesExample.value) {
    window.open(xyzTilesExample.value, '_blank')
  }
}

// 方法：加载服务详情
const loadService = async () => {
  loading.value = true
  try {
    const response = await tileServiceAPI.getService(serviceId.value)

    // createAPIClient extractData=true 已提取 .data
    // 后端返回: { id: 1, service_name: "test", layers: [...] }
    // response 直接就是服务对象
    service.value = response

    console.log('[TileServiceDetail] 加载服务详情:', service.value)

    // 默认选择第一个图层
    if (service.value.layers && service.value.layers.length > 0) {
      selectedLayerForPreview.value = service.value.layers[0].layer_name
    }
  } catch (error) {
    ElMessage.error('加载服务详情失败: ' + (error.response?.data?.error || error.message))
    console.error('Failed to load service:', error)
  } finally {
    loading.value = false
  }
}

// 方法：删除服务
const handleDelete = async () => {
  try {
    await ElMessageBox.confirm(
      '确定要删除此瓦片服务吗？删除后无法恢复。',
      '确认删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    await tileServiceAPI.deleteService(serviceId.value)
    ElMessage.success('瓦片服务已删除')
    router.push('/tile')
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败: ' + (error.response?.data?.error || error.message))
    }
  }
}

// 方法：导航
const goBack = () => {
  router.push('/tile')
}

const goToEdit = () => {
  router.push(`/tile/${serviceId.value}/edit`)
}

// 图层管理方法
const showAddLayerDialog = () => {
  layerDialogMode.value = 'add'
  resetLayerForm()
  layerDialogVisible.value = true
}

const showEditLayerDialog = (layer) => {
  layerDialogMode.value = 'edit'
  layerForm.value = JSON.parse(JSON.stringify(layer)) // 深拷贝
  layerDialogVisible.value = true
}

const resetLayerForm = () => {
  layerForm.value = {
    layer_name: '',
    title: '',
    description: '',
    layer_type: 'dynamic',
    enabled: true,
    layer_config: {
      source: {
        engine_id: null,
        schema: '',
        table: '',
        geometry_column: 'geom',
        srid: 4326
      },
      mvt: {
        extent: 4096,
        buffer: 256
      },
      cache: {
        enabled: true,
        ttl: 3600
      },
      tile_path: '',
      format: 'mvt',
      zoom_range: [0, 18]
    }
  }
  layerFormSpatialMetadata.value = null
}

// 处理表选择（DataSourceCascader 回调）
const handleLayerTableSelection = (selection) => {
  console.log('[TileServiceDetail] Layer table selection:', selection)

  if (!selection) {
    // 清空选择
    layerForm.value.layer_config.source.engine_id = null
    layerForm.value.layer_config.source.schema = ''
    layerForm.value.layer_config.source.table = ''
    layerForm.value.layer_config.source.geometry_column = 'geom'
    layerForm.value.layer_config.source.srid = 4326
    layerFormSpatialMetadata.value = null
    return
  }

  // 更新表单字段
  layerForm.value.layer_config.source.engine_id = selection.engineId
  layerForm.value.layer_config.source.schema = selection.schema
  layerForm.value.layer_config.source.table = selection.tableName

  // 如果检测到几何列，自动填充
  if (selection.hasGeometry) {
    layerForm.value.layer_config.source.geometry_column = selection.geometryColumn
    layerForm.value.layer_config.source.srid = selection.srid || 4326

    layerFormSpatialMetadata.value = {
      hasGeometry: true,
      geometryColumn: selection.geometryColumn,
      srid: selection.srid || 4326,
      geometryTypes: selection.geometryType ? [selection.geometryType] : [],
      extent: selection.extent
    }

    ElMessage.success(`检测到空间字段: ${selection.geometryColumn}`)
  } else {
    layerFormSpatialMetadata.value = { hasGeometry: false }
    ElMessage.warning('未检测到空间字段，请确认数据表是否包含空间数据')
  }
}

const saveLayer = async () => {
  try {
    if (layerDialogMode.value === 'add') {
      await tileServiceAPI.addLayer(serviceId.value, layerForm.value)
      ElMessage.success('图层已添加')
    } else {
      await tileServiceAPI.updateLayer(serviceId.value, layerForm.value.id, layerForm.value)
      ElMessage.success('图层已更新')
    }

    layerDialogVisible.value = false
    await loadService() // 重新加载服务
  } catch (error) {
    ElMessage.error('保存失败: ' + (error.response?.data?.error || error.message))
  }
}

const deleteLayer = async (layer) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除图层 "${layer.title}" 吗？此操作不可恢复。`,
      '确认删除',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    await tileServiceAPI.deleteLayer(serviceId.value, layer.id)
    ElMessage.success('图层已删除')
    await loadService() // 重新加载服务
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('删除失败: ' + (error.response?.data?.error || error.message))
    }
  }
}

const clearLayerCache = async (layer) => {
  try {
    await ElMessageBox.confirm(
      `确定要清理图层 "${layer.title}" 的所有缓存吗？`,
      '确认清理',
      {
        confirmButtonText: '确定',
        cancelButtonText: '取消',
        type: 'warning'
      }
    )

    await tileServiceAPI.clearLayerCache(serviceId.value, layer.id)
    ElMessage.success('缓存已清理')
  } catch (error) {
    if (error !== 'cancel') {
      ElMessage.error('清理失败: ' + (error.response?.data?.error || error.message))
    }
  }
}

// 地图预览事件处理
const handleTileLoadStart = () => {
  console.log('[TileServiceDetail] 瓦片开始加载')
}

const handleTileLoadEnd = (tile) => {
  console.log('[TileServiceDetail] 瓦片加载成功', tile)
}

const handleTileLoadError = (tile) => {
  console.error('[TileServiceDetail] 瓦片加载失败', tile)
  ElMessage.error('瓦片加载失败，请检查服务配置')
}

// 生命周期
onMounted(() => {
  loadService()
})
</script>

<style scoped>
.tile-service-detail {
  padding: 20px;
  max-width: 1400px;
  margin: 0 auto;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  margin-bottom: 24px;
  padding-bottom: 16px;
  border-bottom: 1px solid #ebeef5;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
}

.header-left h2 {
  margin: 0;
  font-size: 24px;
  color: var(--addp-text-primary);
}

.service-meta {
  display: flex;
  align-items: center;
  gap: 8px;
}

.protocol-tags {
  display: flex;
  gap: 4px;
}

.header-right {
  display: flex;
  gap: 8px;
}

.endpoint-item {
  margin-bottom: 24px;
}

.endpoint-item:last-child {
  margin-bottom: 0;
}

.endpoint-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 500;
  color: var(--addp-text-primary);
  margin-bottom: 8px;
}

.endpoint-url {
  display: flex;
  gap: 8px;
  align-items: center;
}

.endpoint-url .el-input {
  flex: 1;
}

code {
  background-color: var(--addp-bg-secondary);
  padding: 2px 6px;
  border-radius: 3px;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: #e83e8c;
}

.code-example {
  background-color: var(--addp-bg-secondary);
  border: 1px solid var(--addp-border-color);
  border-radius: 4px;
  padding: 16px;
  overflow-x: auto;
  margin: 0;
  font-family: 'Courier New', monospace;
  font-size: 13px;
  color: var(--addp-text-primary);
  white-space: pre-wrap;
  word-wrap: break-word;
}

.preview-controls {
  display: flex;
  gap: 12px;
  align-items: center;
}

#map-container {
  position: relative;
}

/* 空间字段信息展示 */
.geometry-info {
  background: #e8f5e9;
  border: 1px solid #4caf50;
  border-radius: 4px;
  padding: 15px;
  margin-bottom: 15px;
}

.geometry-info p {
  margin: 0 0 10px 0;
  color: #2e7d32;
  font-weight: 500;
}

.geometry-info ul {
  list-style: none;
  padding: 0;
  margin: 0;
}

.geometry-info ul li {
  padding: 4px 0;
  color: #555;
}

.geometry-info ul li strong {
  color: #2e7d32;
}
</style>
