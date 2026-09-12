<template>
  <div class="runtime-page" v-loading="loading" data-testid="data-application-runtime">
    <div v-if="pageError" class="runtime-error">
      <el-alert
        :type="accessDenied ? 'warning' : 'error'"
        :closable="false"
        show-icon
        :title="accessDenied ? t('workbench.runtimeAccessDeniedTitle') : pageError"
        :description="accessDenied ? t('workbench.runtimeAccessDeniedDescription') : ''"
      />
      <div class="runtime-error-actions">
        <template v-if="accessDenied">
          <el-button data-testid="runtime-open-portal-action" type="primary" @click="openPortalAssetSearch">
            {{ t('workbench.openAssetPortal') }}
          </el-button>
          <el-button data-testid="runtime-open-applications-action" @click="openPortalApplications">
            {{ t('workbench.openMyApplications') }}
          </el-button>
        </template>
        <el-button data-testid="runtime-retry-action" :type="accessDenied ? 'default' : 'primary'" :loading="loading" @click="load(route.params.id)">
          {{ t('workbench.retry') }}
        </el-button>
      </div>
    </div>
    <DataApplicationCanvas v-else-if="application" :key="`${application.id}:${initialPresetKey}`" :application="application" :initial-preset-key="initialPresetKey" />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { createLatestRequestCoordinator } from '@common-ui'
import { getDataApplicationRuntime } from '../api/dataApplications'
import { applicationParameterPreset, commitLatestDataApplicationLoad, isDataApplicationRuntimeAccessDenied } from '../utils/dataApplicationRuntime.mjs'
import { openPortalApplications, openPortalAssetSearch } from '../utils/moduleNavigation'
import DataApplicationCanvas from '../components/DataApplicationCanvas.vue'

const { t } = useI18n()
const route = useRoute()
const loading = ref(false)
const pageError = ref('')
const accessDenied = ref(false)
const application = ref(null)
const requests = createLatestRequestCoordinator()
const initialPresetKey = computed(() => applicationParameterPreset(application.value?.snapshot, route.query.preset)?.key || '')

async function load(applicationID) {
  const targetID = String(applicationID || '').trim()
  const request = requests.begin(targetID)
  loading.value = true
  pageError.value = ''
  accessDenied.value = false
  application.value = null
  try {
    const { data } = await getDataApplicationRuntime(targetID)
    commitLatestDataApplicationLoad(requests, request, route.params.id, () => {
      application.value = data
    })
  } catch (error) {
    commitLatestDataApplicationLoad(requests, request, route.params.id, () => {
      accessDenied.value = isDataApplicationRuntimeAccessDenied(error)
      pageError.value = error?.response?.data?.error || t('workbench.loadFailed')
    })
  } finally {
    commitLatestDataApplicationLoad(requests, request, route.params.id, () => {
      loading.value = false
    })
  }
}

onBeforeUnmount(() => requests.invalidate())
watch(() => route.params.id, load, { immediate: true })
</script>

<style scoped>
.runtime-page{min-height:100vh;background:var(--addp-bg-secondary)}
.runtime-error{display:flex;flex-direction:column;align-items:flex-start;gap:12px;padding:24px}
.runtime-error>.el-alert{width:100%}
.runtime-error-actions{display:flex;flex-wrap:wrap;gap:8px}
</style>
