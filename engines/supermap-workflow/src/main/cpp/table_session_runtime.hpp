#pragma once

#include "workflow.hpp"

#include <memory>
#include <string>

namespace addp::supermap {

class TableSessionRuntime final {
 public:
  TableSessionRuntime();
  ~TableSessionRuntime();

  TableSessionRuntime(const TableSessionRuntime&) = delete;
  TableSessionRuntime& operator=(const TableSessionRuntime&) = delete;

  addp::workflow::Json invoke(
      const std::string& operator_id,
      const addp::workflow::Json& params);

 private:
  class Impl;
  std::unique_ptr<Impl> impl_;
};

}  // namespace addp::supermap
