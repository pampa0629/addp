// This vocabulary belongs to Quality constraints, not SQL or task expressions.
export const ASSERTION_OPERATORS = ['field', 'value', 'number', 'today', 'and', 'or', 'not', 'eq', 'ne', 'lt', 'lte', 'gt', 'gte', 'is_null', 'not_null', 'exists', 'count', 'count_distinct'];
export const ASSERTION_COLLECTIONS = ['exists', 'count', 'count_distinct'];
export const defaultAssertion = (op = 'eq') => {
  if (op === 'field') return { op, field: 'key' };
  if (op === 'value') return { op, value: '' };
  if (op === 'number') return { op, value: '0' };
  if (op === 'today') return { op };
  if (ASSERTION_COLLECTIONS.includes(op)) return { op, relation: 'detail', ...(op === 'count_distinct' ? { field: 'item' } : {}) };
  if (op === 'and' || op === 'or') return { op, args: [defaultAssertion(), defaultAssertion()] };
  if (op === 'not') return { op, args: [defaultAssertion()] };
  if (op === 'is_null' || op === 'not_null') return { op, args: [defaultAssertion('field')] };
  return { op, args: [defaultAssertion('field'), defaultAssertion('value')] };
};
export const assertionInputs = (root) => {
  const inputs = Object.create(null);
  inputs[''] = new Set();
  const visit = (node) => {
    if (ASSERTION_COLLECTIONS.includes(node.op)) inputs[node.relation] ||= new Set();
    if (node.op === 'field' || node.op === 'count_distinct') {
      const role = node.relation || '';
      (inputs[role] ||= new Set()).add(node.field);
    }
    (node.args || []).forEach(visit);
    if (node.where) visit(node.where);
  };
  visit(root);
  return Object.fromEntries(Object.entries(inputs).map(([role, symbols]) => [role, [...symbols].sort()]));
};
export const assertionBindings = (root, previous = {}) => {
  const fields = {}, relations = {};
  for (const [role, symbols] of Object.entries(assertionInputs(root))) {
    const oldRelation = role && Object.hasOwn(previous.relations || {}, role) ? previous.relations[role] : null;
    const oldFields = role ? oldRelation?.fields : previous.fields;
    const mapping = Object.fromEntries(symbols.map(symbol => [symbol, Object.hasOwn(oldFields || {}, symbol) ? oldFields[symbol] : '']));
    if (role) relations[role] = { table: oldRelation?.table || '', fields: mapping };
    else Object.assign(fields, mapping);
  }
  return { fields, relations };
};
export const validAssertionBindings = (root, bindings, aliases) => {
  const expected = assertionInputs(root);
  if (Object.keys(bindings.relations || {}).length !== Object.keys(expected).length - 1) return false;
  return Object.entries(expected).every(([role, symbols]) => {
    const b = role ? bindings.relations?.[role] : bindings;
    return b && aliases.includes(b.table) && symbols.length === Object.keys(b.fields || {}).length && symbols.every(s => typeof b.fields?.[s] === 'string' && b.fields[s].trim() && b.fields[s].trim() === b.fields[s]);
  });
};
