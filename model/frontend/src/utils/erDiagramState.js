export const filterERDiagramByDomain = (entities = [], relations = [], domainId = null, includeRelated = false) => {
  if (!domainId) return { entities: [], relations: [] }
  const ids = new Set(entities.map(entity => entity.id))
  const validRelations = relations.filter(relation => ids.has(relation.source_entity) && ids.has(relation.target_entity))
  if (domainId === 'all') return { entities: [...entities], relations: validRelations }
  const primary = new Set(entities.filter(entity => entity.domain_id === domainId).map(entity => entity.id))
  const visibleRelations = validRelations.filter(relation => includeRelated
    ? primary.has(relation.source_entity) || primary.has(relation.target_entity)
    : primary.has(relation.source_entity) && primary.has(relation.target_entity))
  const visible = new Set(primary)
  visibleRelations.forEach(relation => { visible.add(relation.source_entity); visible.add(relation.target_entity) })
  return { entities: entities.filter(entity => visible.has(entity.id)), relations: visibleRelations }
}
