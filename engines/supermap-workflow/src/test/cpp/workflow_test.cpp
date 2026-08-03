#include "workflow.hpp"

#include <cstdlib>
#include <iostream>
#include <string>

namespace {

using addp::workflow::Json;
using addp::workflow::OperatorMap;

OperatorMap operators() {
  return {
      {"read",
       {{"execution_modes", Json::array({"workflow", "direct"})},
        {"effects", Json::array({"read"})},
        {"output_ports", Json::array({{{"name", "value"}, {"is_default", true}}})}}},
      {"write",
       {{"execution_modes", Json::array({"workflow"})},
        {"effects", Json::array({"write"})},
        {"output_ports", Json::array({{{"name", "result"}, {"is_default", true}}})}}},
      {"direct",
       {{"execution_modes", Json::array({"direct"})},
        {"effects", Json::array({"ddl"})},
        {"output_ports", Json::array({{{"name", "result"}, {"is_default", true}}})}}},
  };
}

Json request(Json tasks, Json effects = Json::array({"read", "write"})) {
  return {
      {"workflow_def", {{"tasks", std::move(tasks)}}},
      {"runtime", {{"execution_authorization", {{"id", 1}, {"effects", effects}}}}},
  };
}

void require(bool value, const std::string& message) {
  if (!value) {
    std::cerr << "FAILED: " << message << '\n';
    std::exit(1);
  }
}

template <typename Callable>
void require_validation_error(Callable&& callable, const std::string& expected) {
  try {
    callable();
  } catch (const addp::workflow::ValidationError& error) {
    require(std::string(error.what()).find(expected) != std::string::npos, error.what());
    return;
  }
  require(false, "expected validation error containing: " + expected);
}

}  // namespace

int main() {
  const auto registry = operators();
  Json tasks = Json::array(
      {{{"id", "second"},
        {"operator", "write"},
        {"params", {{"input", {{"$ref", "first"}}}}},
        {"depends_on", Json::array({"first"})}},
       {{"id", "first"},
        {"operator", "read"},
        {"params", Json::object()},
        {"depends_on", Json::array()}}});

  const auto plan = addp::workflow::validate_and_plan(request(tasks), registry);
  require(plan.tasks.size() == 2, "plan size");
  require(plan.tasks[0].at("id") == "first", "stable topological order");
  require(plan.tasks[1].at("id") == "second", "dependent task order");

  Json missing_dependency = tasks;
  missing_dependency[0]["depends_on"] = Json::array();
  require_validation_error(
      [&] { addp::workflow::validate_and_plan(request(missing_dependency), registry); },
      "does not declare it in depends_on");

  Json cyclic = tasks;
  cyclic[1]["depends_on"] = Json::array({"second"});
  require_validation_error(
      [&] { addp::workflow::validate_and_plan(request(cyclic), registry); }, "contains a cycle");

  require_validation_error(
      [&] {
        addp::workflow::validate_and_plan(
            request(tasks, Json::array({"read"})), registry);
      },
      "does not cover workflow effects");

  Json direct_task = Json::array(
      {{{"id", "direct"},
        {"operator", "direct"},
        {"params", Json::object()},
        {"depends_on", Json::array()}}});
  require_validation_error(
      [&] { addp::workflow::validate_and_plan(request(direct_task, Json::array({"ddl"})), registry); },
      "does not support workflow execution");

  return 0;
}
