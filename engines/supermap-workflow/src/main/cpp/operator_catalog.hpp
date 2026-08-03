#pragma once

#include "workflow.hpp"

#include <string>
#include <vector>

namespace addp::workflow {

class OperatorCatalog final {
 public:
  static OperatorCatalog load(const std::string& path);

  const std::vector<Json>& descriptors() const;
  const OperatorMap& by_id() const;
  const Json& require(const std::string& id) const;
  bool supports_mode(const std::string& id, const std::string& mode) const;
  std::string default_output_port(const std::string& id) const;

 private:
  OperatorCatalog(std::vector<Json> descriptors, OperatorMap by_id);

  std::vector<Json> descriptors_;
  OperatorMap by_id_;
};

}  // namespace addp::workflow
