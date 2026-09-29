import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createMemoryHistory, createRouter } from 'vue-router'
import { allowsConsoleRoute, resolveModuleLandingRoute } from '../../../common-frontend/basic/src/authorization/consoleRouteAccess.js'

const routerSource = readFileSync(new URL('../src/router/index.js', import.meta.url), 'utf8')
const layoutSource = readFileSync(new URL('../src/components/Layout.vue', import.meta.url), 'utf8')
const planSource = readFileSync(new URL('../src/views/PlanList.vue', import.meta.url), 'utf8')
const issueListSource = readFileSync(new URL('../src/views/IssueList.vue', import.meta.url), 'utf8')
const issueDetailSource = readFileSync(new URL('../src/views/IssueDetail.vue', import.meta.url), 'utf8')

function createQualityRouter(permissions, contextType = 'tenant') {
	const routeTable = routerSource.slice(routerSource.indexOf('const routes ='), routerSource.indexOf('const router ='))
	const authStore = { contextType, permissions }
	const routes = new Function('Layout', 'Login', 'useAuthStore', 'resolveModuleLandingRoute', `${routeTable}; return routes`)(
		{}, {}, () => authStore, resolveModuleLandingRoute
	)
	for (const child of routes[1].children) {
		if (child.component) child.component = {}
	}
	routes.push({ path: '/forbidden', component: {} })
	return createRouter({ history: createMemoryHistory(), routes })
}

test('Quality standalone root selects the first accessible Console page', async () => {
	for (const [permissions, contextType, expected] of [
		[['quality.plan.read', 'quality.issue.read', 'monitor.execution.read'], 'tenant', '/overview'],
		[['quality.rule.read'], 'tenant', '/rules'],
		[['quality.plan.read'], 'tenant', '/plans'],
		[['quality.issue.read'], 'tenant', '/issues'],
		[['monitor.execution.read'], 'tenant', '/forbidden'],
		[[], 'tenant', '/forbidden'],
		[['quality.rule.read'], 'platform', '/forbidden']
	]) {
		const router = createQualityRouter(permissions, contextType)
		await router.push('/')
		assert.equal(router.currentRoute.value.path, expected)
	}

	const direct = createQualityRouter(['quality.rule.read'])
	await direct.push('/rules?revision=3')
	assert.equal(direct.currentRoute.value.fullPath, '/rules?revision=3')
	assert.match(routerSource, /router\.beforeEach\(createAuthGuard\(/)
	assert.doesNotMatch(routerSource, /requiredPermissions|routes\[1\]\.children\.find/)
})

test('standalone Quality navigation hides entries without their human read permission', () => {
	assert.match(layoutSource, /allowsConsoleRoute\(`\/quality\$\{path\}`/)
	for (const [path, permission] of [
		['/rules', 'quality.rule.read'],
		['/plans', 'quality.plan.read'],
		['/issues', 'quality.issue.read']
	]) {
		assert.ok(layoutSource.includes(`v-if="canEnter('${path}')"`))
		assert.equal(allowsConsoleRoute(`/quality${path}`, 'tenant', []), false)
		assert.equal(allowsConsoleRoute(`/quality${path}`, 'tenant', [permission]), true)
	}
	assert.equal(allowsConsoleRoute('/quality/overview', 'tenant', ['quality.plan.read']), false)
	assert.doesNotMatch(layoutSource, /index="\/executions"/)
})

test('task pages only link to execution detail for users allowed to read executions', () => {
	assert.match(planSource, /v-if="can\('monitor\.execution\.read'\)"\s+link\s+type="primary"/)
	assert.match(issueListSource, /v-if="canViewExecutions && issueExecutionRoute\(row\.execution_id\)"/)
	assert.match(issueDetailSource, /v-if="canViewExecutions && issueExecutionRoute\(issue\.execution_id\)"/)
})
