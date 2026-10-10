export function platformInspectionOwner(context) {
  if (context?.principal?.type !== 'user' || context.context?.type !== 'platform') return ''
  return context.principal.id || ''
}

export function platformGraph(definition, locale, colors) {
  return {
    entityTypes: definition.concepts.map(item => ({
      id: item.id, name: item.id, label: item.name[locale], color: colors.node
    })),
    relationTypes: definition.relations.map(item => ({
      id: `${item.from}/${item.kind}/${item.to}`, name: item.kind,
      source_type_id: item.from, target_type_id: item.to, color: colors.edge
    }))
  }
}

export function validCapability(value) {
  return typeof value === 'string' && value.length <= 128 && /^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$/.test(value)
}
