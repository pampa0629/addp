export function translateDynamicKey(translate, namespace, segment) {
  if (typeof segment !== 'string' || segment.length === 0) return ''
  return translate(`${namespace}.${segment}`)
}
