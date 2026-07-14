<template>
  <div class="tile-service-form">
    <div class="page-header">
      <h2>{{ isEditing ? $t('service.tile.formEditTitle') : $t('service.tile.formCreateTitle') }}</h2>
    </div>

    <el-steps :active="currentStep" finish-status="success" align-center class="service-steps">
      <el-step :title="$t('service.tile.step1Title')" />
      <el-step :title="$t('service.tile.step2Title')" />
      <el-step :title="$t('service.tile.step3Title')" />
    </el-steps>

    <!-- Step 0: 添加第一个图层 -->
    <el-card v-if="currentStep === 0" class="form-card" shadow="never">
      <template #header>
        <span>{{ $t('service.tile.selectLayerType') }}</span>
      </template>

      <el-radio-group v-model="layerType" class="layer-type-selector">
        <div
          class="layer-type-option"
          :class="{ selected: layerType === 'dynamic' }"
          @click="layerType = 'dynamic'"
        >
          <el-radio value="dynamic">
            <div class="layer-type-content">
              <h3>{{ $t('service.tile.dynamicLayer') }}</h3>
              <p>{{ $t('service.tile.dynamicLayerDesc') }}</p>
            </div>
          </el-radio>
        </div>
        <div
          class="layer-type-option"
          :class="{ selected: layerType === 'static' }"
          @click="layerType = 'static'"
        >
          <el-radio value="static">
            <div class="layer-type-content">
              <h3>{{ $t('service.tile.staticLayer') }}</h3>
              <p>{{ $t('service.tile.staticLayerDesc') }}</p>
            </div>
          </el-radio>
        </div>
      </el-radio-group>

      <!-- 动态图层配置 -->
      <div v-if="layerType === 'dynamic'" class="layer-config">
        <el-divider content-position="left">{{ $t('service.tile.dynamicLayerConfig') }}</el-divider>
        <el-form :model="form" label-position="top">
          <el-form-item :label="$t('service.tile.layerNameLabel')" required>
            <el-input v-model="form.layerName" :placeholder="$t('service.tile.layerNamePlaceholder')" />
          </el-form-item>
          <el-form-item :label="$t('service.tile.layerTitleLabel')" required>
            <el-input v-model="form.layerTitle" :placeholder="$t('service.tile.layerTitlePlaceholder')" />
          </el-form-item>
          <el-form-item :label="$t('service.tile.layerDescLabel')">
            <el-input
              v-model="form.layerDescription"
              type="textarea"
              :rows="2"
              :placeholder="$t('service.tile.layerDescPlaceholder')"
            />
          </el-form-item>
          <el-form-item :label="$t('service.tile.selectTableLabel')" required>
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
          </el-form-item>
        </el-form>

        <!-- 显示检测到的空间字段信息 -->
        <el-alert
          v-if="spatialMetadata && spatialMetadata.hasGeometry"
          :title="$t('service.tile.spatialDetected')"
          type="success"
          :closable="false"
          show-icon
        >
          <div class="geometry-details">
            <span><strong>{{ $t('service.tile.geomColumnLabel') }}：</strong>{{ spatialMetadata.geometryColumn }}</span>
            <span><strong>SRID：</strong>{{ spatialMetadata.srid }}</span>
            <span v-if="spatialMetadata.geometryTypes && spatialMetadata.geometryTypes.length">
              <strong>{{ $t('service.tile.geomTypeLabel') }}：</strong>{{ spatialMetadata.geometryTypes.join(', ') }}
            </span>
          </div>
        </el-alert>
      </div>

      <!-- 静态图层配置 -->
      <div v-else-if="layerType === 'static'" class="layer-config">
        <el-divider content-position="left">{{ $t('service.tile.staticLayerConfig') }}</el-divider>
        <el-form :model="form" label-position="top">
          <el-form-item :label="$t('service.tile.layerNameLabel')" required>
            <el-input v-model="form.layerName" :placeholder="$t('service.tile.staticLayerNamePlaceholder')" />
          </el-form-item>
          <el-form-item :label="$t('service.tile.layerTitleLabel')" required>
            <el-input v-model="form.layerTitle" :placeholder="$t('service.tile.staticLayerTitlePlaceholder')" />
          </el-form-item>
          <el-form-item :label="$t('service.tile.layerDescLabel')">
            <el-input
              v-model="form.layerDescription"
              type="textarea"
              :rows="2"
              :placeholder="$t('service.tile.layerDescPlaceholder')"
            />
          </el-form-item>
          <el-form-item :label="$t('service.tile.tilePathLabel')" required>
            <el-input v-model="form.tilePath" :placeholder="$t('service.tile.tilePathPlaceholder')" />
            <div class="help-text">{{ $t('service.tile.tilePathHelp') }}</div>
          </el-form-item>
          <el-form-item :label="$t('service.tile.tileFormatLabel')" required>
            <el-select v-model="form.format" class="full-width-control">
              <el-option label="MVT (Mapbox Vector Tile)" value="mvt" />
              <el-option label="PNG" value="png" />
              <el-option label="JPEG" value="jpeg" />
            </el-select>
          </el-form-item>
          <div class="form-row">
            <el-form-item :label="$t('service.tile.minZoomLabel')" required>
              <el-input-number v-model="form.minZoom" :min="0" :max="22" controls-position="right" />
            </el-form-item>
            <el-form-item :label="$t('service.tile.maxZoomLabel')" required>
              <el-input-number v-model="form.maxZoom" :min="0" :max="22" controls-position="right" />
            </el-form-item>
          </div>
        </el-form>
      </div>

      <div class="form-actions">
        <el-button @click="$router.back()">{{ $t('service.common.cancel') }}</el-button>
        <el-button type="primary" :disabled="!validateStep0()" @click="nextStep">
          {{ $t('service.tile.nextStep') }}
        </el-button>
      </div>
    </el-card>

    <!-- Step 1: 配置服务信息 -->
    <el-card v-if="currentStep === 1" class="form-card" shadow="never">
      <template #header>
        <span>{{ $t('service.tile.serviceInfoTitle') }}</span>
      </template>

      <el-form :model="form" label-position="top">
        <el-form-item :label="$t('service.tile.serviceNameLabel')" required>
          <el-input v-model="form.serviceName" :placeholder="$t('service.tile.serviceNamePlaceholder')" />
          <div class="help-text">{{ $t('service.tile.serviceNameHelp') }}</div>
        </el-form-item>
        <el-form-item :label="$t('service.tile.serviceTitleLabel')" required>
          <el-input v-model="form.title" :placeholder="$t('service.tile.serviceTitlePlaceholder')" />
        </el-form-item>
        <el-form-item :label="$t('service.tile.serviceDescLabel')">
          <el-input
            v-model="form.description"
            type="textarea"
            :rows="3"
            :placeholder="$t('service.tile.serviceDescPlaceholder')"
          />
        </el-form-item>
        <el-form-item :label="$t('service.tile.keywordsLabel')">
          <el-input v-model="form.keywords" :placeholder="$t('service.tile.keywordsPlaceholder')" />
        </el-form-item>

        <el-divider content-position="left">{{ $t('service.tile.protocolConfigTitle') }}</el-divider>
        <el-form-item>
          <div class="protocols-config">
            <el-checkbox v-model="protocols.xyz.enabled">{{ $t('service.tile.xyzProtocol') }}</el-checkbox>
            <el-checkbox v-model="protocols.ogc_tiles.enabled">OGC Tiles API</el-checkbox>
            <el-checkbox v-model="protocols.tms.enabled">TMS</el-checkbox>
          </div>
        </el-form-item>

        <el-divider content-position="left">{{ $t('service.tile.accessControlTitle') }}</el-divider>
        <el-form-item>
          <el-checkbox v-model="form.publicAccess">{{ $t('service.tile.publicAccessLabel') }}</el-checkbox>
        </el-form-item>
      </el-form>

      <div class="form-actions">
        <el-button @click="previousStep">{{ $t('service.tile.prevStep') }}</el-button>
        <el-button
          type="primary"
          :disabled="!validateStep1()"
          :loading="submitting"
          @click="submitForm"
        >
          {{ $t('service.tile.createBtn') }}
        </el-button>
      </div>
    </el-card>

    <!-- Step 2: 完成 -->
    <el-card v-if="currentStep === 2" class="form-card completion-step" shadow="never">
      <el-result icon="success" :title="$t('service.tile.createSuccess')" />
      <div v-if="createdService" class="service-info">
        <el-descriptions :column="1" border>
          <el-descriptions-item :label="$t('service.tile.colServiceName')">
            {{ createdService.service_name }}
          </el-descriptions-item>
          <el-descriptions-item :label="$t('service.tile.colTitle')">
            {{ createdService.title }}
          </el-descriptions-item>
        </el-descriptions>
        <div v-if="createdService.endpoints" class="endpoints">
          <el-divider content-position="left">{{ $t('service.tile.endpointsTitle') }}</el-divider>
          <div v-if="createdService.endpoints.xyz_tiles" class="endpoint">
            <span class="endpoint-label">XYZ Tiles</span>
            <el-input :model-value="createdService.endpoints.xyz_tiles" readonly>
              <template #append>
                <el-tooltip :content="$t('service.common.copy')" placement="top">
                  <el-button :aria-label="$t('service.common.copy')" @click="copyToClipboard(createdService.endpoints.xyz_tiles)">
                    <el-icon><DocumentCopy /></el-icon>
                  </el-button>
                </el-tooltip>
              </template>
            </el-input>
          </div>
          <div v-if="createdService.endpoints.wmts" class="endpoint">
            <span class="endpoint-label">WMTS</span>
            <el-input :model-value="createdService.endpoints.wmts" readonly>
              <template #append>
                <el-tooltip :content="$t('service.common.copy')" placement="top">
                  <el-button :aria-label="$t('service.common.copy')" @click="copyToClipboard(createdService.endpoints.wmts)">
                    <el-icon><DocumentCopy /></el-icon>
                  </el-button>
                </el-tooltip>
              </template>
            </el-input>
          </div>
          <div v-if="createdService.endpoints.ogc_tiles" class="endpoint">
            <span class="endpoint-label">OGC Tiles API</span>
            <el-input :model-value="createdService.endpoints.ogc_tiles" readonly>
              <template #append>
                <el-tooltip :content="$t('service.common.copy')" placement="top">
                  <el-button :aria-label="$t('service.common.copy')" @click="copyToClipboard(createdService.endpoints.ogc_tiles)">
                    <el-icon><DocumentCopy /></el-icon>
                  </el-button>
                </el-tooltip>
              </template>
            </el-input>
          </div>
        </div>
      </div>
      <el-skeleton v-else :rows="3" animated />
      <div class="form-actions">
        <el-button type="primary" @click="viewDetail">{{ $t('service.common.detail') }}</el-button>
        <el-button @click="createAnother">{{ $t('service.tile.createAnother') }}</el-button>
      </div>
    </el-card>
  </div>
</template>

<script>
import tileServiceAPI from '@/api/tileService'
import { ResourceTreePicker, detectTableMetadata, locatorPathFromSelection } from '@common-ui'
import { ElMessage } from 'element-plus'
import { DocumentCopy } from '@element-plus/icons-vue'
import { NATIVE_TABLE_ENGINE_TYPES, isNativeTableNode, isNativeTableVisibleNode } from '@/utils/resourceSelection'

export default {
  name: 'TileServiceForm',
  components: {
    DocumentCopy,
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
        ElMessage.error(this.$t('service.tile.createFailed') + ': ' + (error.response?.data?.error || error.message))
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

    async copyToClipboard(text) {
      try {
        await navigator.clipboard.writeText(text)
        ElMessage.success(this.$t('service.common.copied'))
      } catch (err) {
        console.error('复制失败:', err)
      }
    }
  }
}
</script>

<style scoped>
.tile-service-form {
  max-width: 1080px;
  margin: 0 auto;
  padding: 24px;
  color: var(--addp-text-primary);
}

.page-header {
  margin-bottom: 24px;
}

.page-header h2 {
  margin: 0;
  font-size: 24px;
  color: var(--addp-text-primary);
}

.service-steps {
  margin-bottom: 32px;
}

.layer-type-selector {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  width: 100%;
  margin-bottom: 24px;
}

.layer-type-option {
  min-width: 0;
  padding: 20px;
  border: 2px solid var(--addp-border-color);
  border-radius: 8px;
  cursor: pointer;
  background: var(--addp-bg-primary) !important;
}

.layer-type-option:hover,
.layer-type-option.selected {
  border-color: var(--el-color-primary);
}

.layer-type-option.selected {
  background: var(--el-color-primary-light-9) !important;
}

.layer-type-option :deep(.el-radio) {
  align-items: flex-start;
  width: 100%;
  height: auto;
  margin: 0;
}

.layer-type-option :deep(.el-radio__input) {
  margin-top: 3px;
}

.layer-type-option :deep(.el-radio__label) {
  min-width: 0;
  padding-left: 12px;
  white-space: normal;
}

.layer-type-content h3 {
  margin: 0 0 8px;
  color: var(--addp-text-primary);
  font-size: 16px;
  line-height: 1.4;
}

.layer-type-content p {
  margin: 0;
  color: var(--addp-text-secondary);
  font-size: 14px;
  line-height: 1.6;
}

.help-text {
  width: 100%;
  margin-top: 4px;
  font-size: 12px;
  color: var(--addp-text-tertiary);
}

.full-width-control,
.form-row :deep(.el-input-number) {
  width: 100%;
}

.layer-config :deep(.el-form-item:last-child) {
  margin-bottom: 0;
}

.geometry-details {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 24px;
}

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
}

.protocols-config {
  display: flex;
  flex-wrap: wrap;
  gap: 12px 24px;
}

.form-actions {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid var(--addp-border-color);
}

.service-info {
  max-width: 760px;
  margin: 0 auto;
}

.endpoints {
  margin-top: 24px;
}

.endpoint {
  display: grid;
  gap: 8px;
  margin-bottom: 16px;
}

.endpoint-label {
  color: var(--addp-text-primary);
  font-size: 14px;
  font-weight: 500;
}

.completion-step .form-actions {
  justify-content: center;
}

.form-card {
  background: var(--addp-bg-primary) !important;
  border-color: var(--addp-border-color) !important;
}

.form-card :deep(.el-card__header) {
  color: var(--addp-text-primary);
  font-size: 18px;
  font-weight: 600;
}

.form-card :deep(.el-form-item__label),
.form-card :deep(.el-divider__text) {
  color: var(--addp-text-primary);
}

.form-card :deep(.el-divider__text) {
  background: var(--addp-bg-primary) !important;
}

.form-card :deep(.el-result__title p) {
  color: var(--addp-text-primary);
}

.form-card :deep(.el-descriptions__label),
.form-card :deep(.el-descriptions__content) {
  color: var(--addp-text-primary) !important;
  background: var(--addp-bg-primary) !important;
}

@media (max-width: 768px) {
  .tile-service-form {
    padding: 16px;
  }

  .layer-type-selector,
  .form-row {
    grid-template-columns: 1fr;
  }

  .form-actions {
    flex-wrap: wrap;
  }
}
</style>
