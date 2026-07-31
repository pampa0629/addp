export const paginateEngines = (engines, page, pageSize) => {
  const start = (page - 1) * pageSize
  return engines.slice(start, start + pageSize)
}
