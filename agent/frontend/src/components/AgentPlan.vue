<template>
  <section v-if="todos" class="agent-plan" :aria-label="t('agent.workspace.plan')">
    <strong>{{ t('agent.workspace.plan') }}</strong>
    <p>{{ t('agent.workspace.planNotice') }}</p>
    <ol>
      <li v-for="(item, index) in todos" :key="index"><el-tag size="small" :type="item.status === 'completed' ? 'info' : 'warning'">{{ t(`agent.workspace.planStatus.${item.status}`) }}</el-tag><span>{{ item.content }}</span></li>
    </ol>
  </section>
</template>
<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { validPlan } from '../utils/workspace'
const props = defineProps({ plan: { type: Array, required: true } })
const { t } = useI18n()
const todos = computed(() => validPlan(props.plan) ? props.plan : null)
</script>
<style scoped>
.agent-plan { padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); color: var(--addp-text-primary); }
.agent-plan p { margin: 8px 0; font-size: 12px; color: var(--addp-text-secondary); line-height: 1.6; }
.agent-plan ol { padding-left: 20px; margin-bottom: 0; }
.agent-plan li { margin-top: 8px; overflow-wrap: anywhere; }
.agent-plan li span { margin-left: 8px; }
</style>
