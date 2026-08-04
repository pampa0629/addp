#pragma once

#include "runtime_bridge.h"

#include <string>

namespace addp::supermap {

class HttpServer final {
 public:
  HttpServer(
      std::string operators_config,
      std::string sdk_root,
      int thread_count);
  ~HttpServer();

  HttpServer(const HttpServer&) = delete;
  HttpServer& operator=(const HttpServer&) = delete;

  void listen(int port);

 private:
  AddpSuperMapRuntime* runtime_ = nullptr;
  int thread_count_;
};

}  // namespace addp::supermap
