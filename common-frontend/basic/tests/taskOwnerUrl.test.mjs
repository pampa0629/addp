import assert from 'node:assert/strict'
import test from 'node:test'

import { resolveConsoleOrigin } from '../src/utils/consoleOrigin.js'

test('resolves every registered module frontend development port to Console origin', () => {
  for (const port of ['5173', '5188', '5189', '5190', '5191', '5192']) {
    assert.equal(resolveConsoleOrigin({
      origin: `http://localhost:${port}`,
      protocol: 'http:',
      hostname: 'localhost',
      port
    }), 'http://localhost:5170')
  }
})

test('production, test and unallocated origins remain unchanged', () => {
  for (const port of ['', '4192', '5170', '5193', '8123']) {
    const origin = `https://localhost${port ? `:${port}` : ''}`
    assert.equal(resolveConsoleOrigin({origin, protocol:'https:', hostname:'localhost', port}), origin)
  }
})

test('resolves allocated module ports to the allocated Console port', () => {
  const environment = {
    VITE_ADDP_FRONTEND_PORTS: 'console:15170,manager:15174,ontology:15192',
    VITE_ADDP_CONSOLE_PORT: '15170'
  }
  assert.equal(resolveConsoleOrigin({
    origin: 'http://localhost:15174', protocol: 'http:', hostname: 'localhost', port: '15174'
  }, '', environment), 'http://localhost:15170')
  assert.equal(resolveConsoleOrigin({
    origin: 'http://localhost:4192', protocol: 'http:', hostname: 'localhost', port: '4192'
  }, '', environment), 'http://localhost:4192')
})
