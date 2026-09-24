export function parseDevPorts(value) {
  return Object.fromEntries(
    (value || '').split(',').filter(Boolean).map(entry => entry.split(':'))
  )
}
