<template>
  <div class="iam-member-identity">
    <div class="iam-member-identity__heading">
      <el-icon class="iam-member-identity__icon"><User v-if="principalType === 'user'" /><Connection v-else-if="principalType === 'service_principal'" /><Operation v-else /></el-icon>
      <strong>{{ displayName }}</strong>
      <el-tag v-if="principalType" size="small" :type="principalType === 'user' ? 'info' : 'warning'" effect="plain">{{ principalTypeLabel }}</el-tag>
      <el-tag v-if="isCurrent" size="small" type="primary" effect="plain">{{ t('system.iam.memberships.currentAccount') }}</el-tag>
      <el-tag v-if="showStatus && member.status && member.status !== 'active'" size="small" :type="statusTagType">{{ statusLabel }}</el-tag>
    </div>
    <span v-if="identifier !== displayName" class="iam-member-identity__identifier">{{ identifier }}</span>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { Connection, Operation, User } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { isCurrentTenantMembership, tenantMemberDisplayName, tenantMemberIdentifier } from '../../utils/iamPresentation'

const props = defineProps({
  member: { type: Object, required: true },
  currentMembershipId: { type: [String, Number], default: '' },
  showStatus: { type: Boolean, default: false }
})
const { t } = useI18n()
const principalType = computed(() => ['user', 'service_principal'].includes(props.member?.principal_type) ? props.member.principal_type : '')
const displayName = computed(() => tenantMemberDisplayName(props.member))
const identifier = computed(() => String(tenantMemberIdentifier(props.member)))
const isCurrent = computed(() => isCurrentTenantMembership(props.member, props.currentMembershipId))
const principalTypeLabel = computed(() => principalType.value ? t(`system.iam.principalType.${principalType.value}`) : '')
const statusLabel = computed(() => t(`system.iam.status.${props.member?.status || 'active'}`))
const statusTagType = computed(() => props.member?.status === 'suspended' ? 'warning' : 'info')
</script>

<style scoped>
.iam-member-identity { display: flex; min-width: 0; flex-direction: column; gap: 3px; line-height: 1.35; }
.iam-member-identity__heading { display: flex; align-items: center; gap: 6px; min-width: 0; }
.iam-member-identity__heading strong { overflow: hidden; min-width: 0; color: var(--addp-text-primary); text-overflow: ellipsis; white-space: nowrap; }
.iam-member-identity__icon { flex: 0 0 auto; color: var(--addp-text-secondary); }
.iam-member-identity__identifier { overflow: hidden; padding-left: 22px; color: var(--addp-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
</style>
