import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('both ownership editors use one permission-aware selector and explicit null clearing', () => {
  for (const view of ['RuleList', 'PlanList']) {
    const source = readView(view)
    assert.match(source, /<DomainOwnershipSelect/)
    assert.match(source, /can\('standard.domain.read'\)/)
    assert.match(source, /owner_domain_id: form.owner_domain_id \?\? null/)
    assert.doesNotMatch(source, /loadDomains|onMounted/)
  }
  assert.match(readView('RuleList'), /owner_domain_id: rule.owner_domain_id \?\? null/)
})

const readView = (name) => readFileSync(
  new URL(`../src/views/${name}.vue`, import.meta.url),
  'utf8'
)


const planSource=readView('PlanList')
test('rule save feedback names the rule rather than the plan', () => {
  const source=readView('RuleList')
  assert.match(source, /t\(['"]quality\.rule\.saveSuccess['"]\)/)
  assert.doesNotMatch(source, /t\(['"]quality\.plan\.updateSuccess['"]\)/)
})
const executionDetailSource=readView('ExecutionDetail')
const executionFailureSource=readFileSync(new URL('../src/utils/executionFailure.js',import.meta.url),'utf8')
test('plans own bindings and pinned references; rule library owns Standard import',()=>{
for(const marker of ['ResourceTreePicker','systemCatalogAPI.describeFacts','ruleAPI.list','planAPI.run','tasks.value.some(isActive)','onBeforeUnmount','serializeCheckItems','form.version'])assert.ok(planSource.includes(marker),marker)
const ruleSource=readView('RuleList')
for(const marker of ['ruleAPI.listElementCandidates','form.source','serializeConstraint','form.version'])assert.ok(ruleSource.includes(marker),marker)
assert.doesNotMatch(planSource,/listElementCandidates|buildPlanDocument/)
assert.doesNotMatch(planSource,/catalogAPI|ruleApplicationAPI|checkTaskAPI/)
})
test('plan failures preserve form state and stale requests do not overwrite the current list',()=>{
assert.ok(planSource.includes('if (sequence !== listSequence) return'))
assert.ok(planSource.includes('tasks.value = []'))
assert.match(planSource, /if\s*\(submitting\.value\)\s*return/)
assert.ok(planSource.includes('quality.plan.saveFailed'))
})
test('execution detail shows a stable failure state and stops obsolete polling', () => {
  assert.match(executionDetailSource, /<el-result[\s\S]*?v-if="loadError"/)
  assert.match(executionDetailSource, /watch\(\(\) => route\.fullPath/)
  assert.match(executionDetailSource, /if \(requestSequence !== loadSequence\) return/)
  assert.match(executionDetailSource, /onBeforeUnmount\(\(\) => \{[\s\S]*?window\.clearTimeout\(pollTimer\)/)
  assert.match(executionDetailSource, /failureReason/)
  assert.match(executionDetailSource, /executionFailureLabel\(execution\.value, t\)/)
})

test('execution detail exposes the stable rule identity', () => {
  assert.match(executionDetailSource, /prop="rule_key"/)
  assert.match(executionDetailSource, /quality\.execution\.ruleKey/)
})

test('execution detail uses localized stable failure reasons', () => {
  assert.match(executionDetailSource, /executionFailureLabel\(execution\.value, t\)/)
  assert.match(executionFailureSource, /execution\.error_details\?\.code/)
  assert.match(executionFailureSource, /quality\.execution\.failureUnknown/)
  assert.doesNotMatch(executionDetailSource, /error_details\?\.message/)
})
