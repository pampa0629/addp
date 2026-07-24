export function validationIssueParamName(issue) {
  const match = String(issue?.path || '').match(/\.params(?:\.([^.[\]]+)|\[['"]([^'"]+)['"]\])/)
  return match?.[1] || match?.[2] || ''
}

export function validationMessagesForParams(issues, paramNames) {
  const names = new Set((paramNames || []).filter(Boolean))
  const messages = []
  const seen = new Set()

  for (const issue of issues || []) {
    if (issue?.severity === 'warning') continue
    const paramName = issue?.paramName || validationIssueParamName(issue)
    const message = String(issue?.message || '').trim()
    if (!names.has(paramName) || !message || seen.has(message)) continue
    seen.add(message)
    messages.push(message)
  }
  return messages
}

export function groupValidationIssues(issues) {
  const groups = []
  const byKey = new Map()

  for (const issue of issues || []) {
    const key = issue?.nodeId || '__workflow__'
    let group = byKey.get(key)
    if (!group) {
      group = {
        key,
        label: issue?.nodeLabel || issue?.nodeId || '',
        issues: []
      }
      byKey.set(key, group)
      groups.push(group)
    }
    group.issues.push(issue)
  }
  return groups
}
