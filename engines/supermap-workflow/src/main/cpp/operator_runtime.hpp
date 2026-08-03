#pragma once

#include "operator_catalog.hpp"
#include "supermap_runtime.hpp"

#include <functional>
#include <string>
#include <unordered_map>

namespace addp::supermap {

using ResolvedParams = std::unordered_map<std::string, RuntimeValue>;
using OperatorHandler = std::function<RuntimeValue(const ResolvedParams&, ExecutionContext&)>;

class OperatorRuntime final {
 public:
  explicit OperatorRuntime(addp::workflow::OperatorCatalog catalog);

  Json execute_workflow(const std::string& execution_id, const Json& request) const;
  const addp::workflow::OperatorCatalog& catalog() const;

 private:
  RuntimeValue execute_operator(
      const std::string& id, const ResolvedParams& params, ExecutionContext& context) const;

  addp::workflow::OperatorCatalog catalog_;
  std::unordered_map<std::string, OperatorHandler> handlers_;
};

}  // namespace addp::supermap
