<template>
  <component :is="compact ? 'details' : 'section'" v-if="todos" class="agent-plan" :class="{ compact }" role="region" :aria-label="t('agent.workspace.plan')">
    <summary v-if="compact"><strong>{{ t('agent.workspace.plan') }}</strong><span>{{ t('agent.workspace.planProgress', { completed: todos.filter(item => item.status === 'completed').length, total: todos.length }) }}</span><span v-if="activeItem">{{ activeItem.content }}</span></summary>
    <strong v-else>{{ t('agent.workspace.plan') }}</strong>
    <p>{{ t('agent.workspace.planNotice') }}</p>
    <ol>
      <li v-for="(item, index) in todos" :key="index"><el-tag size="small" :type="item.status === 'completed' ? 'info' : 'warning'">{{ t(`agent.workspace.planStatus.${item.status}`) }}</el-tag><span>{{ item.content }}</span></li>
    </ol>
  </component>
</template>
<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { validPlan } from '../utils/workspace'
const props = defineProps({ plan: { type: Array, required: true }, compact: Boolean })
const { t } = useI18n()
const activeItem = computed(() => props.plan.find(item => item?.status === 'in_progress'))
const todos = computed(() => validPlan(props.plan) ? props.plan : null)
</script>
<style scoped>
.agent-plan { padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); color: var(--addp-text-primary); }
.agent-plan summary { cursor: pointer; overflow-wrap: anywhere; }
.agent-plan summary span { margin-left: 8px; font-size: 12px; color: var(--addp-text-secondary); }
.agent-plan.compact { padding: 10px 12px; }
.agent-plan p { margin: 8px 0; font-size: 12px; color: var(--addp-text-secondary); line-height: 1.6; }
.agent-plan ol { padding-left: 20px; margin-bottom: 0; }
.agent-plan li { margin-top: 8px; overflow-wrap: anywhere; }
.agent-plan li span { margin-left: 8px; }
</style>
