<template>
  <div class="published-service-test" v-loading="loading">
    <div class="page-header">
      <h2>{{ t('service.published.testTitle') }} - {{ service?.title }}</h2>
      <el-button @click="goBack">{{ t('service.common.back') }}</el-button>
    </div>

    <el-row :gutter="20">
      <!-- 左侧：测试配置 -->
      <el-col :span="10">
        <el-card :header="t('service.published.testConfig')">
          <el-form label-width="100px">
            <el-form-item :label="t('service.published.protocolType')">
              <el-select v-model="protocol" @change="handleProtocolChange" style="width: 100%;">
                <el-option
                  v-if="service?.enabled_ogc_api"
                  value="ogc_api"
                  label="OGC API Features"
                />
                <el-option
                  v-if="service?.enabled_wfs"
                  value="wfs"
                  label="WFS 2.0"
                />
                <el-option
                  v-if="service?.enabled_wmts"
                  value="wmts"
                  label="WMTS 1.0"
                />
              </el-select>
            </el-form-item>

            <!-- OGC API Features 参数 -->
            <template v-if="protocol === 'ogc_api'">
              <el-form-item :label="t('service.published.endpointLabel')">
                <el-select v-model="ogcApiEndpoint" style="width: 100%;">
                  <el-option value="landing" label="Landing Page" />
                  <el-option value="conformance" label="Conformance" />
                  <el-option value="collections" label="Collections" />
                  <el-option value="items" :label="t('service.published.itemsNeedLayer')" />
                </el-select>
              </el-form-item>

              <el-form-item v-if="ogcApiEndpoint === 'items'" :label="t('service.published.selectLayerLabel')">
                <el-select v-model="selectedLayer" style="width: 100%;">
                  <el-option
                    v-for="layer in service?.layers"
                    :key="layer.id"
                    :value="layer.layer_name"
                    :label="layer.title"
                  />
                </el-select>
              </el-form-item>

              <el-form-item v-if="ogcApiEndpoint === 'items'" label="Limit">
                <el-input-number v-model="params.limit" :min="1" :max="1000" style="width: 100%;" />
              </el-form-item>

              <el-form-item v-if="ogcApiEndpoint === 'items'" label="Offset">
                <el-input-number v-model="params.offset" :min="0" style="width: 100%;" />
              </el-form-item>

              <el-form-item v-if="ogcApiEndpoint === 'items'" label="CRS">
                <el-select v-model="params.crs" style="width: 100%;">
                  <el-option value="4326" label="EPSG:4326" />
                  <el-option value="3857" label="EPSG:3857" />
                  <el-option value="4490" label="EPSG:4490" />
                </el-select>
              </el-form-item>
            </template>

            <!-- WFS 参数 -->
            <template v-if="protocol === 'wfs'">
              <el-form-item :label="t('service.published.requestType')">
                <el-select v-model="wfsRequest" style="width: 100%;">
                  <el-option value="GetCapabilities" label="GetCapabilities" />
                  <el-option value="DescribeFeatureType" label="DescribeFeatureType" />
                  <el-option value="GetFeature" label="GetFeature" />
                </el-select>
              </el-form-item>

              <el-form-item v-if="wfsRequest === 'GetFeature'" label="TypeName">
                <el-select v-model="selectedLayer" style="width: 100%;">
                  <el-option
                    v-for="layer in service?.layers"
                    :key="layer.id"
                    :value="layer.layer_name"
                    :label="layer.title"
                  />
                </el-select>
              </el-form-item>

              <el-form-item v-if="wfsRequest === 'GetFeature'" label="Count">
                <el-input-number v-model="params.count" :min="1" :max="1000" style="width: 100%;" />
              </el-form-item>
            </template>

            <!-- WMTS 参数 -->
            <template v-if="protocol === 'wmts'">
              <el-form-item :label="t('service.published.requestType')">
                <el-select v-model="wmtsRequest" style="width: 100%;">
                  <el-option value="GetCapabilities" label="GetCapabilities" />
                  <el-option value="GetTile" label="GetTile" />
                </el-select>
              </el-form-item>

              <el-form-item v-if="wmtsRequest === 'GetTile'" :label="t('service.published.selectLayerLabel')">
                <el-select v-model="selectedLayer" style="width: 100%;">
                  <el-option
                    v-for="layer in service?.layers"
                    :key="layer.id"
                    :value="layer.layer_name"
                    :label="layer.title"
                  />
                </el-select>
              </el-form-item>

              <el-form-item v-if="wmtsRequest === 'GetTile'" :label="t('service.published.tileCoords')">
                <div style="display: flex; gap: 10px;">
                  <el-input v-model="params.z" placeholder="Z" />
                  <el-input v-model="params.x" placeholder="X" />
                  <el-input v-model="params.y" placeholder="Y" />
                </div>
                <div class="help-text">{{ t('service.published.tileCoordsExample') }}</div>
              </el-form-item>
            </template>

            <el-form-item>
              <el-button type="primary" @click="executeRequest" :loading="executing" style="width: 100%;">
                {{ t('service.published.executeRequest') }}
              </el-button>
            </el-form-item>
          </el-form>

          <!-- 请求 URL 预览 -->
          <el-divider />
          <div>
            <strong>{{ t('service.published.requestUrl') }}</strong>
            <el-input
              :value="requestURL"
              readonly
              type="textarea"
              :rows="4"
              style="margin-top: 10px;"
            />
          </div>
        </el-card>
      </el-col>

      <!-- 右侧：响应展示 -->
      <el-col :span="14">
        <el-card>
          <template #header>
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <span>{{ t('service.published.responseResult') }}</span>
              <div v-if="response">
                <el-tag :type="response.status === 200 ? 'success' : 'danger'">
                  HTTP {{ response.status }}
                </el-tag>
                <span style="margin-left: 10px; color: var(--addp-text-tertiary);">
                  {{ t('service.published.duration') }}: {{ response.duration }}ms
                </span>
              </div>
            </div>
          </template>

          <div v-if="!response" class="empty-state">
            <el-empty :description="t('service.published.configAndExecute')" />
          </div>

          <div v-else>
            <!-- 响应内容 -->
            <div class="response-content">
              <pre class="json-view">{{ formatResponse(response.data) }}</pre>
            </div>
          </div>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import axios from 'axios'
import publishedServiceAPI from "../api/publishedService"
import { navigateServiceRoute } from '@/utils/moduleNavigation'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const goBack = () => navigateServiceRoute(router, `/published-services/${route.params.id}`, { history: 'replace' })

const service = ref(null)
const loading = ref(true)
const executing = ref(false)
const protocol = ref('ogc_api')
const ogcApiEndpoint = ref('landing')
const wfsRequest = ref('GetCapabilities')
const wmtsRequest = ref('GetCapabilities')
const selectedLayer = ref(null)

const params = ref({
  limit: 10,
  offset: 0,
  crs: '4326',
  count: 10,
  z: '10',
  x: '512',
  y: '384'
})

const response = ref(null)

// 计算请求 URL
const requestURL = computed(() => {
  const serviceName = service.value?.service_name
  if (!serviceName) return ''

  const apiBase = import.meta.env.DEV ? 'http://localhost:8086' : window.location.origin
  const base = `${apiBase}/ogc`

  if (protocol.value === 'ogc_api') {
    if (ogcApiEndpoint.value === 'landing') {
      return `${base}/api/${serviceName}/`
    } else if (ogcApiEndpoint.value === 'conformance') {
      return `${base}/api/${serviceName}/conformance`
    } else if (ogcApiEndpoint.value === 'collections') {
      return `${base}/api/${serviceName}/collections`
    } else if (ogcApiEndpoint.value === 'items' && selectedLayer.value) {
      return `${base}/api/${serviceName}/collections/${selectedLayer.value}/items?limit=${params.value.limit}&offset=${params.value.offset}&crs=EPSG:${params.value.crs}`
    }
  } else if (protocol.value === 'wfs') {
    let url = `${base}/wfs/${serviceName}?service=WFS&version=2.0.0&request=${wfsRequest.value}`
    if (wfsRequest.value === 'GetFeature' && selectedLayer.value) {
      url += `&typeName=${selectedLayer.value}&count=${params.value.count}`
    }
    return url
  } else if (protocol.value === 'wmts') {
    if (wmtsRequest.value === 'GetCapabilities') {
      return `${base}/wmts/${serviceName}?service=WMTS&version=1.0.0&request=GetCapabilities`
    } else if (wmtsRequest.value === 'GetTile' && selectedLayer.value) {
      return `${base}/wmts/${serviceName}/tile/${selectedLayer.value}/${params.value.z}/${params.value.x}/${params.value.y}.mvt`
    }
  }

  return ''
})

// 加载服务
const loadService = async () => {
  loading.value = true
  try {
    // createAPIClient 默认 extractData=true，已经提取了 response.data
    const service_data = await publishedServiceAPI.getService(route.params.id)
    service.value = service_data

    // 设置默认协议
    if (service.value.enabled_ogc_api) {
      protocol.value = 'ogc_api'
    } else if (service.value.enabled_wfs) {
      protocol.value = 'wfs'
    } else if (service.value.enabled_wmts) {
      protocol.value = 'wmts'
    }

    // 默认选择第一个图层
    if (service.value.layers?.length > 0) {
      selectedLayer.value = service.value.layers[0].layer_name
    }
  } catch (error) {
    ElMessage.error(t('service.published.loadFailed') + ': ' + (error.message || t('service.common.unknownError')))
  } finally {
    loading.value = false
  }
}

// 协议切换处理
const handleProtocolChange = () => {
  response.value = null
  if (protocol.value === 'ogc_api') {
    ogcApiEndpoint.value = 'landing'
  } else if (protocol.value === 'wfs') {
    wfsRequest.value = 'GetCapabilities'
  } else if (protocol.value === 'wmts') {
    wmtsRequest.value = 'GetCapabilities'
  }
}

// 执行请求
const executeRequest = async () => {
  if (!requestURL.value) {
    ElMessage.warning(t('service.published.configComplete'))
    return
  }

  executing.value = true
  const startTime = Date.now()

  try {
    const res = await axios.get(requestURL.value, {
      // 对于 XML 响应，不要自动解析为 JSON
      responseType: protocol.value === 'wfs' || (protocol.value === 'wmts' && wmtsRequest.value === 'GetCapabilities') ? 'text' : 'json'
    })
    const duration = Date.now() - startTime

    response.value = {
      status: res.status,
      data: res.data,
      duration
    }

    ElMessage.success(t('service.published.requestSuccess'))
  } catch (error) {
    const duration = Date.now() - startTime
    response.value = {
      status: error.response?.status || 500,
      data: error.response?.data || { error: error.message },
      duration
    }
    ElMessage.error(t('service.published.requestFailed') + ': ' + (error.message || t('service.common.unknownError')))
  } finally {
    executing.value = false
  }
}

// 格式化响应数据
const formatResponse = (data) => {
  if (typeof data === 'string') {
    return data
  }
  return JSON.stringify(data, null, 2)
}

onMounted(loadService)
</script>

<style scoped>
.published-service-test {
  padding: 20px;
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
}

.page-header h2 {
  margin: 0;
  font-size: 24px;
  color: var(--addp-text-primary);
}

.empty-state {
  padding: 60px 0;
  text-align: center;
}

.response-content {
  max-height: 600px;
  overflow: auto;
}

.json-view {
  background: var(--addp-bg-secondary);
  padding: 16px;
  border-radius: 4px;
  font-size: 12px;
  line-height: 1.5;
  margin: 0;
  white-space: pre-wrap;
  word-wrap: break-word;
}

.help-text {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  margin-top: 4px;
}

:deep(.el-card__header) {
  font-weight: 600;
  font-size: 16px;
}
</style>
