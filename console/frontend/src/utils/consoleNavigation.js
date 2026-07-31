export function splitConsoleRoute(fullPath) {
  const route = String(fullPath || '')
  const queryIndex = route.indexOf('?')
  if (queryIndex < 0) return [route, '']
  return [route.slice(0, queryIndex), route.slice(queryIndex + 1)]
}

export function consoleRouteModule(fullPath) {
  const [path] = splitConsoleRoute(fullPath)
  return path.split('/').filter(Boolean)[0] || ''
}

export function isSynchronizedIframeRoute(synchronizedModule, fullPath) {
  const module = consoleRouteModule(fullPath)
  return Boolean(synchronizedModule && module === synchronizedModule)
}
