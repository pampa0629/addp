export function referenceCandidateLabel(option, unavailableLabel) {
  const name = option?.name || unavailableLabel
  return option?.code ? `${name} · ${option.code}` : name
}
