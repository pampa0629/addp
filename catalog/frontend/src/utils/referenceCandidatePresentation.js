export function referenceCandidateLabel(option, unavailableLabel, { compact = false } = {}) {
  const name = option?.name || unavailableLabel
  if (!option?.code) return name
  const characters = Array.from(option.code)
  const code = compact && characters.length > 24
    ? `${characters.slice(0, 12).join('')}…${characters.slice(-8).join('')}`
    : option.code
  return `${name} · ${code}`
}
