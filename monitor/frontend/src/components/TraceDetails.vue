<template>
  <section class="trace-detail" aria-labelledby="trace-operation">
    <header class="trace-summary">
      <div>
        <h3 id="trace-operation">{{ operationTitle(summary.operation) }}</h3>
        <div class="trace-identification"><span>Trace ID</span> <code>{{ trace.trace_id }}</code></div>
      </div>
      <div class="trace-facts">
        <el-tag v-if="summary.entry || summary.operation.phase" :type="statusType(summary.entry || summary.operation)" effect="light">{{ status(summary.entry || summary.operation) }}</el-tag>
        <el-tag v-else type="info" effect="light">{{ t('monitor.traces.resultUnavailable') }}</el-tag>
        <span>{{ t('monitor.traces.totalTime', { time: number(trace.duration_ms) }) }}</span>
        <span>{{ t('monitor.traces.spanCount', { count: trace.spans.length }) }}</span>
        <el-tag v-if="trace.partial" type="warning" effect="light">{{ t('monitor.traces.partial') }}</el-tag>
        <span v-if="summary.failures.length">{{ t('monitor.traces.observedFailures', { count: summary.failures.length }) }}</span>
      </div>
      <div v-for="{ span, label, testId } in highlights" :key="span.span_id" class="trace-highlight"
           :data-testid="testId">
        <div class="highlight-facts">
          <span v-if="label" class="highlight-label">{{ t(`monitor.traces.${label}`) }}</span>
          <span v-if="span === summary.longestDownstream" class="highlight-label">{{ t('monitor.traces.longestDownstream') }}</span>
          <strong>{{ span.module_name }}</strong>
          <span>{{ number(span.duration_ms) }} ms</span>
          <el-tag v-if="span.status || span.outcome || span.failed" :type="statusType(span)" size="small" effect="light">{{ status(span) }}</el-tag>
          <span class="highlight-operation">{{ span.phase || span.route ? operationTitle(span) : t(`monitor.traces.kinds.${span.kind}`) }}</span>
        </div>
        <div class="span-actions">
          <el-button link type="primary" @click="focusSpan(span)">{{ t('monitor.traces.locateSpan') }}</el-button>
          <el-button v-if="canReadLogs" link type="primary" @click="emit('logs', span)">{{ t('monitor.traces.logs') }}</el-button>
        </div>
      </div>
    </header>

    <h4>{{ t('monitor.traces.timeline') }}</h4>
    <ol class="timeline" data-testid="trace-spans">
      <li v-for="{ span, depth, offsetPercent, widthPercent } in summary.rows" :id="'span-' + span.span_id" :key="span.span_id"
          class="timeline-row" :class="{ failed: span.failed }" tabindex="-1">
        <div class="span-operation" :style="{ paddingInlineStart: Math.min(depth, 4) * 12 + 'px' }">
          <strong>{{ span.module_name }}</strong>
          <span class="span-kind">{{ t(`monitor.traces.kinds.${span.kind}`) }}</span>
          <span v-if="span.phase || span.route" class="span-route">{{ operationTitle(span) }}</span>
        </div>
        <div class="span-timing">
          <div class="time-track" aria-hidden="true">
            <span :style="{ insetInlineStart: offsetPercent + '%', width: widthPercent + '%' }"></span>
          </div>
          <span>{{ number(span.duration_ms) }} ms</span>
        </div>
        <div class="span-actions">
          <el-tag v-if="span.status || span.outcome || span.failed" :type="statusType(span)" size="small" effect="light">{{ status(span) }}</el-tag>
          <el-button v-if="canReadLogs" link type="primary" @click="emit('logs', span)">{{ t('monitor.traces.logs') }}</el-button>
        </div>
        <details class="span-identifiers">
          <summary>{{ t('monitor.traces.identifiers') }}</summary>
          <dl>
            <div><dt>Span ID</dt><dd><code>{{ span.span_id }}</code></dd></div>
            <div v-if="span.parent_span_id"><dt>{{ t('monitor.traces.parentSpan') }}</dt><dd><code>{{ span.parent_span_id }}</code></dd></div>
            <div><dt>{{ t('monitor.http.instance') }}</dt><dd><code>{{ span.instance_id }}</code></dd></div>
            <div><dt>{{ t('monitor.traces.start') }}</dt><dd>{{ date(span.start) }}</dd></div>
            <div v-if="span.phase"><dt>{{ t('monitor.traces.attempt') }}</dt><dd>{{ span.attempt }}</dd></div>
          </dl>
        </details>
      </li>
    </ol>

    <section v-if="links.length">
      <h4>{{ t('monitor.traces.causalLinks') }}</h4>
      <ul class="relation-list" data-testid="trace-links">
        <li v-for="link in links" :key="link.source_span_id + link.trace_id + link.span_id">
          <strong>{{ link.module_name }}</strong>
          <time :datetime="link.started_at">{{ date(link.started_at) }}</time>
          <code>{{ link.trace_id }}</code>
          <el-button link type="primary" @click="emit('follow-link', link)">{{ t('monitor.traces.followLink') }}</el-button>
          <details><summary>{{ t('monitor.traces.identifiers') }}</summary>{{ t('monitor.traces.sourceSpan') }} <code>{{ link.source_span_id }}</code></details>
        </li>
      </ul>
    </section>
    <details class="trace-relationships">
      <summary>{{ t('monitor.traces.relationships') }} ({{ trace.edges.length }})</summary>
      <p>{{ t('monitor.traces.relationshipHint') }}</p>
      <ul class="relation-list" data-testid="trace-edges">
        <li v-for="edge in trace.edges" :key="edge.client_span_id + edge.server_span_id">
          {{ edge.from_module }} <code>{{ edge.from_instance }}</code> → {{ edge.to_module }} <code>{{ edge.to_instance }}</code>
        </li>
        <li v-if="!trace.edges.length">{{ t('monitor.traces.noEdges') }}</li>
      </ul>
    </details>
  </section>
</template>

<script setup>
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { traceTimeline } from '../utils/traces'

const props = defineProps({ trace: { type: Object, required: true }, matchedFailure: Object, canReadLogs: Boolean })
const emit = defineEmits(['logs', 'follow-link'])
const { t, locale } = useI18n()
const summary = computed(() => traceTimeline(props.trace, props.matchedFailure))
const highlights = computed(() => {
  const { firstFailure, downstreamFailure, longestDownstream } = summary.value
  const failedSpan = props.matchedFailure || firstFailure?.span
  const rows = []
  if (failedSpan) rows.push({ span: failedSpan, label: props.matchedFailure ? 'matchedHTTPFailure' : 'failureFocus', testId: 'trace-failure' })
  if (downstreamFailure) rows.push({ span: downstreamFailure, label: 'observedDownstreamFailure', testId: 'trace-downstream-failure' })
  if (longestDownstream && !rows.some(({ span }) => span === longestDownstream)) {
    rows.push({ span: longestDownstream, testId: 'trace-downstream' })
  }
  return rows
})
const links = computed(() => props.trace.spans.flatMap(span => span.links.map(link => ({ ...link, source_span_id: span.span_id }))))
const formatNumber = computed(() => new Intl.NumberFormat(locale.value, { maximumFractionDigits: 3 }))
const number = value => formatNumber.value.format(value)
const date = value => new Date(value).toLocaleString(locale.value)
function operationTitle(span) {
  if (span.phase) return t(`monitor.traces.phases.${span.phase}`)
  return [span.method, span.route].filter(Boolean).join(' ') || span.module_name
}
function status(span) {
  if (span.outcome) return t(`monitor.traces.outcomes.${span.outcome}`)
  if (span.status) return `HTTP ${span.status}`
  return t(span.failed ? 'monitor.traces.outcomes.failed' : 'monitor.traces.resultUnavailable')
}
function statusType(span) {
  if (span.failed) return 'danger'
  if (span.status >= 400) return 'warning'
  return (span.status > 0 || span.outcome === 'success') ? 'success' : 'info'
}
function focusSpan(span) {
  const row = document.getElementById('span-' + span.span_id)
  row?.scrollIntoView({ block: 'nearest' })
  row?.focus({ preventScroll: true })
}
</script>

<style scoped>
.trace-detail { margin: 16px 0; }
.trace-summary {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 12px;
  padding: 16px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-secondary);
}
.trace-summary h3 { margin: 0 0 6px; overflow-wrap: anywhere; color: var(--addp-text-primary); }
.trace-identification, .span-kind, .span-identifiers, .trace-relationships p { color: var(--addp-text-secondary); font-size: 13px; }
.trace-facts, .span-actions, .relation-list li, .trace-highlight, .highlight-facts { display: flex; flex-wrap: wrap; align-items: center; gap: 8px 12px; }
.trace-highlight { flex-basis: 100%; min-width: 0; justify-content: space-between; padding-top: 12px; border-top: 1px solid var(--addp-border-color); }
.highlight-facts { min-width: 0; }
.highlight-label, .highlight-operation { color: var(--addp-text-secondary); font-size: 13px; }
.highlight-operation { overflow-wrap: anywhere; }
.timeline, .relation-list { margin: 0; padding: 0; list-style: none; }
.timeline-row {
  display: grid;
  grid-template-columns: minmax(0, 1.5fr) minmax(110px, 1fr) 145px;
  gap: 6px 16px;
  align-items: center;
  padding: 10px 12px;
  border-inline-start: 3px solid transparent;
  border-bottom: 1px solid var(--addp-border-color);
}
.timeline-row.failed { border-inline-start-color: var(--el-color-danger); }
.timeline-row:focus { outline: 2px solid var(--el-color-primary); outline-offset: -2px; }
.span-operation, .span-route, .span-identifiers, code { overflow-wrap: anywhere; }
.span-kind { margin-inline-start: 8px; }
.span-route { display: block; }
.span-timing { display: grid; grid-template-columns: minmax(0, 1fr) 82px; gap: 8px; align-items: center; }
.span-timing > span { text-align: end; font-variant-numeric: tabular-nums; }
.time-track { position: relative; height: 6px; overflow: hidden; border-radius: 4px; background: var(--addp-bg-tertiary); }
.time-track > span { position: absolute; height: 100%; min-width: 2px; background: var(--el-color-primary); }
.failed .time-track > span { background: var(--el-color-danger); }
.span-identifiers { grid-column: 1 / -1; }
summary { cursor: pointer; }
dl { display: flex; flex-wrap: wrap; gap: 6px 16px; margin: 6px 0 0; }
dd { margin: 0; }
.relation-list li, .trace-relationships { margin-top: 8px; }
@media (max-width: 700px) {
  .timeline-row { grid-template-columns: minmax(0, 1fr) auto; }
  .span-timing { grid-column: 1 / -1; grid-row: 2; }
}
</style>
