import { defaultAssertion, assertionBindings } from './assertion.js';
export const PLAN_TYPES = [
  "not_null",
  "allowed_values",
  "format",
  "length",
  "value_range",
  "unique_key",
  "foreign_key",
  "predicate_implication",
  "row_count",
  "relational_assertion",
];
export const PLAN_OPERATORS = [
  "eq",
  "not_eq",
  "is_null",
  "is_not_null",
  "is_true",
  "is_false",
];

export const bindingAlias = (code, fallback) => {
  const normalized = String(code || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9_]/g, "_")
    .replace(/_+/g, "_")
    .replace(/^_+|_+$/g, "");
  if (/^[a-z]/.test(normalized)) return normalized;
  return `table_${fallback}`;
};

export const createCheckItem = (
  rule,
  table = "",
  key = crypto.randomUUID(),
) => {
  const type = rule.type;
  const bindings = { table };
  if (type === 'relational_assertion') Object.assign(bindings, assertionBindings(rule.params.assertion));
  if (
    ["not_null", "allowed_values", "format", "length", "value_range"].includes(
      type,
    )
  )
    bindings.column = "";
  if (["unique_key", "foreign_key"].includes(type)) bindings.columns = [];
  if (type === "foreign_key")
    Object.assign(bindings, { reference_table: "", reference_columns: [] });
  if (type === "predicate_implication")
    Object.assign(bindings, { when_column: "", then_column: "" });
  return {
    rule_key: key,
    rule_id: rule.id,
    revision_no: rule.revision_no,
    severity: "error",
    disabled: false,
    bindings,
    rule: {
      ...JSON.parse(JSON.stringify(rule)),
      rule_id: rule.id,
      latest_revision_no: rule.revision_no,
    },
  };
};
export const serializeCheckItems = (items) =>
  items.map((item) => ({
    rule_key: item.rule_key,
    rule_id: item.rule_id,
    revision_no: item.revision_no,
    severity: item.severity,
    disabled: Boolean(item.disabled),
    bindings: JSON.parse(JSON.stringify(item.bindings)),
  }));
export const defaultConstraint = (type) => {
  if (type === 'relational_assertion') return { assertion: defaultAssertion() };
  if (["format", "length", "value_range"].includes(type))
    return {
      constraint:
        type === "format" ? { pattern: "" } : { min: null, max: null },
    };
  if (type === "allowed_values") return { values: [] };
  if (type === "row_count")
    return { mode: "range", min: 1, max: null, exact: null };
  if (type === "predicate_implication")
    return {
      when: { operator: "eq", value: "" },
      then: { operator: "eq", value: "" },
    };
  return {};
};
export const editableConstraint = (type, raw) => {
  const params = JSON.parse(JSON.stringify(raw));
  if (type === "row_count")
    params.mode = Object.hasOwn(params, "exact") ? "exact" : "range";
  return params;
};
export const serializeConstraint = (type, raw) => {
  const p = JSON.parse(JSON.stringify(raw));
  if (type === "row_count")
    return p.mode === "exact"
      ? { exact: p.exact }
      : Object.fromEntries(
          ["min", "max"].filter((k) => p[k] != null).map((k) => [k, p[k]]),
        );
  if (["format", "length", "value_range"].includes(type))
    return {
      constraint: Object.fromEntries(
        Object.entries(p.constraint).filter(([, v]) => v != null),
      ),
    };
  if (type === "predicate_implication") {
    const condition = (c) => ({
      operator: c.operator,
      ...(["eq", "not_eq"].includes(c.operator) ? { value: c.value } : {}),
    });
    return { when: condition(p.when), then: condition(p.then) };
  }
  return p;
};
