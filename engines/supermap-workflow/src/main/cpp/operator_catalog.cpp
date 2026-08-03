#include "operator_catalog.hpp"

#include <fstream>
#include <set>
#include <stdexcept>

namespace addp::workflow {
namespace {

const std::set<std::string> kExecutionModes = {"direct", "workflow"};
const std::set<std::string> kEffects = {"ddl", "external_effect", "read", "write"};

std::string require_non_empty_text(
    const Json& value, const char* field, const std::string& context) {
  if (!value.contains(field) || !value.at(field).is_string() ||
      value.at(field).get<std::string>().empty()) {
    throw std::runtime_error(context + "." + field + " must be a non-empty string");
  }
  return value.at(field).get<std::string>();
}

void validate_string_set(
    const Json& descriptor,
    const char* field,
    const std::set<std::string>& allowed,
    const std::string& id) {
  const auto values = descriptor.find(field);
  if (values == descriptor.end() || !values->is_array() || values->empty()) {
    throw std::runtime_error("operator " + id + " must declare " + field);
  }
  std::set<std::string> unique;
  for (const auto& value : *values) {
    if (!value.is_string() || !allowed.contains(value.get<std::string>()) ||
        !unique.insert(value.get<std::string>()).second) {
      throw std::runtime_error("operator " + id + " has invalid " + field);
    }
  }
}

void validate_output_ports(const Json& descriptor, const std::string& id) {
  const auto ports = descriptor.find("output_ports");
  if (ports == descriptor.end() || !ports->is_array() || ports->empty()) {
    throw std::runtime_error("operator " + id + " must declare output_ports");
  }
  std::set<std::string> names;
  std::size_t default_count = 0;
  for (const auto& port : *ports) {
    if (!port.is_object()) {
      throw std::runtime_error("operator " + id + " has invalid output port");
    }
    const std::string name = require_non_empty_text(port, "name", "operator " + id + " output");
    require_non_empty_text(port, "type", "operator " + id + " output " + name);
    if (!names.insert(name).second) {
      throw std::runtime_error("operator " + id + " has duplicate output ports");
    }
    if (port.value("is_default", false)) {
      ++default_count;
    }
  }
  if (default_count != 1) {
    throw std::runtime_error("operator " + id + " must have exactly one default output port");
  }
}

}  // namespace

OperatorCatalog::OperatorCatalog(std::vector<Json> descriptors, OperatorMap by_id)
    : descriptors_(std::move(descriptors)), by_id_(std::move(by_id)) {}

OperatorCatalog OperatorCatalog::load(const std::string& path) {
  std::ifstream input(path);
  if (!input) {
    throw std::runtime_error("failed to open operator catalog: " + path);
  }
  Json document;
  input >> document;
  const auto operators = document.find("operators");
  if (operators == document.end() || !operators->is_array() || operators->empty()) {
    throw std::runtime_error("operator catalog must contain a non-empty operators array");
  }

  std::vector<Json> descriptors;
  OperatorMap by_id;
  descriptors.reserve(operators->size());
  for (const auto& descriptor : *operators) {
    if (!descriptor.is_object()) {
      throw std::runtime_error("operator descriptor must be an object");
    }
    const std::string id = require_non_empty_text(descriptor, "id", "operator");
    if (require_non_empty_text(descriptor, "name", "operator " + id) != id) {
      throw std::runtime_error("operator name must equal id: " + id);
    }
    if (descriptor.value("engine_type", "") != "supermap_workflow") {
      throw std::runtime_error("operator has invalid engine_type: " + id);
    }
    validate_string_set(descriptor, "execution_modes", kExecutionModes, id);
    validate_string_set(descriptor, "effects", kEffects, id);
    validate_output_ports(descriptor, id);
    if (!by_id.emplace(id, descriptor).second) {
      throw std::runtime_error("duplicate operator id: " + id);
    }
    descriptors.push_back(descriptor);
  }
  return OperatorCatalog(std::move(descriptors), std::move(by_id));
}

const std::vector<Json>& OperatorCatalog::descriptors() const { return descriptors_; }

const OperatorMap& OperatorCatalog::by_id() const { return by_id_; }

const Json& OperatorCatalog::require(const std::string& id) const {
  const auto descriptor = by_id_.find(id);
  if (descriptor == by_id_.end()) {
    throw std::invalid_argument("unsupported operator: " + id);
  }
  return descriptor->second;
}

bool OperatorCatalog::supports_mode(const std::string& id, const std::string& mode) const {
  const Json& descriptor = require(id);
  for (const auto& value : descriptor.at("execution_modes")) {
    if (value == mode) {
      return true;
    }
  }
  return false;
}

std::string OperatorCatalog::default_output_port(const std::string& id) const {
  for (const auto& port : require(id).at("output_ports")) {
    if (port.value("is_default", false)) {
      return port.at("name").get<std::string>();
    }
  }
  throw std::logic_error("operator has no default output port: " + id);
}

}  // namespace addp::workflow
