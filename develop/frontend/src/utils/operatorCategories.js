// Runtime 的分类路径是唯一归属；完整路径区分不同父组下的同名目录。
export function buildOperatorCategories(operators) {
  const categories = []
  for (const operator of operators) {
    let children = categories
    let category
    const path = []
    for (const name of operator.categoryPath) {
      path.push(name)
      category = children.find(item => item.name === name)
      if (!category) {
        category = { key: JSON.stringify(path), name, path: [...path], children: [], operators: [], count: 0 }
        children.push(category)
      }
      category.count += 1
      children = category.children
    }
    category.operators.push(operator)
  }
  return categories
}

export function filterOperatorCategories(categories, query) {
  const text = query.trim().toLowerCase()
  if (!text) return categories
  return categories.flatMap(category => {
    if (category.name.toLowerCase().includes(text)) return [category]
    const children = filterOperatorCategories(category.children, text)
    const operators = category.operators.filter(operator => (
      [operator.name, operator.displayName, operator.description].some(value => value.toLowerCase().includes(text))
    ))
    const count = operators.length + children.reduce((total, child) => total + child.count, 0)
    return count ? [{ ...category, children, operators, count }] : []
  })
}

export function operatorCategoryKeys(categories) {
  return categories.flatMap(category => [category.key, ...operatorCategoryKeys(category.children)])
}
