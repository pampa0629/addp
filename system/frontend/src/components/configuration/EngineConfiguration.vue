<template>
  <section data-testid="engine-configuration" :data-state="loading ? 'loading' : engines.length ? 'loaded' : 'unavailable'">
    <div class="engine-toolbar">
      <el-form label-position="top" class="engine-selector">
        <el-form-item :label="t('system.configuration.registeredEngine')">
          <el-select v-model="engineID" :placeholder="t('system.configuration.registeredEngine')"
            :disabled="loading || policy?.saving" data-testid="configuration-engine">
            <el-option v-for="engine in engines" :key="engine.id" :label="engine.name" :value="engine.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <el-button :loading="loading || policy?.loading" :disabled="policy?.saving" @click="reload">
        {{ t('system.rasterPolicy.refresh') }}
      </el-button>
    </div>
    <RasterPolicy v-if="engineID" ref="policy" :engine-id="engineID" />
    <el-empty v-else-if="!loading" :description="t('system.rasterPolicy.empty')" />
  </section>
</template>

<script setup>
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../../store/auth'
import { rasterPoliciesAPI } from '../../api/rasterPolicies'
import RasterPolicy from './RasterPolicy.vue'

const { t } = useI18n()
const auth = useAuthStore()
const engines = ref([]), engineID = ref(null), loading = ref(false), policy = ref(null)
let requestID = 0

async function reload() {
  const ticket = ++requestID, selectedID = engineID.value, context = auth.contextType
  engineID.value = null; engines.value = []; loading.value = true
  try {
    const values = await rasterPoliciesAPI.engines(context)
    if (ticket !== requestID) return
    engines.value = values
    engineID.value = values.some(engine => engine.id === selectedID) ? selectedID : values[0]?.id ?? null
  } catch {
    if (ticket === requestID) ElMessage.error(t('system.rasterPolicy.loadFailed'))
  } finally {
    if (ticket === requestID) loading.value = false
  }
}

watch(() => JSON.stringify([auth.authContext?.principal?.id, auth.authContext?.context]), () => {
  engineID.value = null
  reload()
})
onMounted(reload)
onBeforeUnmount(() => { requestID++ })
</script>

<style scoped>
.engine-toolbar { display: flex; align-items: flex-end; gap: 16px; margin-bottom: 20px; }
.engine-selector { flex: 1; min-width: 0; }
.engine-selector :deep(.el-form-item) { margin-bottom: 0; }
</style>
