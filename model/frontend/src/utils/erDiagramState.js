export const filterERDiagramByDomain = (entities = [], relations = [], domainId = null) => {
  const visibleEntities = domainId
    ? entities.filter(entity => entity.domain_id === domainId)
    : [...entities]
  const visibleEntityIds = new Set(visibleEntities.map(entity => entity.id))
  const visibleRelations = relations.filter(relation =>
    visibleEntityIds.has(relation.source_entity) && visibleEntityIds.has(relation.target_entity)
  )

  return { entities: visibleEntities, relations: visibleRelations }
}
