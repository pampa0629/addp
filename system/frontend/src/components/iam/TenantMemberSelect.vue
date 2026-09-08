<template>
  <div class="iam-member-select">
    <el-select
      v-if="showTypeFilter"
      v-model="typeModel"
      class="iam-member-select__type"
      :placeholder="typePlaceholder || t('system.iam.memberSelect.type')"
      clearable
      @change="handleTypeChange"
    >
      <el-option v-for="type in principalTypes" :key="type" :label="t(`system.iam.principalType.${type}`)" :value="type" />
    </el-select>
    <el-select
      v-model="memberModel"
      class="iam-member-select__member"
      :placeholder="placeholder || t('system.iam.roleAssignments.allMembers')"
      :disabled="disabled"
      :loading="loading"
      clearable
      filterable
      @change="handleMemberChange"
    >
      <el-option-group v-for="group in groups" :key="group.key" :label="t(`system.iam.memberSelect.groups.${group.key}`)">
        <el-option v-for="member in group.members" :key="member.id" :label="memberOptionLabel(member)" :value="memberValue(member)">
          <TenantMemberIdentity :member="member" :current-membership-id="currentMembershipId" :show-status="!activeOnly" />
        </el-option>
      </el-option-group>
    </el-select>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatMemberOptionLabel, groupTenantMembers } from '../../utils/iamPresentation'
import TenantMemberIdentity from './TenantMemberIdentity.vue'

const props = defineProps({
  modelValue: { type: [String, Number], default: '' },
  principalType: { type: String, default: '' },
  members: { type: Array, default: () => [] },
  currentMembershipId: { type: [String, Number], default: '' },
  valueField: { type: String, default: 'id', validator: (value) => ['id', 'principal_id'].includes(value) },
  stringifyValue: { type: Boolean, default: false },
  activeOnly: { type: Boolean, default: false },
  showTypeFilter: { type: Boolean, default: false },
  placeholder: { type: String, default: '' },
  typePlaceholder: { type: String, default: '' },
  disabled: { type: Boolean, default: false },
  loading: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue', 'update:principalType', 'change', 'principal-type-change'])
const { t } = useI18n()
const principalTypes = ['user', 'service_principal']
const groups = computed(() => groupTenantMembers(props.members, props.currentMembershipId, {
  activeOnly: props.activeOnly,
  principalType: props.principalType
}))
const memberModel = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value ?? '')
})
const typeModel = computed({
  get: () => props.principalType,
  set: (value) => emit('update:principalType', value || '')
})

function memberValue(member) {
  const value = member?.[props.valueField]
  return props.stringifyValue && value != null ? String(value) : value
}
function memberOptionLabel(member) {
  const type = member?.principal_type === 'service_principal' ? 'service_principal' : 'user'
  return formatMemberOptionLabel(member, props.currentMembershipId, t('system.iam.memberships.currentAccount'), t(`system.iam.principalType.${type}`))
}
function handleMemberChange(value) {
  emit('change', value ?? '')
}
function handleTypeChange(value) {
  const selected = props.members.find((member) => String(memberValue(member)) === String(props.modelValue))
  if (selected && value && selected.principal_type !== value) {
    emit('update:modelValue', '')
  }
  emit('principal-type-change', value || '')
}
</script>

<style scoped>
.iam-member-select { display: flex; flex: 1 1 440px; gap: 8px; min-width: 280px; }
.iam-member-select__type { flex: 0 0 150px; }
.iam-member-select__member { flex: 1 1 260px; min-width: 220px; }
@media (max-width: 620px) {
  .iam-member-select { flex-basis: 100%; flex-direction: column; min-width: 0; }
  .iam-member-select__type, .iam-member-select__member { width: 100%; flex-basis: auto; }
}
</style>
