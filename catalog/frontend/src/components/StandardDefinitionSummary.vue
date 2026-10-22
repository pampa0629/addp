<template>
 <section data-testid="catalog-standard-definition" :data-state="state.status">
  <p v-if="state.status === 'ready'" class="definition">{{ state.text || t('catalog.standardDefinition.empty') }}</p>
  <p v-else>{{ t(`catalog.standardDefinition.${state.status}`) }}</p>
  <div v-if="state.revision" class="revision">
   <el-tag size="small">{{ t(`catalog.entries.domainStandard.revisionStatus.${state.revision.revision.status}`) }}</el-tag>
   <span>{{ t(state.revision.isCurrent ? 'catalog.standardDefinition.current' : 'catalog.standardDefinition.notCurrent') }}</span>
  </div>
  <el-button v-if="state.status === 'unavailable'" link @click="load">{{ t('catalog.common.retry') }}</el-button>
  <el-button v-if="canOpen" link type="primary" @click="openConsoleRoute(state.config.detailPath)">{{ t('catalog.standardDefinition.open') }}</el-button>
 </section>
</template>
<script setup>
import { reactive, watch, computed, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { allowsConsoleRoute, openConsoleRoute } from '@common-ui'
import { useAuthStore } from '../store/auth'
import client from '../api/client'
import { loadStandardDefinition } from '../utils/standardResourcePresentation'
const props = defineProps({ source: { type: Object, required: true } })
const auth = useAuthStore()
const { t } = useI18n()
const state = reactive({ status: 'loading', version: 0, text: '', revision: null, config: null })
const canOpen = computed(() => state.config && allowsConsoleRoute(state.config.detailPath, auth.contextType, auth.permissions))
const load = () => loadStandardDefinition(state, { source: props.source, hasPermission: permission => auth.hasPermission(permission), read: path => client.get(path) })
watch([() => props.source, () => auth.permissions.join(','), () => auth.authContext?.context?.tenant_id, () => auth.contextType], load, { immediate: true })
onBeforeUnmount(() => { state.version++ })
</script>
<style scoped>
p{margin:6px 0;color:var(--el-text-color-secondary);}.definition{white-space:pre-wrap;overflow-wrap:anywhere;color:var(--el-text-color-primary);}.revision{display:flex;align-items:center;gap:8px;flex-wrap:wrap;}
</style>
