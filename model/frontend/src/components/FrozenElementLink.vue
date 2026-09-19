<template>
  <el-button
    v-if="binding.element_revision_id"
    link type="primary" size="small" class="revision-tag"
    :disabled="!binding.element_id || !auth.hasPermission('standard.element.read')"
    @click="openRevision"
  >{{ label }} #{{ binding.element_revision_id }}</el-button>
</template>

<script setup>
import { buildStandardElementRevisionRoute, openConsoleRoute } from '@common-ui'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { getModelErrorMessage } from '../utils/apiError'

const props = defineProps({ binding: { type: Object, required: true }, label: { type: String, required: true } })
const auth = useAuthStore()
const { t } = useI18n()
async function openRevision() {
  if (!auth.hasPermission('standard.element.read')) return
  try {
    await openConsoleRoute(buildStandardElementRevisionRoute(props.binding.element_id, props.binding.element_revision_id))
  } catch (error) {
    ElMessage.error(getModelErrorMessage(error, t))
  }
}
</script>
