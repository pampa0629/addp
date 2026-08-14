import test from 'node:test'
import assert from 'node:assert/strict'
import { initializeMermaidTheme, resolveMermaidTheme } from '../src/utils/mermaidTheme.js'

const themeValues = {
  '--addp-bg-primary': '#101114',
  '--addp-bg-secondary': '#181a20',
  '--addp-text-primary': '#f2f3f5',
  '--addp-text-secondary': '#c6c9d0',
  '--addp-border-color': '#3c4048',
  '--addp-border-color-light': '#2e3138',
  '--addp-graph-edge-default': '#7b8494',
  '--addp-graph-edge-label': '#e0e3e8',
  '--addp-graph-edge-label-stroke': '#101114',
  '--addp-graph-node-stroke': '#3c4048',
  '--addp-graph-label-light': '#ffffff',
  '--addp-graph-label-dark': '#111827',
  ...Object.fromEntries(Array.from({ length: 12 }, (_, index) => [
    `--addp-graph-category-${index + 1}`,
    `category-${index + 1}`
  ]))
}

const styles = {
  getPropertyValue(variable) {
    return ` ${themeValues[variable] || ''} `
  }
}

test('Mermaid theme resolves every visual value from ADDP theme tokens', () => {
  const theme = resolveMermaidTheme(styles)

  assert.equal(theme.background, '#101114')
  assert.equal(theme.surface, '#181a20')
  assert.equal(theme.edgeLabelBackground, '#101114')
  assert.deepEqual(theme.categories, Array.from({ length: 12 }, (_, index) => `category-${index + 1}`))
})

test('Mermaid initialization maps ADDP background, text, border and edge tokens', () => {
  const originalDocument = globalThis.document
  const originalGetComputedStyle = globalThis.getComputedStyle
  let initialized
  globalThis.document = { documentElement: {} }
  globalThis.getComputedStyle = () => styles

  try {
    initializeMermaidTheme({ initialize: config => { initialized = config } }, { er: { useMaxWidth: true } })
  } finally {
    globalThis.document = originalDocument
    globalThis.getComputedStyle = originalGetComputedStyle
  }

  assert.equal(initialized.theme, 'base')
  assert.equal(initialized.themeVariables.background, '#101114')
  assert.equal(initialized.themeVariables.entityBkg, '#181a20')
  assert.equal(initialized.themeVariables.relationshipLabelBackground, '#101114')
  assert.deepEqual(initialized.er, { useMaxWidth: true })
})
