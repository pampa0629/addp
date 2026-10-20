#include "runtime_health.hpp"

#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <random>

namespace {
void require(bool value, const char* message) {
  if (!value) {
    std::cerr << message << '\n';
    std::exit(1);
  }
}
}

int main() {
  using addp::workflow::Json;
  using addp::workflow::runtime_health;
  Json dependencies = {{"sdk", {{"available", true}, {"required", true}}},
                       {"optional", {{"available", false}, {"required", false}}}};
  require(runtime_health("test", "1", 0, 2, dependencies).at("status") == "healthy",
          "optional dependencies must not block readiness");
  dependencies["sdk"]["available"] = false;
  const Json unhealthy = runtime_health("test", "1", 0, 2, dependencies);
  require(unhealthy.at("status") == "unhealthy", "required dependency must block readiness");
  require(unhealthy.at("service") == "test" && unhealthy.at("version") == "1",
          "unhealthy response must retain identity");

  const auto root = std::filesystem::temp_directory_path() /
      ("addp-supermap-health-" + std::to_string(std::random_device{}()));
  require(std::filesystem::create_directory(root), "create private temporary directory");
  const auto missing = addp::workflow::local_files_dependency(root, {"runtime.so"}, true);
  require(missing.at("required") && !missing.at("available"), "missing files fail readiness");
  require(!missing.contains("path"), "health facts must not expose host paths");
  std::ofstream(root / "runtime.so").put('\n');
  require(!addp::workflow::local_files_dependency(root, {"runtime.so"}, true).at("available"),
          "missing license file fails readiness");
  std::ofstream(root / "runtime.lic12").put('\n');
  require(addp::workflow::local_files_dependency(root, {"runtime.so"}, true).at("available"),
          "all required files available");
  std::filesystem::remove_all(root);
  return 0;
}
