<template>
  <section class="quick-view-policy">
    <header class="policy-header">
      <div>
        <h2>{{ t('manager.quickViewPolicy.title') }}</h2>
        <p>{{ t('manager.quickViewPolicy.description') }}</p>
      </div>
      <el-button :icon="Refresh" circle :loading="loading" :title="t('manager.quickViewPolicy.reload')" @click="load" />
    </header>

    <el-form v-loading="loading" :model="form" label-position="top" class="policy-form">
      <el-form-item :label="t('manager.quickViewPolicy.flatgeobufRows')">
        <el-input-number v-model="form.directFlatGeobufMaxRows" :min="1" :max="1000000" controls-position="right" />
      </el-form-item>
      <el-form-item :label="t('manager.quickViewPolicy.mvtTimeout')">
        <el-input-number v-model="form.realtimeTileTimeoutMS" :min="100" :max="120000" controls-position="right" />
      </el-form-item>
      <el-form-item :label="t('manager.quickViewPolicy.retryAfter')">
        <el-input-number v-model="form.realtimeTileRetryAfterSec" :min="1" :max="3600" controls-position="right" />
      </el-form-item>
      <footer class="form-actions">
        <el-button type="primary" :icon="Check" :loading="saving" :disabled="!canUpdate" @click="save">
          {{ t('manager.quickViewPolicy.save') }}
        </el-button>
      </footer>
    </el-form>
  </section>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../store/auth'
import { getQuickViewPolicy, updateQuickViewPolicy } from '../api/embeddingConfiguration'

const { t } = useI18n()
const authStore = useAuthStore()
const loading = ref(false)
const saving = ref(false)
const canUpdate = computed(() => authStore.hasPermission('manager.configuration.update'))
const form = reactive({ version: 0, directFlatGeobufMaxRows: 2000, realtimeTileTimeoutMS: 2500, realtimeTileRetryAfterSec: 60 })

async function load() {
  loading.value = true
  try {
    const value = await getQuickViewPolicy()
    Object.assign(form, {
      version: value.version || 0,
      directFlatGeobufMaxRows: value.direct_flatgeobuf_max_rows,
      realtimeTileTimeoutMS: value.realtime_tile_timeout_ms,
      realtimeTileRetryAfterSec: value.realtime_tile_retry_after_sec
    })
  } catch (error) {
    ElMessage.error(error?.response?.data?.error || t('manager.quickViewPolicy.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  saving.value = true
  try {
    await updateQuickViewPolicy({
      version: form.version,
      direct_flatgeobuf_max_rows: form.directFlatGeobufMaxRows,
      realtime_tile_timeout_ms: form.realtimeTileTimeoutMS,
      realtime_tile_retry_after_sec: form.realtimeTileRetryAfterSec
    })
    ElMessage.success(t('manager.quickViewPolicy.saveSuccess'))
    await load()
  } catch (error) {
    if (error?.response?.status === 409) {
      ElMessage.warning(t('manager.quickViewPolicy.versionConflict'))
      await load()
    } else {
      ElMessage.error(error?.response?.data?.error || t('manager.quickViewPolicy.saveFailed'))
    }
  } finally {
    saving.value = false
  }
}

load()
</script>

<style scoped>
.quick-view-policy { padding: 8px 0 16px; }
.policy-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 24px; }
.policy-header h2 { margin: 0 0 6px; color: var(--addp-text-primary); font-size: 20px; font-weight: 600; letter-spacing: 0; }
.policy-header p { margin: 0; color: var(--addp-text-secondary); font-size: 14px; }
.policy-form { max-width: 560px; }
.form-actions { display: flex; justify-content: flex-end; margin-top: 12px; }
</style>
