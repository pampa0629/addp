#include "workflow.hpp"

#include <algorithm>
#include <deque>
#include <set>
#include <unordered_set>

namespace addp::workflow {
namespace {

std::string required_text(const Json& value, const char* field, const std::string& context) {
  if (!value.contains(field) || !value.at(field).is_string()) {
    throw ValidationError(context + "." + field + " must be a non-empty string");
  }
  const std::string text = value.at(field).get<std::string>();
  if (text.empty()) {
    throw ValidationError(context + "." + field + " must be a non-empty string");
  }
  return text;
}

bool supports_mode(const Json& descriptor, const std::string& mode) {
  const auto modes = descriptor.find("execution_modes");
  if (modes == descriptor.end() || !modes->is_array()) {
    return false;
  }
  return std::find(modes->begin(), modes->end(), mode) != modes->end();
}

void collect_references_into(const Json& value, std::vector<Reference>& result) {
  if (value.is_object()) {
    const auto ref = value.find("$ref");
    if (ref != value.end()) {
      if (!ref->is_string() || ref->get<std::string>().empty()) {
        throw ValidationError("$ref must be a non-empty task id");
      }
      std::string port;
      const auto port_value = value.find("port");
      if (port_value != value.end()) {
        if (!port_value->is_string() || port_value->get<std::string>().empty()) {
          throw ValidationError("reference port must be a non-empty string");
        }
        port = port_value->get<std::string>();
      }
      result.push_back({ref->get<std::string>(), port});
      return;
    }
    for (const auto& [key, nested] : value.items()) {
      static_cast<void>(key);
      collect_references_into(nested, result);
    }
    return;
  }
  if (value.is_array()) {
    for (const auto& nested : value) {
      collect_references_into(nested, result);
    }
  }
}

std::unordered_set<std::string> output_ports(const Json& descriptor) {
  std::unordered_set<std::string> result;
  const auto ports = descriptor.find("output_ports");
  if (ports == descriptor.end() || !ports->is_array()) {
    return result;
  }
  for (const auto& port : *ports) {
    if (port.is_object() && port.value("name", "") != "") {
      result.insert(port.at("name").get<std::string>());
    }
  }
  return result;
}

}  // namespace

std::vector<Reference> collect_references(const Json& value) {
  std::vector<Reference> result;
  collect_references_into(value, result);
  return result;
}

void validate_execution_authorization(
    const Json& request, const std::vector<Json>& tasks, const OperatorMap& operators) {
  const auto runtime = request.find("runtime");
  if (runtime == request.end() || !runtime->is_object()) {
    throw ValidationError("runtime.execution_authorization.id must be a positive integer");
  }
  const auto authorization = runtime->find("execution_authorization");
  if (authorization == runtime->end() || !authorization->is_object() ||
      !authorization->contains("id") || !authorization->at("id").is_number_integer() ||
      authorization->at("id").get<long long>() <= 0) {
    throw ValidationError("runtime.execution_authorization.id must be a positive integer");
  }
  const auto effects = authorization->find("effects");
  if (effects == authorization->end() || !effects->is_array() || effects->empty()) {
    throw ValidationError("runtime.execution_authorization.effects must be a non-empty array");
  }

  const std::set<std::string> allowed = {"read", "write", "ddl", "external_effect"};
  std::set<std::string> authorized;
  for (const auto& effect : *effects) {
    if (!effect.is_string() || !allowed.contains(effect.get<std::string>()) ||
        !authorized.insert(effect.get<std::string>()).second) {
      throw ValidationError("runtime.execution_authorization.effects is invalid");
    }
  }

  std::set<std::string> required;
  for (const auto& task : tasks) {
    const std::string operator_id = task.at("operator").get<std::string>();
    const auto descriptor = operators.find(operator_id);
    if (descriptor == operators.end() || !supports_mode(descriptor->second, "workflow")) {
      throw ValidationError("operator does not support workflow execution: " + operator_id);
    }
    const auto descriptor_effects = descriptor->second.find("effects");
    if (descriptor_effects == descriptor->second.end() || !descriptor_effects->is_array() ||
        descriptor_effects->empty()) {
      throw ValidationError("operator effects are required: " + operator_id);
    }
    for (const auto& effect : *descriptor_effects) {
      if (!effect.is_string() || !allowed.contains(effect.get<std::string>())) {
        throw ValidationError("operator effect is invalid: " + operator_id);
      }
      required.insert(effect.get<std::string>());
    }
  }
  for (const auto& effect : required) {
    if (!authorized.contains(effect)) {
      throw ValidationError("runtime execution authorization does not cover workflow effects");
    }
  }
}

WorkflowPlan validate_and_plan(const Json& request, const OperatorMap& operators) {
  const auto definition = request.find("workflow_def");
  if (definition == request.end() || !definition->is_object()) {
    throw ValidationError("workflow_def.tasks must be a non-empty array");
  }
  const auto tasks_value = definition->find("tasks");
  if (tasks_value == definition->end() || !tasks_value->is_array() || tasks_value->empty()) {
    throw ValidationError("workflow_def.tasks must be a non-empty array");
  }

  std::vector<Json> tasks;
  tasks.reserve(tasks_value->size());
  std::unordered_map<std::string, std::size_t> positions;
  for (std::size_t index = 0; index < tasks_value->size(); ++index) {
    const Json& task = tasks_value->at(index);
    const std::string context = "workflow_def.tasks[" + std::to_string(index) + "]";
    if (!task.is_object()) {
      throw ValidationError(context + " must be an object");
    }
    const std::string id = required_text(task, "id", context);
    required_text(task, "operator", context);
    if (!task.contains("params") || !task.at("params").is_object()) {
      throw ValidationError(context + ".params must be an object");
    }
    if (!task.contains("depends_on") || !task.at("depends_on").is_array()) {
      throw ValidationError(context + ".depends_on must be a string array");
    }
    if (!positions.emplace(id, index).second) {
      throw ValidationError("duplicate task id: " + id);
    }
    tasks.push_back(task);
  }

  std::vector<std::vector<std::size_t>> outgoing(tasks.size());
  std::vector<std::size_t> indegree(tasks.size(), 0);
  for (std::size_t index = 0; index < tasks.size(); ++index) {
    const Json& task = tasks[index];
    const std::string id = task.at("id").get<std::string>();
    const std::string operator_id = task.at("operator").get<std::string>();
    const auto descriptor = operators.find(operator_id);
    if (descriptor == operators.end() || !supports_mode(descriptor->second, "workflow")) {
      throw ValidationError("operator does not support workflow execution: " + operator_id);
    }

    std::unordered_set<std::string> dependencies;
    for (const auto& dependency_value : task.at("depends_on")) {
      if (!dependency_value.is_string() || dependency_value.get<std::string>().empty()) {
        throw ValidationError("task " + id + " depends_on contains an invalid task id");
      }
      const std::string dependency = dependency_value.get<std::string>();
      if (dependency == id) {
        throw ValidationError("task " + id + " cannot depend on itself");
      }
      if (!dependencies.insert(dependency).second) {
        throw ValidationError("task " + id + " contains duplicate dependencies");
      }
      const auto dependency_position = positions.find(dependency);
      if (dependency_position == positions.end()) {
        throw ValidationError("task " + id + " depends on unknown task " + dependency);
      }
      outgoing[dependency_position->second].push_back(index);
      ++indegree[index];
    }

    for (const auto& reference : collect_references(task.at("params"))) {
      const auto source_position = positions.find(reference.task_id);
      if (source_position == positions.end()) {
        throw ValidationError("task " + id + " references unknown task " + reference.task_id);
      }
      if (!dependencies.contains(reference.task_id)) {
        throw ValidationError(
            "task " + id + " references task " + reference.task_id +
            " but does not declare it in depends_on");
      }
      if (!reference.port.empty()) {
        const std::string source_operator =
            tasks[source_position->second].at("operator").get<std::string>();
        if (!output_ports(operators.at(source_operator)).contains(reference.port)) {
          throw ValidationError(
              "task " + id + " references unknown output port " + reference.port);
        }
      }
    }
  }

  std::deque<std::size_t> ready;
  for (std::size_t index = 0; index < indegree.size(); ++index) {
    if (indegree[index] == 0) {
      ready.push_back(index);
    }
  }
  std::vector<Json> ordered;
  ordered.reserve(tasks.size());
  while (!ready.empty()) {
    const std::size_t current = ready.front();
    ready.pop_front();
    ordered.push_back(tasks[current]);
    for (const std::size_t next : outgoing[current]) {
      --indegree[next];
      if (indegree[next] == 0) {
        const auto insertion = std::upper_bound(ready.begin(), ready.end(), next);
        ready.insert(insertion, next);
      }
    }
  }
  if (ordered.size() != tasks.size()) {
    throw ValidationError("workflow dependency graph contains a cycle");
  }

  validate_execution_authorization(request, tasks, operators);
  return {std::move(ordered)};
}

}  // namespace addp::workflow
