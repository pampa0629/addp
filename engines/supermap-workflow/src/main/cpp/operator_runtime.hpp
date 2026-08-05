#pragma once

#include "operator_catalog.hpp"
#include "supermap_runtime.hpp"
#include "table_session_runtime.hpp"

#include <functional>
#include <string>
#include <unordered_map>

namespace addp::supermap {

using ResolvedParams = std::unordered_map<std::string, RuntimeValue>;
using OperatorHandler = std::function<RuntimeValue(const ResolvedParams&, ExecutionContext&)>;
using DirectOperatorHandler = std::function<Json(const Json&)>;

class OperatorRuntime final {
 public:
  explicit OperatorRuntime(addp::workflow::OperatorCatalog catalog);

  Json execute_workflow(const std::string& execution_id, const Json& request) const;
  Json invoke_direct(const std::string& id, const Json& params) const;
  const addp::workflow::OperatorCatalog& catalog() const;

 private:
  RuntimeValue execute_operator(
      const std::string& id, const ResolvedParams& params, ExecutionContext& context) const;

  addp::workflow::OperatorCatalog catalog_;
  std::unordered_map<std::string, OperatorHandler> handlers_;
  std::unordered_map<std::string, DirectOperatorHandler> direct_handlers_;
  mutable TableSessionRuntime table_sessions_;
};

}  // namespace addp::supermap
