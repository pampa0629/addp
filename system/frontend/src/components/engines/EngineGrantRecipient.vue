<template>
  <el-tooltip :content="identifier">
    <div class="grant-recipient" data-testid="grant-recipient">
      <TenantMemberIdentity v-if="identity && type === 'user'" :member="identity" show-status />
      <template v-else-if="identity">
        <strong>{{ identity.name }}</strong>
        <span>{{ typeLabel }}<template v-if="identity.code"> · {{ identity.code }}</template></span>
      </template>
      <template v-else>
        <span>{{ t(`system.engine.sourceGrants.recipientNames.${status}`) }}</span>
        <small>{{ identifier }}</small>
      </template>
    </div>
  </el-tooltip>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import TenantMemberIdentity from '../iam/TenantMemberIdentity.vue'

const props = defineProps({
  type: { type: String, required: true },
  id: { type: [String, Number], required: true },
  identity: { type: Object, default: null },
  status: { type: String, default: 'unavailable' }
})
const { t } = useI18n()
const typeLabel = computed(() => t(`system.engine.sourceGrants.types.${props.type}`))
const identifier = computed(() => t('system.engine.sourceGrants.recipientIdentifier', { type: typeLabel.value, id: props.id }))
</script>

<style scoped>
.grant-recipient { display: flex; flex-direction: column; gap: 3px; min-width: 0; overflow-wrap: anywhere; }
.grant-recipient > span, .grant-recipient > small { color: var(--addp-text-secondary); }
</style>
