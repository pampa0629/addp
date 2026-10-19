import { expect, it } from 'vitest'
import { graphBrowserDiagnostic } from '../e2e/graph-theme.fixture.js'

const message = (text, url = '') => ({
  text: () => text, type: () => 'warning',
  location: () => ({ url, lineNumber: 12, columnNumber: 3 })
})

it('keeps a Canvas warning actionable with its observed stage and source', () => {
  expect(graphBrowserDiagnostic(message('Canvas2D: Multiple readback operations', 'https://example.test/assets/graph.js?token=private#state'), 'theme:dark')).toEqual({
    stage: 'theme:dark', type: 'warning', message: 'Canvas2D: Multiple readback operations',
    source: '/assets/graph.js', line: 12, column: 3
  })
})

it('redacts supplied credentials, encoded credentials, bearer tokens and URL queries', () => {
  const diagnostic = graphBrowserDiagnostic(message('password=p@ss/word encoded=p%40ss%2Fword Authorization: Bearer private-token https://u:password@example.test/api?secret=private#state'), 'login', ['p@ss/word'])
  expect(diagnostic.message).toBe('password=[redacted] encoded=[redacted] Authorization: Bearer [redacted] https://example.test/api')
})

it('redacts a JWT even when it has no bearer prefix', () => {
  expect(graphBrowserDiagnostic(message('session eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiI0MiJ9.signature'), 'login').message).toBe('session [redacted]')
})

it('preserves unknown warnings and handles browser-generated locations', () => {
  const diagnostic = graphBrowserDiagnostic(message('unknown third-party warning'), 'knowledge-service:config')
  expect(diagnostic).toMatchObject({ type: 'warning', message: 'unknown third-party warning', source: '' })
})
