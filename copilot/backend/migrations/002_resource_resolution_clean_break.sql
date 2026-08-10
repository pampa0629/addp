-- Clean break: resource confirmation and domain generation use distinct scenario bindings.
DELETE FROM copilot.inference_scenario_bindings
WHERE scenario_code IN ('nl2sql', 'nl2dag', 'nl2transfer');

DROP TABLE IF EXISTS copilot.messages;
DROP TABLE IF EXISTS copilot.conversations;
DROP TABLE IF EXISTS copilot.matching_policies;
DROP TABLE IF EXISTS copilot.matching_policy;
