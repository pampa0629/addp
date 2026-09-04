<template>
  <div class="runtime-page" v-loading="loading" data-testid="data-application-runtime">
    <el-alert v-if="pageError" type="error" :closable="false" :title="pageError" />
    <DataApplicationCanvas v-else-if="application" :application="application" />
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { getDataApplicationRuntime } from '../api/dataApplications'
import DataApplicationCanvas from '../components/DataApplicationCanvas.vue'

const { t } = useI18n()
const route = useRoute()
const loading = ref(false)
const pageError = ref('')
const application = ref(null)

async function load() {
  loading.value = true
  pageError.value = ''
  try {
    const { data } = await getDataApplicationRuntime(route.params.id)
    application.value = data
  } catch (error) {
    pageError.value = error?.response?.data?.error || t('workbench.loadFailed')
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<style scoped>
.runtime-page{min-height:100vh;background:var(--addp-bg-secondary)}.runtime-page>.el-alert{margin:24px;width:auto}
</style>
