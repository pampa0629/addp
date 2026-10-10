// Test-only Chromium diagnostics. A stage describes when a message was observed,
// not proof of which GL call caused it. Chromium can stop repeating the message.
const readPixelsWarning = /^\[\.WebGL-0x[0-9a-f]+\]GL Driver Message \(OpenGL, Performance, GL_CLOSE_PATH_NV, High\): GPU stall due to ReadPixels(?: \(this message will no longer repeat\))?$/

export function createBrowserGpuDiagnostics(initialStage) {
  let stage = initialStage
  const counts = new Map()
  return {
    setStage(value) { stage = value },
    record(message) {
      if (message.type() !== 'warning' || !readPixelsWarning.test(message.text())) return false
      counts.set(stage, (counts.get(stage) || 0) + 1)
      return true
    },
    snapshot() {
      const stages = [...counts].map(([stage, count]) => ({ source: 'chromium-webgl-driver', stage, count }))
      return { count: stages.reduce((sum, item) => sum + item.count, 0), stages }
    }
  }
}
