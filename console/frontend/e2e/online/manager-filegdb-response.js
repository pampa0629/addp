// Test-only observer: the application receives its original fetch Promise and
// Response. Reading a clone never re-fetches an authenticated download URL.
export async function observeFileGDBDownload(page, path) {
  // The authenticated client binds fetch during module initialization. Install
  // before navigation so its original binding includes this test observer.
  await page.addInitScript(targetPath => {
    if (window.__addpFileGDBResponse) throw new Error('FileGDB response observer already installed')
    const original = window.fetch
    const records = []
    function observed(...args) {
      const pending = Reflect.apply(original, this, args)
      let requestURL
      try { requestURL = new URL(args[0] instanceof Request ? args[0].url : args[0], location.href).href }
      catch { return pending }
      if (requestURL === new URL(targetPath, location.href).href) {
        records.push(pending.then(response => {
          const copy = response.clone()
          const facts = { url: response.url, status: response.status,
            headers: Object.fromEntries(['content-type', 'content-disposition', 'content-length']
              .filter(name => response.headers.has(name)).map(name => [name, response.headers.get(name)])) }
          return copy.arrayBuffer().then(buffer => ({ ...facts, bytes: Array.from(new Uint8Array(buffer)) }))
        }).catch(() => ({ error: 'response_copy_failed' })))
      }
      return pending
    }
    window.__addpFileGDBResponse = { original, observed, records }
    window.fetch = observed
  }, path)
  return {
    read: frame => frame.evaluate(() => Promise.all(window.__addpFileGDBResponse.records)),
    dispose: frame => frame.evaluate(() => {
      const state = window.__addpFileGDBResponse
      if (window.fetch !== state.observed) throw new Error('FileGDB response observer was replaced')
      window.fetch = state.original
      delete window.__addpFileGDBResponse
    })
  }
}
