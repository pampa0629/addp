#!/usr/bin/env node
import { createServer } from 'node:net'
import { mkdtempSync, mkdirSync, writeFileSync, readdirSync, rmSync, openSync, closeSync, existsSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { basename, dirname, resolve } from 'node:path'
import { createRequire } from 'node:module'
import { spawn, execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { browserTestFixtures } from '../../common-frontend/basic/src/utils/browserTestIsolation.mjs'

const delay = milliseconds => new Promise(done => setTimeout(done, milliseconds))
const alive = pid => { try { process.kill(pid, 0); return true } catch (error) { if (error.code === 'ESRCH') return false; throw error } }
const signalOwned = (pid, signal) => { try { process.kill(pid, signal) } catch (error) { if (error.code !== 'ESRCH') throw error } }

async function claimPort(runtimeDir) {
  for (let attempt = 0; attempt < 100; attempt++) {
    const server = createServer()
    await new Promise((done, fail) => { server.once('error', fail); server.listen(0, '127.0.0.1', done) })
    const port = server.address().port
    // Ports belong to the host, even when two gates use different TMPDIRs.
    const lease = resolve('/tmp', `addp-browser-port-${port}.lease`)
    try {
      const fd = openSync(lease, 'wx', 0o600)
      try { writeFileSync(fd, JSON.stringify({ pid: process.pid, runtimeDir })) }
      catch (error) { rmSync(lease, { force: true }); throw error }
      finally { closeSync(fd) }
      // The lease prevents another ADDP gate selecting this port between probe
      // and Vite bind. An unrelated bind still fails explicitly via strictPort.
      return { port, lease }
    } catch (error) {
      if (error.code !== 'EEXIST') throw error
    } finally {
      await new Promise(done => server.close(done))
    }
  }
  throw new Error('Unable to claim an isolated browser fixture port')
}

async function cleanupFixtures(runtimeDir) {
  const pids = readdirSync(runtimeDir).filter(name => /^[1-9]\d*\.pid$/.test(name)).map(name => Number(name.slice(0, -4))).filter(pid => {
    // A stale PID file must never authorize stopping a reused, unrelated PID.
    try {
      const title = execFileSync('ps', ['-ww', '-p', String(pid), '-o', 'args='], { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim()
      return title === `addp-browser-${basename(runtimeDir)}-${pid}`
    } catch { return false }
  })
  for (const pid of pids) signalOwned(pid, 'SIGTERM')
  const deadline = Date.now() + 6000
  while (pids.some(alive) && Date.now() < deadline) await delay(50)
  for (const pid of pids) if (alive(pid)) signalOwned(pid, 'SIGKILL')
  const killDeadline = Date.now() + 2000
  while (pids.some(alive) && Date.now() < killDeadline) await delay(25)
  if (pids.some(alive)) throw new Error('Owned browser fixture processes remain after cleanup')
}

export async function runBrowserGate(frontend, args) {
  frontend = resolve(frontend)
  for (let index = 0; index < args.length; index++) {
    const argument = args[index]
    if (/^--output(?:=|$)/.test(argument)) throw new Error('Browser output is owned by shared isolation')
    if (/^--config(?:=|$)|^-c/.test(argument)) {
      const config = argument.includes('=') ? argument.split('=')[1]
        : argument.startsWith('-c') && argument.length > 2 ? argument.slice(2) : args[++index]
      if (!['playwright.config.js', 'playwright.config.mjs'].includes(config) || !existsSync(resolve(frontend, config))) {
        throw new Error('Use the dedicated Online entry for Online browser configurations')
      }
    }
  }
  if (dirname(frontend) === frontend || frontend.split(/[\\/]/).at(-1) !== 'frontend') throw new Error('Run the browser gate from an owner frontend directory')
  const owner = dirname(frontend).split(/[\\/]/).at(-1)
  const fixtures = browserTestFixtures(frontend)
  if (!fixtures.some(fixture => fixture.module === owner)) throw new Error('Browser fixtures must include their owner')
  const repository = resolve(frontend, '../..')
  const runtimeDir = mkdtempSync(resolve(tmpdir(), `addp-${owner}-browser-runtime-`))
  const outputDir = mkdtempSync(resolve(tmpdir(), `addp-${owner}-playwright-results-`))
  const claims = []
  let result = 1
  let interrupted
  let child
  let run
  let escalation
  const interrupt = signal => {
    interrupted ||= signal
    child?.kill('SIGINT')
    escalation ||= setTimeout(() => child?.kill('SIGKILL'), 10_000)
  }
  const onInterrupt = () => interrupt('SIGINT')
  const onTerminate = () => interrupt('SIGTERM')
  process.on('SIGINT', onInterrupt)
  process.on('SIGTERM', onTerminate)
  try {
    const ports = {}
    for (const fixture of fixtures) {
      const claim = await claimPort(runtimeDir)
      claims.push(claim)
      ports[fixture.module] = claim.port
    }
    run = { owner, repository, runtimeDir, outputDir, ports }
    writeFileSync(resolve(outputDir, 'run.json'), JSON.stringify(run, null, 2))
    if (!interrupted) {
      const cli = createRequire(resolve(frontend, 'package.json')).resolve('@playwright/test/cli')
      console.log(`Browser gate ${owner}: ${JSON.stringify(ports)}; evidence ${outputDir}`)
      child = spawn(process.execPath, [cli, 'test', ...args], {
        cwd: frontend, stdio: 'inherit', env: { ...process.env, ADDP_BROWSER_RUN: JSON.stringify(run) }
      })
      result = await new Promise((done, fail) => { child.once('error', fail); child.once('exit', code => done(code ?? 1)) })
    }
  } finally {
    try {
      await cleanupFixtures(runtimeDir)
      rmSync(runtimeDir, { recursive: true, force: true })
    } catch (error) {
      result = 1
      throw error
    } finally {
      for (const claim of claims) rmSync(claim.lease, { force: true })
      if (result === 0 && !interrupted) rmSync(outputDir, { recursive: true, force: true })
      else {
        // Playwright clears outputDir at startup, so restore the run manifest
        // only after it has finished writing screenshots and traces.
        mkdirSync(outputDir, { recursive: true })
        if (run) writeFileSync(resolve(outputDir, 'run.json'), JSON.stringify(run, null, 2))
        console.error(`Browser failure evidence retained: ${outputDir}`)
      }
      clearTimeout(escalation)
      process.removeListener('SIGINT', onInterrupt)
      process.removeListener('SIGTERM', onTerminate)
    }
  }
  return interrupted === 'SIGINT' ? 130 : interrupted === 'SIGTERM' ? 143 : result
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { process.exitCode = await runBrowserGate(process.cwd(), process.argv.slice(2)) }
  catch (error) { console.error(error); process.exitCode = 1 }
}
