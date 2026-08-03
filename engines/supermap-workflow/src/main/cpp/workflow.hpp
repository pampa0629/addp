#pragma once

#include <nlohmann/json.hpp>

#include <stdexcept>
#include <string>
#include <unordered_map>
#include <vector>

namespace addp::workflow {

using Json = nlohmann::json;

class ValidationError final : public std::runtime_error {
 public:
  using std::runtime_error::runtime_error;
};

struct Reference {
  std::string task_id;
  std::string port;
};

struct WorkflowPlan {
  std::vector<Json> tasks;
};

using OperatorMap = std::unordered_map<std::string, Json>;

std::vector<Reference> collect_references(const Json& value);
WorkflowPlan validate_and_plan(const Json& request, const OperatorMap& operators);
void validate_execution_authorization(
    const Json& request, const std::vector<Json>& tasks, const OperatorMap& operators);

}  // namespace addp::workflow
