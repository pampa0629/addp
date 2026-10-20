#pragma once

#include "workflow.hpp"

#include <filesystem>
#include <string>
#include <vector>

namespace addp::workflow {

inline Json local_files_dependency(
    const std::filesystem::path& root,
    const std::vector<std::string>& files,
    bool require_license = false) {
  std::string missing;
  for (const auto& name : files) {
    if (!std::filesystem::is_regular_file(root / name)) {
      missing += (missing.empty() ? "" : ", ") + name;
    }
  }
  if (require_license) {
    bool found = false;
    if (std::filesystem::is_directory(root)) {
      for (const auto& entry : std::filesystem::directory_iterator(root)) {
        if (entry.is_regular_file() && entry.path().extension() == ".lic12") {
          found = true;
          break;
        }
      }
    }
    if (!found) {
      missing += (missing.empty() ? "" : ", ") + std::string("*.lic12");
    }
  }
  Json fact = {{"available", missing.empty()}, {"required", true}};
  if (!missing.empty()) {
    fact["details"] = "Missing local files: " + missing;
  }
  return fact;
}

inline Json runtime_health(
    const std::string& service, const std::string& version,
    long long uptime, std::size_t operators_count, Json dependencies) {
  bool ready = true;
  for (const auto& item : dependencies.items()) {
    ready = ready && (!item.value().at("required").get<bool>() ||
                      item.value().at("available").get<bool>());
  }
  return {{"status", ready ? "healthy" : "unhealthy"},
          {"service", service}, {"version", version},
          {"uptime", uptime < 0 ? 0 : uptime},
          {"operators_count", operators_count},
          {"dependencies", std::move(dependencies)}};
}

}  // namespace addp::workflow
