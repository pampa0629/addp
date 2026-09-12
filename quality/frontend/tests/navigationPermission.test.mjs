import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

const routerSource = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const layoutSource = readFileSync(new URL('../src/components/Layout.vue', import.meta.url), 'utf8')
const checkTaskSource = readFileSync(new URL('../src/views/CheckTaskList.vue', import.meta.url), 'utf8')
const gateTaskSource = readFileSync(new URL('../src/views/MaterializationGateTaskList.vue', import.meta.url), 'utf8')
const issueListSource = readFileSync(new URL('../src/views/IssueList.vue', import.meta.url), 'utf8')
const issueDetailSource = readFileSync(new URL('../src/views/IssueDetail.vue', import.meta.url), 'utf8')

test('Quality routes declare the matching human read permission', () => {
	for (const permission of [
		'quality.rule_application.read',
		'quality.check_task.read',
		'quality.materialization_gate.read',
		'monitor.execution.read',
		'quality.issue.read'
	]) {
		assert.ok(routerSource.includes(`requiredPermissions: ['${permission}']`))
	}
	assert.match(routerSource, /required\.some\(permission => !authStore\.hasPermission\(permission\)\)/)
})

test('standalone Quality navigation hides entries without their human read permission', () => {
	for (const permission of [
		'quality.rule_application.read',
		'quality.check_task.read',
		'quality.materialization_gate.read',
		'monitor.execution.read',
		'quality.issue.read'
	]) {
		assert.ok(layoutSource.includes(`v-if="can('${permission}')"`))
	}
})

test('task pages only link to execution detail for users allowed to read executions', () => {
	assert.match(checkTaskSource, /v-if="canViewExecutions" link type="primary"/)
	assert.match(gateTaskSource, /v-if="can\('monitor\.execution\.read'\)" link type="primary"/)
	assert.match(issueListSource, /v-if="canViewExecutions && issueExecutionRoute\(row\.execution_id\)"/)
	assert.match(issueDetailSource, /v-if="canViewExecutions && issueExecutionRoute\(issue\.execution_id\)"/)
})
