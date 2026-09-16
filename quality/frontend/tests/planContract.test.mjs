import assert from "node:assert/strict";
import test from "node:test";
import {
  bindingAlias,
  createCheckItem,
  serializeCheckItems,
  editableConstraint,
  serializeConstraint,
} from "../src/utils/planContract.js";
import { resolveRuleRouteState } from "../src/utils/ruleRouteState.js";

test("rule route canonicalizes editor identity and pagination", () => {
  const state = resolveRuleRouteState({
    create: "1",
    rule_id: "9",
    page: "2",
    page_size: "50",
  });
  assert.equal(state.mode, "edit");
  assert.equal(state.ruleID, "9");
  assert.deepEqual(state.query, { rule_id: "9", page: "2", page_size: "50" });
});
test("check items carry fixed revision references and independent bindings, never copied constraints", () => {
  const rule = {
    id: 7,
    revision_no: 3,
    type: "allowed_values",
    params: { values: ["signup", "leader"] },
  };
  const a = createCheckItem(
    rule,
    "members",
    "123e4567-e89b-12d3-a456-426614174000",
  );
  a.bindings.column = "status";
  const b = createCheckItem(
    rule,
    "events",
    "123e4567-e89b-12d3-a456-426614174001",
  );
  b.bindings.column = "member_status";
  const items = serializeCheckItems([a, b]);
  assert.equal(items[0].rule_id, items[1].rule_id);
  assert.equal(items[0].revision_no, 3);
  assert.notEqual(items[0].rule_key, items[1].rule_key);
  assert.deepEqual(items[0].bindings, { table: "members", column: "status" });
  assert.equal(Object.hasOwn(items[0], "rule"), false);
  assert.equal(Object.hasOwn(items[0], "params"), false);
  rule.params.values.push("changed");
  assert.deepEqual(a.rule.params.values, ["signup", "leader"]);
});
test("neutral constraints preserve typed predicates and drop inactive condition values", () => {
  const raw = {
    when: { operator: "eq", value: true },
    then: { operator: "is_true", value: "ignored" },
  };
  assert.deepEqual(serializeConstraint("predicate_implication", raw), {
    when: { operator: "eq", value: true },
    then: { operator: "is_true" },
  });
  assert.deepEqual(
    serializeConstraint(
      "row_count",
      editableConstraint("row_count", { exact: 3 }),
    ),
    { exact: 3 },
  );
  assert.deepEqual(
    serializeConstraint("row_count", {
      mode: "range",
      min: 1,
      max: null,
      exact: 5,
    }),
    { min: 1 },
  );
});
test("binding aliases are stable SQL-safe names", () => {
  assert.equal(bindingAlias("DWD Outdoor Person", 5), "dwd_outdoor_person");
  assert.equal(bindingAlias("123", 5), "table_5");
});
