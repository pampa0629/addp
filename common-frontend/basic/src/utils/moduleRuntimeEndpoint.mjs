export function getRegisteredEndpoint(instance) {
  const address = instance?.module_url || instance?.health_check_url
  if (typeof address !== 'string' || !address) return null
  try {
    const url = new URL(address)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    const port = url.port || (url.protocol === 'https:' ? '443' : '80')
    return `${url.hostname}:${port}`
  } catch {
    return null
  }
}
