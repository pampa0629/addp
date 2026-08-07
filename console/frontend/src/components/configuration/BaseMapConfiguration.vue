<template>
  <section class="resource-configuration">
    <header class="resource-header"><div><h2>{{ t('console.configuration.resources.baseMap.title') }}</h2><p>{{ contextLabel }}</p></div><el-button :icon="Refresh" circle :loading="loading" @click="load" /></header>
    <el-table v-loading="loading" :data="rows" class="map-table">
      <el-table-column :label="t('console.configuration.resources.baseMap.provider')" width="170"><template #default="{ row }"><strong>{{ row.provider }}</strong></template></el-table-column>
      <el-table-column :label="t('console.configuration.resources.baseMap.enabled')" width="110"><template #default="{ row }"><el-switch v-model="row.enabled" :disabled="!canUpdate" /></template></el-table-column>
      <el-table-column :label="t('console.configuration.resources.baseMap.order')" width="100"><template #default="{ row }"><el-input-number v-model="row.sort_order" :min="0" :max="999" controls-position="right" :disabled="!canUpdate" /></template></el-table-column>
      <el-table-column :label="t('console.configuration.resources.baseMap.key')" min-width="260"><template #default="{ row }"><el-input v-if="row.provider !== 'osm'" v-model="row.key" type="password" show-password :placeholder="row.configured ? t('console.configuration.resources.configured') : t('console.configuration.resources.baseMap.enterKey')" :disabled="!canUpdate" /><span v-else class="not-applicable">-</span></template></el-table-column>
      <el-table-column :label="t('console.configuration.resources.baseMap.securityJsCode')" min-width="260"><template #default="{ row }"><el-input v-if="row.provider === 'amap'" v-model="row.security_js_code" type="password" show-password :placeholder="row.security_configured ? t('console.configuration.resources.configured') : t('console.configuration.resources.baseMap.enterSecurityJsCode')" :disabled="!canUpdate" /><span v-else class="not-applicable">-</span></template></el-table-column>
      <el-table-column width="110" align="right"><template #default="{ row }"><el-button type="primary" :icon="Check" :loading="row.saving" :disabled="!canUpdate" @click="save(row)">{{ t('console.configuration.save') }}</el-button></template></el-table-column>
    </el-table>
  </section>
</template>
<script setup>
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../../store/auth'
import { moduleConfigurationAPI } from '../../api/moduleConfiguration'
const { t } = useI18n(); const authStore = useAuthStore(); const loading = ref(false); const rows = ref([])
const canUpdate = computed(() => authStore.hasPermission('manager.configuration.update')); const contextLabel = computed(() => authStore.contextType === 'platform' ? t('console.configuration.platformContext') : t('console.configuration.tenantContext'))
const providers = ['osm', 'amap', 'tianditu']
async function load() { loading.value = true; try { const result = await moduleConfigurationAPI.listBaseMapProviders(); const configured = new Map((result.data || []).map((item) => [item.provider, item])); rows.value = providers.map((provider) => { const item = configured.get(provider) || {}; return { provider, version: item.version || 0, enabled: item.enabled ?? provider === 'osm', sort_order: item.sort_order || 0, configured: item.amap_key_configured || item.tdt_key_configured, security_configured: item.amap_security_js_code_configured, key: '', security_js_code: '', saving: false } }) } catch (error) { ElMessage.error(error?.response?.data?.error || t('console.configuration.loadFailed')) } finally { loading.value = false } }
async function save(row) { row.saving = true; try { const payload = { version: row.version, provider: row.provider, enabled: row.enabled, sort_order: row.sort_order }; if (row.provider === 'amap') { payload.amap_key = row.key; payload.amap_security_js_code = row.security_js_code } if (row.provider === 'tianditu') payload.tdt_key = row.key; const result = await moduleConfigurationAPI.updateBaseMapProvider(payload); Object.assign(row, { ...result, configured: row.key ? true : row.configured, security_configured: row.security_js_code ? true : row.security_configured, key: '', security_js_code: '' }); ElMessage.success(t('console.configuration.saveSuccess')) } catch (error) { ElMessage.error(error?.response?.data?.error || t('console.configuration.saveFailed')) } finally { row.saving = false } }
onMounted(load)
</script>
<style scoped>.resource-configuration{width:100%}.resource-header{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:20px}.resource-header h2{margin:0 0 6px;color:var(--addp-text-primary);font-size:20px;font-weight:600;letter-spacing:0}.resource-header p,.not-applicable{margin:0;color:var(--addp-text-secondary);font-size:14px}.map-table{width:100%}</style>
