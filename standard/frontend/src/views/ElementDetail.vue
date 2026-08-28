<template>
  <div class="page-shell" v-loading="loading">
    <div class="page-header">
      <div class="header-left"><el-button :icon="ArrowLeft" @click="goBack">{{ $t('standard.common.back') }}</el-button><h2>{{ revision.name || element.code || $t('standard.element.detailTitle') }}</h2><el-tag v-if="revision.status" :type="statusType(revision.status)">R{{ revision.revision_no }} · {{ statusLabel(revision.status) }}</el-tag></div>
      <div class="actions">
        <el-button v-if="editable" type="primary" :loading="saving" @click="save">{{ $t('standard.common.save') }}</el-button>
        <el-button v-if="editable" type="warning" @click="act('submit')">{{ $t('standard.revision.submit') }}</el-button>
        <el-button v-if="reviewing && canPublish" @click="act('return')">{{ $t('standard.revision.return') }}</el-button>
        <el-button v-if="reviewing && canPublish" type="success" @click="act('publish')">{{ $t('standard.revision.publish') }}</el-button>
        <el-button v-if="!element.draft_revision && element.current_revision && canUpdate" @click="newDraft">{{ $t('standard.revision.newDraft') }}</el-button>
        <el-button v-if="revision.status === 'published' && canPublish" type="danger" @click="act('withdraw')">{{ $t('standard.revision.withdraw') }}</el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <el-col :span="16">
        <el-card shadow="never" class="section"><template #header>{{ $t('standard.element.basicInfo') }}</template>
          <el-form :model="revision" label-width="130px" :disabled="!editable">
            <el-row :gutter="16">
              <el-col :span="12"><el-form-item :label="$t('standard.element.codeLabel')"><el-input :model-value="element.code" disabled /></el-form-item></el-col>
              <el-col :span="12"><el-form-item :label="$t('standard.element.nameLabel')"><el-input v-model="revision.name" /></el-form-item></el-col>
              <el-col :span="12"><el-form-item :label="$t('standard.glossary.domainLabel')"><el-select v-model="element.domain_id" clearable style="width:100%"><el-option v-for="d in domains" :key="d.id" :label="d.name" :value="d.id" /></el-select></el-form-item></el-col>
              <el-col :span="12"><el-form-item :label="$t('standard.element.dataTypeLabel')"><el-select v-model="revision.data_type" style="width:100%"><el-option v-for="type in dataTypes" :key="type" :label="type" :value="type" /></el-select></el-form-item></el-col>
              <el-col :span="12"><el-form-item :label="$t('standard.element.lengthLabel')"><el-input-number v-model="revision.length" :min="1" style="width:100%" /></el-form-item></el-col>
              <el-col :span="12"><el-form-item :label="$t('standard.element.nullableLabel')"><el-switch v-model="revision.nullable" /></el-form-item></el-col>
              <el-col :span="12"><el-form-item :label="$t('standard.element.unitLabel')"><el-select v-model="revision.unit_id" clearable filterable style="width:100%"><el-option v-for="u in units" :key="u.id" :label="`${u.name} (${u.symbol})`" :value="u.id" /></el-select></el-form-item></el-col>
              <el-col :span="12"><el-form-item :label="$t('standard.element.classificationLabel')"><el-tree-select v-model="revision.classification_id" :data="classificationTree" :props="{ label:'name', value:'id', children:'children' }" clearable style="width:100%" /></el-form-item></el-col>
            </el-row>
            <el-form-item :label="$t('standard.element.definitionLabel')"><el-input v-model="revision.definition" type="textarea" :rows="3" /></el-form-item>
            <el-form-item :label="$t('standard.element.formatLabel')"><el-input v-model="revision.format" /></el-form-item>
            <el-form-item :label="$t('standard.element.defaultValueLabel')"><el-input v-model="revision.default_value" /></el-form-item>
            <el-form-item :label="$t('standard.element.exampleValuesLabel')"><el-select v-model="revision.example_values" multiple filterable allow-create style="width:100%" /></el-form-item>
            <el-form-item :label="$t('standard.revision.changeSummary')"><el-input v-model="revision.change_summary" type="textarea" :rows="2" /></el-form-item>
          </el-form>
        </el-card>

        <el-card shadow="never" class="section"><template #header>{{ $t('standard.element.valueDomain') }}</template>
          <el-form :model="revision" label-width="130px" :disabled="!editable">
            <el-form-item :label="$t('standard.element.valueDomainKind')"><el-radio-group v-model="revision.value_domain_kind" @change="resetValueDomain"><el-radio-button value="unrestricted">{{ $t('standard.element.unrestricted') }}</el-radio-button><el-radio-button value="range">{{ $t('standard.element.range') }}</el-radio-button><el-radio-button value="enumeration">{{ $t('standard.element.enumeration') }}</el-radio-button></el-radio-group></el-form-item>
            <template v-if="revision.value_domain_kind === 'range'">
              <el-row :gutter="16"><el-col :span="12"><el-form-item :label="$t('standard.element.rangeMin')"><el-input-number v-model="revision.range_constraint.min" style="width:100%" /></el-form-item></el-col><el-col :span="12"><el-form-item :label="$t('standard.element.rangeMax')"><el-input-number v-model="revision.range_constraint.max" style="width:100%" /></el-form-item></el-col></el-row>
              <el-row :gutter="16"><el-col :span="12"><el-form-item :label="$t('standard.element.minInclusive')"><el-switch v-model="revision.range_constraint.min_inclusive" /></el-form-item></el-col><el-col :span="12"><el-form-item :label="$t('standard.element.maxInclusive')"><el-switch v-model="revision.range_constraint.max_inclusive" /></el-form-item></el-col></el-row>
            </template>
            <el-form-item v-if="revision.value_domain_kind === 'enumeration'" :label="$t('standard.element.codeSetLabel')"><el-select v-model="revision.code_set_revision_id" filterable style="width:100%"><el-option v-for="cs in publishedCodeSets" :key="cs.current_revision.id" :label="`${cs.current_revision.name} (${cs.code}) · R${cs.current_revision.revision_no}`" :value="cs.current_revision.id" /></el-select></el-form-item>
          </el-form>
        </el-card>

        <el-card shadow="never" class="section"><template #header>{{ $t('standard.element.qualityRules') }}</template>
          <el-alert :title="$t('standard.element.compiledRuleHint')" type="info" :closable="false" />
          <el-checkbox v-model="uniqueEnabled" :disabled="!editable" class="unique-rule">{{ $t('standard.element.ruleUnique') }}</el-checkbox>
        </el-card>
        <DocumentPanel v-if="element.id" entity-type="element" :entity-id="element.id" v-model:entity-version="element.version" />
      </el-col>

      <el-col :span="8">
        <el-card shadow="never" class="section"><template #header>{{ $t('standard.revision.history') }}</template>
          <el-timeline><el-timeline-item v-for="item in revisions" :key="item.id" :timestamp="formatTime(item.created_at)"><div class="history-row"><el-link @click="selectRevision(item)">R{{ item.revision_no }} · {{ item.name }}</el-link><el-tag size="small" :type="statusType(item.status)">{{ statusLabel(item.status) }}</el-tag></div><small>{{ item.change_summary }}</small></el-timeline-item></el-timeline>
        </el-card>
        <el-card shadow="never" class="section"><template #header>{{ $t('standard.common.metadata') }}</template><el-descriptions :column="1"><el-descriptions-item :label="$t('standard.common.id')">{{ element.id }}</el-descriptions-item><el-descriptions-item :label="$t('standard.element.codeLabel')">{{ element.code }}</el-descriptions-item><el-descriptions-item :label="$t('standard.common.createdAt')">{{ formatTime(element.created_at) }}</el-descriptions-item></el-descriptions></el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { useConsolePageDescriptor } from '@common-ui'
import { classificationAPI, codeSetAPI, domainAPI, elementAPI, unitAPI } from '../api/standard'
import DocumentPanel from '../components/DocumentPanel.vue'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { formatStandardDateTime } from '../utils/dateTime'

const route = useRoute(), router = useRouter()
const { t, locale } = useI18n()
const { canUpdate, canPublish } = useStandardPermissions('element')
const loading = ref(false), saving = ref(false), element = ref({}), revisions = ref([]), domains = ref([]), units = ref([]), classifications = ref([]), publishedCodeSets = ref([])
const revision = reactive({}), uniqueEnabled = ref(false), uniqueRuleKey = ref('')
const dataTypes = ['string', 'text', 'int', 'bigint', 'float', 'decimal', 'date', 'datetime', 'bool', 'json']
const editable = computed(() => canUpdate.value && revision.status === 'draft' && element.value.draft_revision_id === revision.id)
const reviewing = computed(() => revision.status === 'in_review' && element.value.draft_revision_id === revision.id)
const classificationTree = computed(() => tree(classifications.value))
useConsolePageDescriptor(router, 'standard', { title: computed(() => t('standard.element.recentVisitTitle')), subject: computed(() => revision.name || element.value.code || ''), ready: computed(() => Boolean(element.value.id)) })
function tree(list, parent = null) { return list.filter(x => (x.parent_id || null) === parent).map(x => ({ ...x, children: tree(list, x.id) })) }
const flatten = nodes => nodes.flatMap(node => [node, ...flatten(node.children || [])])
const statusLabel = s => s ? t(`standard.revision.status.${s}`) : '-'
const statusType = s => ({ draft:'info', in_review:'warning', published:'success', superseded:'', withdrawn:'danger' }[s] || 'info')
const formatTime = value => formatStandardDateTime(value, locale.value)
const extraRules = enabled => { if (enabled && !uniqueRuleKey.value) uniqueRuleKey.value = crypto.randomUUID(); return { schema_version:'addp.quality.rules/v1', rules: enabled ? [{ rule_key:uniqueRuleKey.value, type:'unique', enabled:true, severity:'error', message:'', params:{} }] : [] } }
const setRevision = value => { Object.keys(revision).forEach(k => delete revision[k]); Object.assign(revision, structuredClone(value || {})); revision.example_values ||= []; revision.value_domain_kind ||= 'unrestricted'; if (revision.value_domain_kind === 'range') revision.range_constraint ||= { min:null, max:null, min_inclusive:true, max_inclusive:true }; const uniqueRule = revision.extra_quality_rules?.rules?.find(r => r.type === 'unique'); uniqueRuleKey.value = uniqueRule?.rule_key || ''; uniqueEnabled.value = Boolean(uniqueRule?.enabled) }
const load = async () => { loading.value = true; try { const [aggregate, history] = await Promise.all([elementAPI.get(route.params.id), elementAPI.listRevisions(route.params.id)]); element.value = aggregate; revisions.value = history || []; setRevision(aggregate.draft_revision || aggregate.current_revision || history?.[0]) } catch (e) { ElMessage.error(getStandardErrorMessage(e,t,'standard.common.loadFailed')); goBack() } finally { loading.value = false } }
const loadOptions = async () => { const [d,u,c,cs] = await Promise.allSettled([domainAPI.list(), unitAPI.list({page_size:500}), classificationAPI.list(), codeSetAPI.list({status:'published',page_size:500})]); domains.value = d.status === 'fulfilled' ? flatten(d.value || []) : []; units.value = u.status === 'fulfilled' ? u.value || [] : []; classifications.value = c.status === 'fulfilled' ? c.value || [] : []; publishedCodeSets.value = cs.status === 'fulfilled' ? (cs.value.data || []).filter(x => x.current_revision) : [] }
const revisionPayload = version => ({ version, name:revision.name, definition:revision.definition, data_type:revision.data_type, length:revision.length || null, precision_num:revision.precision_num || null, scale:revision.scale ?? null, nullable:Boolean(revision.nullable), default_value:revision.default_value || '', format:revision.format || '', value_domain_kind:revision.value_domain_kind, range_constraint:revision.value_domain_kind === 'range' ? revision.range_constraint : null, code_set_revision_id:revision.value_domain_kind === 'enumeration' ? revision.code_set_revision_id : null, unit_id:revision.unit_id || null, security_level:revision.security_level || '', classification_id:revision.classification_id || null, example_values:revision.example_values || [], extra_quality_rules:extraRules(uniqueEnabled.value), change_summary:revision.change_summary, effective_from:revision.effective_from || null, effective_to:revision.effective_to || null })
const save = async () => { saving.value = true; try { const identity = await elementAPI.update(element.value.id, { version:element.value.version, domain_id:element.value.domain_id || null, steward_id:element.value.steward_id || null, tags:element.value.tags || [] }); const aggregate = await elementAPI.updateRevision(element.value.id, revision.id, revisionPayload(identity.version)); element.value = aggregate; setRevision(aggregate.draft_revision); ElMessage.success(t('standard.common.saveSuccess')); await load() } catch (e) { ElMessage.error(getStandardErrorMessage(e,t,'standard.common.saveFailed')) } finally { saving.value = false } }
const resetValueDomain = kind => { revision.range_constraint = kind === 'range' ? { min:null, max:null, min_inclusive:true, max_inclusive:true } : null; revision.code_set_revision_id = null }
const act = async action => { try { await ElMessageBox.confirm(t(`standard.revision.confirm.${action}`), t('standard.common.hint')); const method = { submit:'submitRevision', return:'returnRevision', publish:'publishRevision', withdraw:'withdrawRevision' }[action]; element.value = await elementAPI[method](element.value.id, revision.id, element.value.version); ElMessage.success(t('standard.common.updateSuccess')); await load() } catch (e) { if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e,t)) } }
const newDraft = async () => { try { const { value } = await ElMessageBox.prompt(t('standard.revision.changeSummary'), t('standard.revision.newDraft'), { inputPattern:/\S+/, inputErrorMessage:t('standard.revision.changeSummaryRequired') }); element.value = await elementAPI.createRevision(element.value.id, { version:element.value.version, change_summary:value.trim() }); await load() } catch (e) { if (!isCanceledInteraction(e)) ElMessage.error(getStandardErrorMessage(e,t)) } }
const selectRevision = item => setRevision(item)
const goBack = () => navigateStandardRoute(router, '/elements', { history:'replace' })
watch(() => route.params.id, () => { load(); loadOptions() }, { immediate:true })
</script>

<style scoped>
.page-shell{min-height:100%;padding:20px;background:var(--addp-bg-secondary);color:var(--addp-text-primary)}.page-header,.header-left,.actions,.history-row{display:flex;align-items:center}.page-header{justify-content:space-between;gap:16px;margin-bottom:16px}.header-left,.actions{gap:10px;flex-wrap:wrap}.section{margin-bottom:16px}.unique-rule{margin-top:16px}.history-row{justify-content:space-between;gap:8px}.page-shell :deep(.el-card){background:var(--addp-bg-primary);border-color:var(--addp-border-color)}h2{margin:0;font-size:20px}
</style>
