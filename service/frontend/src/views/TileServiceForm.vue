<template>
  <div class="tile-service-form">
    <h1>{{ isEditing ? '编辑瓦片服务' : '创建瓦片服务' }}</h1>

    <!-- 步骤指示器 -->
    <div class="steps">
      <div class="step" :class="{ active: currentStep === 0, completed: currentStep > 0 }">
        <div class="step-number">1</div>
        <div class="step-title">添加第一个图层</div>
      </div>
      <div class="step-line" :class="{ active: currentStep > 0 }"></div>
      <div class="step" :class="{ active: currentStep === 1, completed: currentStep > 1 }">
        <div class="step-number">2</div>
        <div class="step-title">配置服务信息</div>
      </div>
      <div class="step-line" :class="{ active: currentStep > 1 }"></div>
      <div class="step" :class="{ active: currentStep === 2, completed: currentStep > 2 }">
        <div class="step-number">3</div>
        <div class="step-title">完成</div>
      </div>
    </div>

    <!-- Step 0: 添加第一个图层 -->
    <div v-if="currentStep === 0" class="form-step">
      <h2>选择图层类型</h2>
      <div class="layer-type-selector">
        <div
          class="layer-type-card"
          :class="{ selected: layerType === 'dynamic' }"
          @click="layerType = 'dynamic'"
        >
          <h3>动态图层</h3>
          <p>实时从数据库生成瓦片，支持最新数据</p>
        </div>
        <div
          class="layer-type-card"
          :class="{ selected: layerType === 'static' }"
          @click="layerType = 'static'"
        >
          <h3>静态图层</h3>
          <p>使用预生成的静态瓦片文件</p>
        </div>
      </div>

      <!-- 动态图层配置 -->
      <div v-if="layerType === 'dynamic'" class="layer-config">
        <h3>动态图层配置</h3>
        <div class="form-group">
          <label>图层名称 *</label>
          <input v-model="form.layerName" type="text" placeholder="例如: roads" />
        </div>
        <div class="form-group">
          <label>图层标题 *</label>
          <input v-model="form.layerTitle" type="text" placeholder="例如: 道路图层" />
        </div>
        <div class="form-group">
          <label>图层描述</label>
          <textarea v-model="form.layerDescription" placeholder="图层描述（可选）" rows="2"></textarea>
        </div>

        <div class="form-group">
          <label>选择数据表 *</label>
          <DataSourceCascader
            :api-base-url="metaApiBaseUrl"
            :engine-types="['postgresql', 'mysql', 'doris', 'clickhouse']"
            :selectable-node-types="['table']"
            :enable-geometry-detection="true"
            :require-geometry="true"
            :show-selection-info="true"
            @update:selection="handleTableSelection"
          />
        </div>

        <!-- 显示检测到的空间字段信息 -->
        <div v-if="spatialMetadata && spatialMetadata.hasGeometry" class="geometry-info">
          <p>✅ 已自动检测到空间字段：</p>
          <ul>
            <li><strong>几何列：</strong>{{ spatialMetadata.geometryColumn }}</li>
            <li><strong>SRID：</strong>{{ spatialMetadata.srid }}</li>
            <li v-if="spatialMetadata.geometryTypes && spatialMetadata.geometryTypes.length"><strong>几何类型：</strong>{{ spatialMetadata.geometryTypes.join(', ') }}</li>
          </ul>
        </div>
      </div>

      <!-- 静态图层配置 -->
      <div v-else-if="layerType === 'static'" class="layer-config">
        <h3>静态图层配置</h3>
        <div class="form-group">
          <label>图层名称 *</label>
          <input v-model="form.layerName" type="text" placeholder="例如: basemap" />
        </div>
        <div class="form-group">
          <label>图层标题 *</label>
          <input v-model="form.layerTitle" type="text" placeholder="例如: 底图" />
        </div>
        <div class="form-group">
          <label>图层描述</label>
          <textarea v-model="form.layerDescription" placeholder="图层描述（可选）" rows="2"></textarea>
        </div>
        <div class="form-group">
          <label>瓦片路径 *</label>
          <input v-model="form.tilePath" type="text" placeholder="例如: s3://bucket/tiles或 tiles/osm" />
          <p class="help-text">MinIO/S3 路径或相对路径</p>
        </div>
        <div class="form-group">
          <label>瓦片格式 *</label>
          <select v-model="form.format">
            <option value="mvt">MVT (Mapbox Vector Tile)</option>
            <option value="png">PNG</option>
            <option value="jpeg">JPEG</option>
          </select>
        </div>
        <div class="form-row">
          <div class="form-group">
            <label>最小缩放级别 *</label>
            <input v-model.number="form.minZoom" type="number" min="0" max="22" />
          </div>
          <div class="form-group">
            <label>最大缩放级别 *</label>
            <input v-model.number="form.maxZoom" type="number" min="0" max="22" />
          </div>
        </div>
      </div>

      <div class="form-actions">
        <button @click="$router.back()" class="btn btn-secondary">取消</button>
        <button @click="nextStep" class="btn btn-primary" :disabled="!validateStep0()">下一步</button>
      </div>
    </div>

    <!-- Step 1: 配置服务信息 -->
    <div v-if="currentStep === 1" class="form-step">
      <h2>服务信息配置</h2>
      <div class="form-group">
        <label>服务名称 * <span class="help-text">（URL 中使用，建议使用小写字母和下划线）</span></label>
        <input v-model="form.serviceName" type="text" placeholder="例如: beijing_tiles" />
      </div>
      <div class="form-group">
        <label>服务标题 *</label>
        <input v-model="form.title" type="text" placeholder="例如: 北京市瓦片服务" />
      </div>
      <div class="form-group">
        <label>服务描述</label>
        <textarea v-model="form.description" placeholder="服务描述（可选）" rows="3"></textarea>
      </div>
      <div class="form-group">
        <label>关键词</label>
        <input v-model="form.keywords" type="text" placeholder="多个关键词用逗号分隔" />
      </div>

      <h3>协议配置</h3>
      <div class="protocols-config">
        <label class="checkbox-label">
          <input type="checkbox" v-model="protocols.xyz.enabled" />
          XYZ Tiles（推荐）
        </label>
        <label class="checkbox-label">
          <input type="checkbox" v-model="protocols.ogc_tiles.enabled" />
          OGC Tiles API
        </label>
        <label class="checkbox-label">
          <input type="checkbox" v-model="protocols.tms.enabled" />
          TMS
        </label>
      </div>

      <h3>访问控制</h3>
      <div class="form-group">
        <label class="checkbox-label">
          <input type="checkbox" v-model="form.publicAccess" />
          公开访问（无需认证）
        </label>
      </div>

      <div class="form-actions">
        <button @click="previousStep" class="btn btn-secondary">上一步</button>
        <button @click="submitForm" class="btn btn-primary" :disabled="!validateStep1() || submitting">
          {{ submitting ? '创建中...' : '创建服务' }}
        </button>
      </div>
    </div>

    <!-- Step 2: 完成 -->
    <div v-if="currentStep === 2" class="form-step completion-step">
      <div class="success-icon">✓</div>
      <h2>瓦片服务创建成功！</h2>
      <div v-if="createdService" class="service-info">
        <p><strong>服务名称:</strong> {{ createdService.service_name }}</p>
        <p><strong>服务标题:</strong> {{ createdService.title }}</p>
        <div v-if="createdService.endpoints" class="endpoints">
          <h3>服务端点:</h3>
          <div v-if="createdService.endpoints.xyz_tiles" class="endpoint">
            <strong>XYZ Tiles:</strong>
            <code>{{ createdService.endpoints.xyz_tiles }}</code>
            <button @click="copyToClipboard(createdService.endpoints.xyz_tiles)" class="btn btn-sm btn-secondary">复制</button>
          </div>
          <div v-if="createdService.endpoints.wmts" class="endpoint">
            <strong>WMTS:</strong>
            <code>{{ createdService.endpoints.wmts }}</code>
            <button @click="copyToClipboard(createdService.endpoints.wmts)" class="btn btn-sm btn-secondary">复制</button>
          </div>
          <div v-if="createdService.endpoints.ogc_tiles" class="endpoint">
            <strong>OGC Tiles API:</strong>
            <code>{{ createdService.endpoints.ogc_tiles }}</code>
            <button @click="copyToClipboard(createdService.endpoints.ogc_tiles)" class="btn btn-sm btn-secondary">复制</button>
          </div>
        </div>
      </div>
      <div v-else class="service-info">
        <p>正在加载服务信息...</p>
      </div>
      <div class="form-actions">
        <button @click="viewDetail" class="btn btn-primary">查看详情</button>
        <button @click="createAnother" class="btn btn-secondary">创建另一个服务</button>
      </div>
    </div>
  </div>
</template>

<script>
import tileServiceAPI from '@/api/tileService'
import { DataSourceCascader } from '@common-ui'
import { ElMessage } from 'element-plus'

export default {
  name: 'TileServiceForm',
  components: {
    DataSourceCascader
  },
  data() {
    return {
      currentStep: 0,
      layerType: 'dynamic',
      form: {
        serviceName: '',
        title: '',
        description: '',
        keywords: '',
        publicAccess: false,
        // 图层配置
        layerName: '',
        layerTitle: '',
        layerDescription: '',
        // 动态图层
        engineId: null,
        schema: '',
        table: '',
        geomColumn: '',
        srid: 4326,
        // 静态图层
        tilePath: '',
        format: 'mvt',
        minZoom: 0,
        maxZoom: 18
      },
      protocols: {
        xyz: { enabled: true },
        ogc_tiles: { enabled: false },
        tms: { enabled: false }
      },
      spatialMetadata: null,
      submitting: false,
      createdService: null
    }
  },
  computed: {
    isEditing() {
      return this.$route.params.id !== undefined
    },
    metaApiBaseUrl() {
      // Service API 基础 URL（用于 DataSourceCascader）
      // 开发环境：通过 Vite proxy 代理到 Gateway (localhost:8000)
      // 生产环境：直接访问 Gateway
      return '/api/v1/service'
    }
  },
  methods: {
    // 处理表选择（DataSourceCascader 回调）
    handleTableSelection(selection) {
      console.log('[TileServiceForm] Table selection:', selection)

      if (!selection) {
        // 清空选择
        this.form.engineId = null
        this.form.schema = ''
        this.form.table = ''
        this.form.geomColumn = ''
        this.form.srid = 4326
        this.spatialMetadata = null
        return
      }

      // 更新表单字段
      this.form.engineId = selection.engineId
      this.form.schema = selection.schema
      this.form.table = selection.tableName

      // 如果检测到几何列，自动填充
      if (selection.hasGeometry) {
        this.form.geomColumn = selection.geometryColumn
        this.form.srid = selection.srid || 4326

        this.spatialMetadata = {
          hasGeometry: true,
          geometryColumn: selection.geometryColumn,
          srid: selection.srid || 4326,
          geometryTypes: selection.geometryType ? [selection.geometryType] : [],
          extent: selection.extent
        }

        ElMessage.success(`检测到空间字段: ${selection.geometryColumn}`)
      } else {
        this.spatialMetadata = { hasGeometry: false }
        ElMessage.warning('未检测到空间字段，请确认数据表是否包含空间数据')
      }
    },

    validateStep0() {
      if (!this.form.layerName || !this.form.layerTitle) {
        return false
      }
      if (this.layerType === 'dynamic') {
        return this.form.engineId && this.form.schema && this.form.table && this.form.geomColumn
      } else {
        return this.form.tilePath && this.form.format
      }
    },

    validateStep1() {
      return this.form.serviceName && this.form.title
    },

    nextStep() {
      if (this.currentStep === 0 && this.validateStep0()) {
        this.currentStep = 1
      }
    },

    previousStep() {
      if (this.currentStep > 0) {
        this.currentStep--
      }
    },

    async submitForm() {
      if (!this.validateStep1() || this.submitting) {
        return
      }

      this.submitting = true

      try {
        // 构建图层配置
        const layerConfig = this.layerType === 'dynamic'
          ? {
              source: {
                engine_id: this.form.engineId,
                schema: this.form.schema,
                table: this.form.table,
                geometry_column: this.form.geomColumn,
                srid: this.form.srid
              },
              mvt: {
                extent: 4096,
                buffer: 256
              },
              cache: {
                enabled: true,
                ttl: 3600
              }
            }
          : {
              source: 'external',
              tile_path: this.form.tilePath,
              format: this.form.format,
              zoom_range: [this.form.minZoom, this.form.maxZoom]
            }

        // 构建请求数据
        const requestData = {
          service_name: this.form.serviceName,
          title: this.form.title,
          description: this.form.description,
          keywords: this.form.keywords.split(',').map(k => k.trim()).filter(k => k),
          protocols: this.protocols,
          public_access: this.form.publicAccess,
          first_layer: {
            layer_name: this.form.layerName,
            title: this.form.layerTitle,
            description: this.form.layerDescription,
            layer_type: this.layerType,
            layer_config: layerConfig
          }
        }

        const response = await tileServiceAPI.createService(requestData)
        console.log('[TileServiceForm] 创建响应:', response)

        // createAPIClient 默认 extractData=true，已经自动提取了 .data
        // 所以 response 本身就是服务对象
        if (!response || !response.id) {
          console.error('[TileServiceForm] 响应数据无效:', response)
          throw new Error('服务数据无效或缺少 ID')
        }

        this.createdService = response
        this.currentStep = 2
      } catch (error) {
        console.error('创建瓦片服务失败:', error)
        alert('创建失败: ' + (error.response?.data?.error || error.message))
      } finally {
        this.submitting = false
      }
    },

    viewDetail() {
      if (this.createdService && this.createdService.id) {
        this.$router.push(`/tile/${this.createdService.id}`)
      }
    },

    createAnother() {
      // 重置表单
      this.currentStep = 0
      this.createdService = null
      this.form = {
        serviceName: '',
        title: '',
        description: '',
        keywords: '',
        publicAccess: false,
        layerName: '',
        layerTitle: '',
        layerDescription: '',
        engineId: null,
        schema: '',
        table: '',
        geomColumn: '',
        srid: 4326,
        tilePath: '',
        format: 'mvt',
        minZoom: 0,
        maxZoom: 18
      }
      this.protocols = {
        xyz: { enabled: true },
        ogc_tiles: { enabled: false },
        tms: { enabled: false }
      }
    },

    copyToClipboard(text) {
      navigator.clipboard.writeText(text).then(() => {
        alert('已复制到剪贴板')
      }).catch(err => {
        console.error('复制失败:', err)
      })
    }
  }
}
</script>

<style scoped>
.tile-service-form {
  max-width: 900px;
  margin: 0 auto;
  padding: 20px;
}

h1 {
  margin-bottom: 30px;
  color: var(--addp-text-primary);
}

/* 步骤指示器 */
.steps {
  display: flex;
  justify-content: center;
  align-items: center;
  margin-bottom: 40px;
}

.step {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.step-number {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  background: #e0e0e0;
  color: var(--addp-text-tertiary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: bold;
  transition: all 0.3s;
}

.step.active .step-number {
  background: #1976d2;
  color: white;
}

.step.completed .step-number {
  background: #388e3c;
  color: white;
}

.step-title {
  font-size: 14px;
  color: var(--addp-text-tertiary);
}

.step.active .step-title {
  color: #1976d2;
  font-weight: 600;
}

.step-line {
  width: 80px;
  height: 2px;
  background: #e0e0e0;
  margin: 0 10px;
  transition: all 0.3s;
}

.step-line.active {
  background: #388e3c;
}

/* 表单步骤 */
.form-step {
  background: white;
  padding: 30px;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
}

h2 {
  margin-bottom: 20px;
  color: var(--addp-text-primary);
  font-size: 20px;
}

h3 {
  margin: 20px 0 15px 0;
  color: #555;
  font-size: 16px;
}

/* 图层类型选择卡片 */
.layer-type-selector {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
  margin-bottom: 30px;
}

.layer-type-card {
  padding: 30px 20px;
  border: 2px solid #e0e0e0;
  border-radius: 8px;
  text-align: center;
  cursor: pointer;
  transition: all 0.2s;
}

.layer-type-card:hover {
  border-color: #1976d2;
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
}

.layer-type-card.selected {
  border-color: #1976d2;
  background: #e3f2fd;
}

.layer-type-card h3 {
  margin: 0 0 10px 0;
  color: var(--addp-text-primary);
}

.layer-type-card p {
  margin: 0;
  color: var(--addp-text-secondary);
  font-size: 14px;
}

/* 表单组件 */
.layer-config,
.protocols-config {
  margin-bottom: 20px;
}

.form-group {
  margin-bottom: 20px;
}

.form-group label {
  display: block;
  margin-bottom: 8px;
  font-weight: 500;
  color: #555;
}

.help-text {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 4px;
}

.form-group input[type="text"],
.form-group input[type="number"],
.form-group select,
.form-group textarea {
  width: 100%;
  padding: 10px;
  border: 1px solid #ddd;
  border-radius: 4px;
  font-size: 14px;
  box-sizing: border-box;
}

.form-group textarea {
  resize: vertical;
}

/* 空间字段信息展示 */
.geometry-info {
  background: #e8f5e9;
  border: 1px solid #4caf50;
  border-radius: 4px;
  padding: 15px;
  margin-top: 15px;
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

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 20px;
}

.checkbox-label {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
  cursor: pointer;
}

.checkbox-label input[type="checkbox"] {
  width: 18px;
  height: 18px;
  cursor: pointer;
}

/* 表单操作按钮 */
.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 15px;
  margin-top: 30px;
  padding-top: 20px;
  border-top: 1px solid #eee;
}

/* 完成步骤 */
.completion-step {
  text-align: center;
}

.success-icon {
  width: 80px;
  height: 80px;
  margin: 0 auto 20px;
  background: #388e3c;
  color: white;
  font-size: 48px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.service-info {
  background: #f9f9f9;
  padding: 20px;
  border-radius: 4px;
  margin: 20px 0;
  text-align: left;
}

.service-info p {
  margin: 10px 0;
}

.endpoints {
  margin-top: 20px;
}

.endpoint {
  margin: 15px 0;
  padding: 15px;
  background: white;
  border-radius: 4px;
  border: 1px solid #ddd;
}

.endpoint strong {
  display: block;
  margin-bottom: 8px;
  color: #555;
}

.endpoint code {
  display: inline-block;
  padding: 8px 12px;
  background: var(--addp-bg-secondary);
  border-radius: 4px;
  font-family: monospace;
  font-size: 12px;
  word-break: break-all;
  margin-right: 10px;
}

/* 按钮样式 */
.btn {
  padding: 10px 20px;
  border: none;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  transition: all 0.2s;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.btn-primary {
  background: #1976d2;
  color: white;
}

.btn-primary:hover:not(:disabled) {
  background: #1565c0;
}

.btn-secondary {
  background: #757575;
  color: white;
}

.btn-secondary:hover {
  background: #616161;
}

.btn-sm {
  padding: 6px 12px;
  font-size: 12px;
}
</style>
