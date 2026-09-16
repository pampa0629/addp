import {
  buildPlanRouteQuery,
  resolvePlanRouteState,
} from "./planRouteState.js";

// Rule and plan lists share the dialog/pagination contract, not object identity.
export const buildRuleRouteQuery = (state) => {
  const query = buildPlanRouteQuery({ ...state, taskID: state.ruleID });
  if (query.task_id) {
    query.rule_id = query.task_id;
    delete query.task_id;
  }
  return query;
};
export const resolveRuleRouteState = (query) => {
  const { rule_id, task_id: ignored, ...rest } = query;
  const state = resolvePlanRouteState({
    ...rest,
    ...(rule_id ? { task_id: rule_id } : {}),
  });
  const canonical = buildRuleRouteQuery({ ...state, ruleID: state.taskID });
  return {
    ...state,
    ruleID: state.taskID,
    query: canonical,
    changed:
      Object.keys(canonical).length !== Object.keys(query).length ||
      Object.keys(canonical).some((key) => canonical[key] !== query[key]),
  };
};
