import assert from 'node:assert/strict'
import test from 'node:test'
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, existsSync, rmSync } from 'node:fs'
import { resolve } from 'node:path'
import { tmpdir } from 'node:os'
import { spawn } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { createServer, createConnection } from 'node:net'
import { browserTestOrigin, browserTestFixtures, withBrowserTestIsolation } from '../src/utils/browserTestIsolation.mjs'
import { runBrowserGate } from '../../../scripts/test/frontend-browser-gate.mjs'

const launcher = fileURLToPath(new URL('../../../scripts/test/frontend-browser-gate.mjs', import.meta.url))
const waitFor = async predicate => {
  const deadline = Date.now() + 15_000
  while (!predicate()) {
    if (Date.now() >= deadline) throw new Error('Browser fixture did not reach its expected state')
    await new Promise(done => setTimeout(done, 25))
  }
}

function fakeFrontend() {
  const repository = mkdtempSync(resolve(tmpdir(), 'addp-browser-gate-test-'))
  const frontend = resolve(repository, 'sample/frontend')
  const dependency = resolve(frontend, 'node_modules/@playwright/test')
  mkdirSync(dependency, { recursive: true })
  writeFileSync(resolve(frontend, 'package.json'), JSON.stringify({ addpBrowserTest: {
    fixtures: [{ module: 'sample', readyPath: '/login', base: '/' }]
  } }))
  writeFileSync(resolve(dependency, 'package.json'), JSON.stringify({ exports: { './cli': './cli.cjs' } }))
  writeFileSync(resolve(dependency, 'cli.cjs'), `
    const { fork } = require('node:child_process')
    const { writeFileSync, mkdirSync, rmSync } = require('node:fs')
    const { resolve } = require('node:path')
    const run = JSON.parse(process.env.ADDP_BROWSER_RUN)
    if (process.argv.includes('--serve')) {
      process.title = 'addp-browser-' + require('node:path').basename(run.runtimeDir) + '-' + process.pid
      const server = require('node:net').createServer()
      server.listen(run.ports.sample, '127.0.0.1', () => {
        writeFileSync(resolve(run.runtimeDir, process.pid + '.pid'), 'sample')
        const foreign = process.argv.find(argument => argument.startsWith('--foreign-pid='))
        if (foreign) writeFileSync(resolve(run.runtimeDir, foreign.split('=')[1] + '.pid'), 'stale unrelated PID')
        writeFileSync(resolve(run.runtimeDir, 'cache'), 'owned cache')
        process.send({ pid: process.pid })
      })
      process.on('SIGTERM', () => {
        if (!process.argv.includes('--ignore-stop')) server.close(() => process.exit(0))
      })
    } else {
      // Playwright clears outputDir before a real test run.
      rmSync(run.outputDir, { recursive: true, force: true })
      mkdirSync(run.outputDir)
      const child = fork(__filename, ['--serve', ...process.argv.slice(2)], { stdio: ['ignore', 'ignore', 'inherit', 'ipc'] })
      child.once('message', ({pid}) => {
        writeFileSync('scope.json', JSON.stringify({ ...run, fixturePID: pid }))
        if (!process.argv.includes('--wait')) process.exit(process.argv.includes('--fail') ? 7 : 0)
      })
      process.on('SIGINT', () => process.exit(0))
    }
  `)
  return { repository, frontend }
}

function launch(frontend, args = [], temporaryDirectory = tmpdir()) {
  const child = spawn(process.execPath, [launcher, ...args], {
    cwd: frontend, stdio: ['ignore', 'pipe', 'pipe'], env: { ...process.env, TMPDIR: temporaryDirectory }
  })
  let output = ''
  child.stdout.on('data', chunk => { output += chunk })
  child.stderr.on('data', chunk => { output += chunk })
  const completion = new Promise((done, fail) => { child.once('error', fail); child.once('exit', code => done({ code, output })) })
  return { child, completion }
}

async function assertClean(run) {
  assert.equal(existsSync(run.runtimeDir), false)
  for (const port of Object.values(run.ports)) {
    assert.equal(existsSync(resolve('/tmp', `addp-browser-port-${port}.lease`)), false)
    const server = createServer()
    await new Promise((done, fail) => { server.once('error', fail); server.listen(port, '127.0.0.1', done) })
    await new Promise(done => server.close(done))
  }
}

test('shared browser configuration owns every address, server and result directory', () => {
  const fixture = fakeFrontend()
  const previous = process.env.ADDP_BROWSER_RUN
  try {
    delete process.env.ADDP_BROWSER_RUN
    assert.throws(() => browserTestOrigin('sample'), /npm run test:e2e/)
    process.env.ADDP_BROWSER_RUN = JSON.stringify({ owner: 'sample', repository: fixture.repository, ports: { sample: 34567 }, outputDir: '/tmp/owned-results' })
    const config = withBrowserTestIsolation('sample', { use: { headless: true } })
    assert.equal(config.use.baseURL, 'http://127.0.0.1:34567')
    assert.equal(config.outputDir, '/tmp/owned-results')
    const server = config.webServer[0]
    assert.equal(server.url, 'http://127.0.0.1:34567/login')
    assert.equal(server.command, 'npm run dev -- --host 127.0.0.1 --port 34567 --strictPort --base /')
    assert.equal(server.env.ADDP_E2E, '1')
    assert.equal(server.env.VITE_ADDP_CONSOLE_PORT, '34567')
    assert.equal(server.reuseExistingServer, false)
    assert.deepEqual(server.gracefulShutdown, { signal: 'SIGTERM', timeout: 5000 })
    assert.throws(() => withBrowserTestIsolation('other', {}), /owner/)
    for (const config of [{ outputDir: '/tmp/shared' }, { webServer: {} }, { use: { baseURL: 'http://other' } }, { projects: [] }]) {
      assert.throws(() => withBrowserTestIsolation('sample', config), /owned by shared isolation/)
    }
    assert.throws(() => browserTestOrigin('missing'), /Missing browser fixture/)
    writeFileSync(resolve(fixture.frontend, 'package.json'), JSON.stringify({ addpBrowserTest: { fixtures: [{ module: '../escape', readyPath: '/' }] } }))
    assert.throws(() => browserTestFixtures(fixture.frontend), /Invalid/)
  } finally {
    if (previous === undefined) delete process.env.ADDP_BROWSER_RUN
    else process.env.ADDP_BROWSER_RUN = previous
    rmSync(fixture.repository, { recursive: true, force: true })
  }
})

test('the deterministic launcher rejects Online configurations and shared output overrides', async () => {
  const fixture = fakeFrontend()
  try {
    await assert.rejects(runBrowserGate(fixture.frontend, ['--config=playwright.online.config.js']), /dedicated Online/)
    await assert.rejects(runBrowserGate(fixture.frontend, ['-cplaywright.online.config.js']), /dedicated Online/)
    await assert.rejects(runBrowserGate(fixture.frontend, ['--output=/tmp/shared']), /owned by shared isolation/)
  } finally {
    rmSync(fixture.repository, { recursive: true, force: true })
  }
})

test('a stale PID record cannot stop an unrelated listener', async () => {
  const fixture = fakeFrontend()
  const foreign = spawn(process.execPath, ['-e', `
    process.title = 'addp-browser-unrelated-test-fixture'
    const server = require('node:net').createServer()
    server.listen(0, '127.0.0.1', () => process.stdout.write(JSON.stringify({pid: process.pid, port: server.address().port})))
    process.on('SIGTERM', () => server.close(() => process.exit(0)))
  `], { stdio: ['ignore', 'pipe', 'inherit'] })
  const foreignExit = new Promise(done => foreign.once('exit', done))
  let gate
  let run
  try {
    const listener = await new Promise((done, fail) => { foreign.once('error', fail); foreign.stdout.once('data', chunk => done(JSON.parse(chunk))) })
    gate = launch(fixture.frontend, [`--foreign-pid=${listener.pid}`])
    await waitFor(() => existsSync(resolve(fixture.frontend, 'scope.json')))
    run = JSON.parse(readFileSync(resolve(fixture.frontend, 'scope.json')))
    assert.equal((await gate.completion).code, 0)
    await assertClean(run)
    await new Promise((done, fail) => {
      const connection = createConnection({ host: '127.0.0.1', port: listener.port })
      connection.once('error', fail)
      connection.once('connect', () => { connection.destroy(); done() })
    })
  } finally {
    gate?.child.kill('SIGINT')
    if (gate) await gate.completion
    foreign.kill('SIGTERM')
    await foreignExit
    if (run) rmSync(run.outputDir, { recursive: true, force: true })
    rmSync(fixture.repository, { recursive: true, force: true })
  }
})

test('concurrent runs of the same owner isolate ports and evidence, and release owned resources', async () => {
  const fixtures = [fakeFrontend(), fakeFrontend()]
  const gates = fixtures.map(({ frontend, repository }) => launch(frontend, ['--wait'], repository))
  const runs = []
  try {
    for (const fixture of fixtures) await waitFor(() => existsSync(resolve(fixture.frontend, 'scope.json')))
    runs.push(...fixtures.map(fixture => JSON.parse(readFileSync(resolve(fixture.frontend, 'scope.json')))))
    assert.notEqual(runs[0].ports.sample, runs[1].ports.sample)
    assert.notEqual(runs[0].outputDir, runs[1].outputDir)
    writeFileSync(resolve(runs[0].outputDir, 'evidence'), 'first')
    assert.equal(existsSync(resolve(runs[1].outputDir, 'evidence')), false)
    for (const gate of gates) gate.child.kill('SIGINT')
    for (const gate of gates) assert.equal((await gate.completion).code, 130)
    for (const run of runs) { await assertClean(run); assert.equal(existsSync(run.outputDir), true) }
  } finally {
    for (const gate of gates) gate.child.kill('SIGINT')
    await Promise.all(gates.map(gate => gate.completion))
    for (const run of runs) rmSync(run.outputDir, { recursive: true, force: true })
    for (const fixture of fixtures) rmSync(fixture.repository, { recursive: true, force: true })
  }
})

for (const scenario of [{ args: [], code: 0 }, { args: ['--fail'], code: 7 }, { args: ['--wait'], signal: 'SIGTERM', code: 143 },
  { args: ['--wait', '--ignore-stop'], signal: 'SIGTERM', repeatSignal: true, code: 143 }]) {
  test(`browser ${scenario.signal || scenario.code} exit cleans a surviving fixture and preserves only failure evidence`, async () => {
    const fixture = fakeFrontend()
    const gate = launch(fixture.frontend, scenario.args)
    let run
    try {
      await waitFor(() => existsSync(resolve(fixture.frontend, 'scope.json')))
      run = JSON.parse(readFileSync(resolve(fixture.frontend, 'scope.json')))
      if (scenario.signal) gate.child.kill(scenario.signal)
      if (scenario.repeatSignal) setTimeout(() => gate.child.kill(scenario.signal), 100)
      const result = await gate.completion
      assert.equal(result.code, scenario.code, result.output)
      await assertClean(run)
      assert.equal(existsSync(run.outputDir), scenario.code !== 0)
      if (scenario.code !== 0) assert.equal(JSON.parse(readFileSync(resolve(run.outputDir, 'run.json'))).owner, 'sample')
    } finally {
      gate.child.kill('SIGINT')
      await gate.completion
      if (run) rmSync(run.outputDir, { recursive: true, force: true })
      rmSync(fixture.repository, { recursive: true, force: true })
    }
  })
}
