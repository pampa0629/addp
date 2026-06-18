<template>
  <div class="tile-service-form">
    <h1>{{ isEditing ? $t('service.tile.formEditTitle') : $t('service.tile.formCreateTitle') }}</h1>

    <!-- 步骤指示器 -->
    <div class="steps">
      <div class="step" :class="{ active: currentStep === 0, completed: currentStep > 0 }">
        <div class="step-number">1</div>
        <div class="step-title">{{ $t('service.tile.step1Title') }}</div>
      </div>
      <div class="step-line" :class="{ active: currentStep > 0 }"></div>
      <div class="step" :class="{ active: currentStep === 1, completed: currentStep > 1 }">
        <div class="step-number">2</div>
        <div class="step-title">{{ $t('service.tile.step2Title') }}</div>
      </div>
      <div class="step-line" :class="{ active: currentStep > 1 }"></div>
      <div class="step" :class="{ active: currentStep === 2, completed: currentStep > 2 }">
        <div class="step-number">3</div>
        <div class="step-title">{{ $t('service.tile.step3Title') }}</div>
      </div>
    </div>

    <!-- Step 0: 添加第一个图层 -->
    <div v-if="currentStep === 0" class="form-step">
      <h2>{{ $t('service.tile.selectLayerType') }}</h2>
      <div class="layer-type-selector">
        <div
          class="layer-type-card"
          :class="{ selected: layerType === 'dynamic' }"
          @click="layerType = 'dynamic'"
        >
          <h3>{{ $t('service.tile.dynamicLayer') }}</h3>
          <p>{{ $t('service.tile.dynamicLayerDesc') }}</p>
        </div>
        <div
          class="layer-type-card"
          :class="{ selected: layerType === 'static' }"
          @click="layerType = 'static'"
        >
          <h3>{{ $t('service.tile.staticLayer') }}</h3>
          <p>{{ $t('service.tile.staticLayerDesc') }}</p>
        </div>
      </div>

      <!-- 动态图层配置 -->
      <div v-if="layerType === 'dynamic'" class="layer-config">
        <h3>{{ $t('service.tile.dynamicLayerConfig') }}</h3>
        <div class="form-group">
          <label>{{ $t('service.tile.layerNameLabel') }} *</label>
          <input v-model="form.layerName" type="text" :placeholder="$t('service.tile.layerNamePlaceholder')" />
        </div>
        <div class="form-group">
          <label>{{ $t('service.tile.layerTitleLabel') }} *</label>
          <input v-model="form.layerTitle" type="text" :placeholder="$t('service.tile.layerTitlePlaceholder')" />
        </div>
        <div class="form-group">
          <label>{{ $t('service.tile.layerDescLabel') }}</label>
          <textarea v-model="form.layerDescription" :placeholder="$t('service.tile.layerDescPlaceholder')" rows="2"></textarea>
        </div>

        <div class="form-group">
          <label>{{ $t('service.tile.selectTableLabel') }} *</label>
          <ResourceTreePicker
            :api-base-url="metaApiBaseUrl"
            :engine-types="nativeTableEngineTypes"
            mode="item"
            :node-filter="isNativeTableVisibleNode"
            :selectable-filter="isNativeTableNode"
            :show-selection-summary="true"
            :engine-multiple="true"
            :select-all-engines-by-default="true"
            :search-selectable-only="true"
            :show-disabled-label="false"
            :show-count="false"
            @update:model-value="handleTableSelection"
          />
        </div>

        <!-- 显示检测到的空间字段信息 -->
        <div v-if="spatialMetadata && spatialMetadata.hasGeometry" class="geometry-info">
          <p>✅ {{ $t('service.tile.spatialDetected') }}</p>
          <ul>
            <li><strong>{{ $t('service.tile.geomColumnLabel') }}：</strong>{{ spatialMetadata.geometryColumn }}</li>
            <li><strong>SRID：</strong>{{ spatialMetadata.srid }}</li>
            <li v-if="spatialMetadata.geometryTypes && spatialMetadata.geometryTypes.length"><strong>{{ $t('service.tile.geomTypeLabel') }}：</strong>{{ spatialMetadata.geometryTypes.join(', ') }}</li>
          </ul>
        </div>
      </div>

      <!-- 静态图层配置 -->
      <div v-else-if="layerType === 'static'" class="layer-config">
        <h3>{{ $t('service.tile.staticLayerConfig') }}</h3>
        <div class="form-group">
          <label>{{ $t('service.tile.layerNameLabel') }} *</label>
          <input v-model="form.layerName" type="text" :placeholder="$t('service.tile.staticLayerNamePlaceholder')" />
        </div>
        <div class="form-group">
          <label>{{ $t('service.tile.layerTitleLabel') }} *</label>
          <input v-model="form.layerTitle" type="text" :placeholder="$t('service.tile.staticLayerTitlePlaceholder')" />
        </div>
        <div class="form-group">
          <label>{{ $t('service.tile.layerDescLabel') }}</label>
          <textarea v-model="form.layerDescription" :placeholder="$t('service.tile.layerDescPlaceholder')" rows="2"></textarea>
        </div>
        <div class="form-group">
          <label>{{ $t('service.tile.tilePathLabel') }} *</label>
          <input v-model="form.tilePath" type="text" :placeholder="$t('service.tile.tilePathPlaceholder')" />
          <p class="help-text">{{ $t('service.tile.tilePathHelp') }}</p>
        </div>
        <div class="form-group">
          <label>{{ $t('service.tile.tileFormatLabel') }} *</label>
          <select v-model="form.format">
            <option value="mvt">MVT (Mapbox Vector Tile)</option>
            <option value="png">PNG</option>
            <option value="jpeg">JPEG</option>
          </select>
        </div>
        <div class="form-row">
          <div class="form-group">
            <label>{{ $t('service.tile.minZoomLabel') }} *</label>
            <input v-model.number="form.minZoom" type="number" min="0" max="22" />
          </div>
          <div class="form-group">
            <label>{{ $t('service.tile.maxZoomLabel') }} *</label>
            <input v-model.number="form.maxZoom" type="number" min="0" max="22" />
          </div>
        </div>
      </div>

      <div class="form-actions">
        <button @click="$router.back()" class="btn btn-secondary">{{ $t('service.common.cancel') }}</button>
        <button @click="nextStep" class="btn btn-primary" :disabled="!validateStep0()">{{ $t('service.tile.nextStep') }}</button>
      </div>
    </div>

    <!-- Step 1: 配置服务信息 -->
    <div v-if="currentStep === 1" class="form-step">
      <h2>{{ $t('service.tile.serviceInfoTitle') }}</h2>
      <div class="form-group">
        <label>{{ $t('service.tile.serviceNameLabel') }} * <span class="help-text">{{ $t('service.tile.serviceNameHelp') }}</span></label>
        <input v-model="form.serviceName" type="text" :placeholder="$t('service.tile.serviceNamePlaceholder')" />
      </div>
      <div class="form-group">
        <label>{{ $t('service.tile.serviceTitleLabel') }} *</label>
        <input v-model="form.title" type="text" :placeholder="$t('service.tile.serviceTitlePlaceholder')" />
      </div>
      <div class="form-group">
        <label>{{ $t('service.tile.serviceDescLabel') }}</label>
        <textarea v-model="form.description" :placeholder="$t('service.tile.serviceDescPlaceholder')" rows="3"></textarea>
      </div>
      <div class="form-group">
        <label>{{ $t('service.tile.keywordsLabel') }}</label>
        <input v-model="form.keywords" type="text" :placeholder="$t('service.tile.keywordsPlaceholder')" />
      </div>

      <h3>{{ $t('service.tile.protocolConfigTitle') }}</h3>
      <div class="protocols-config">
        <label class="checkbox-label">
          <input type="checkbox" v-model="protocols.xyz.enabled" />
          {{ $t('service.tile.xyzProtocol') }}
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

      <h3>{{ $t('service.tile.accessControlTitle') }}</h3>
      <div class="form-group">
        <label class="checkbox-label">
          <input type="checkbox" v-model="form.publicAccess" />
          {{ $t('service.tile.publicAccessLabel') }}
        </label>
      </div>

      <div class="form-actions">
        <button @click="previousStep" class="btn btn-secondary">{{ $t('service.tile.prevStep') }}</button>
        <button @click="submitForm" class="btn btn-primary" :disabled="!validateStep1() || submitting">
          {{ submitting ? $t('service.tile.creating') : $t('service.tile.createBtn') }}
        </button>
      </div>
    </div>

    <!-- Step 2: 完成 -->
    <div v-if="currentStep === 2" class="form-step completion-step">
      <div class="success-icon">✓</div>
      <h2>{{ $t('service.tile.createSuccess') }}</h2>
      <div v-if="createdService" class="service-info">
        <p><strong>{{ $t('service.tile.colServiceName') }}:</strong> {{ createdService.service_name }}</p>
        <p><strong>{{ $t('service.tile.colTitle') }}:</strong> {{ createdService.title }}</p>
        <div v-if="createdService.endpoints" class="endpoints">
          <h3>{{ $t('service.tile.endpointsTitle') }}:</h3>
          <div v-if="createdService.endpoints.xyz_tiles" class="endpoint">
            <strong>XYZ Tiles:</strong>
            <code>{{ createdService.endpoints.xyz_tiles }}</code>
            <button @click="copyToClipboard(createdService.endpoints.xyz_tiles)" class="btn btn-sm btn-secondary">{{ $t('service.common.copy') }}</button>
          </div>
          <div v-if="createdService.endpoints.wmts" class="endpoint">
            <strong>WMTS:</strong>
            <code>{{ createdService.endpoints.wmts }}</code>
            <button @click="copyToClipboard(createdService.endpoints.wmts)" class="btn btn-sm btn-secondary">{{ $t('service.common.copy') }}</button>
          </div>
          <div v-if="createdService.endpoints.ogc_tiles" class="endpoint">
            <strong>OGC Tiles API:</strong>
            <code>{{ createdService.endpoints.ogc_tiles }}</code>
            <button @click="copyToClipboard(createdService.endpoints.ogc_tiles)" class="btn btn-sm btn-secondary">{{ $t('service.common.copy') }}</button>
          </div>
        </div>
      </div>
      <div v-else class="service-info">
        <p>{{ $t('service.common.loading') }}</p>
      </div>
      <div class="form-actions">
        <button @click="viewDetail" class="btn btn-primary">{{ $t('service.common.detail') }}</button>
        <button @click="createAnother" class="btn btn-secondary">{{ $t('service.tile.createAnother') }}</button>
      </div>
    </div>
  </div>
</template>

<script>
import tileServiceAPI from '@/api/tileService'
import { ResourceTreePicker, detectTableMetadata, locatorPathFromSelection } from '@common-ui'
import { ElMessage } from 'element-plus'
import { NATIVE_TABLE_ENGINE_TYPES, isNativeTableNode, isNativeTableVisibleNode } from '@/utils/resourceSelection'

export default {
  name: 'TileServiceForm',
  components: {
    ResourceTreePicker
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
        locator: '',
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
      return '/api/v1/meta'
    },
    nativeTableEngineTypes() {
      return NATIVE_TABLE_ENGINE_TYPES
    }
  },
  methods: {
    isNativeTableNode,
    isNativeTableVisibleNode,
    // 处理表选择（ResourceTreePicker 回调）
    async handleTableSelection(selection) {
      console.log('[TileServiceForm] Table selection:', selection)

      if (!selection) {
        // 清空选择
        this.form.engineId = null
        this.form.locator = ''
        this.form.schema = ''
        this.form.table = ''
        this.form.geomColumn = ''
        this.form.srid = 4326
        this.spatialMetadata = null
        return
      }

      const path = locatorPathFromSelection(selection)

      // 更新表单字段
      this.form.engineId = selection.identity?.engine_id
      this.form.locator = selection.identity?.locator || ''
      this.form.schema = path[0] || ''
      this.form.table = path[path.length - 1] || selection.display?.label || ''

      const geometry = await detectTableMetadata('/api/v1/meta', {
        locator: this.form.locator,
        item_id: selection.identity?.item_id
      })

      // 如果检测到几何列，自动填充
      if (geometry.has_geometry) {
        this.form.geomColumn = geometry.geometry_column
        this.form.srid = geometry.srid || 4326

        this.spatialMetadata = {
          hasGeometry: true,
          geometryColumn: geometry.geometry_column,
          srid: geometry.srid || 4326,
          geometryTypes: geometry.geometry_types || [],
          extent: geometry.extent
        }

        ElMessage.success(this.$t('service.tile.spatialDetectedMsg', { column: geometry.geometry_column }))
      } else {
        this.spatialMetadata = { hasGeometry: false }
        ElMessage.warning(this.$t('service.tile.noSpatialWarning'))
      }
    },

    validateStep0() {
      if (!this.form.layerName || !this.form.layerTitle) {
        return false
      }
      if (this.layerType === 'dynamic') {
        return this.form.locator && this.form.geomColumn
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
                locator: this.form.locator,
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
          throw new Error(this.$t('service.tile.invalidServiceData'))
        }

        this.createdService = response
        this.currentStep = 2
      } catch (error) {
        console.error('创建瓦片服务失败:', error)
        alert(this.$t('service.tile.createFailed') + ': ' + (error.response?.data?.error || error.message))
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
        locator: '',
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
        alert(this.$t('service.common.copied'))
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
