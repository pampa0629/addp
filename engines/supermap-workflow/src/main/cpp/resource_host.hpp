#pragma once

#include <algorithm>
#include <cctype>
#include <cstdlib>
#include <string>

namespace addp::supermap {

inline std::string normalize_resource_host(std::string host) {
  std::string normalized = host;
  std::transform(normalized.begin(), normalized.end(), normalized.begin(), [](unsigned char c) {
    return static_cast<char>(std::tolower(c));
  });
  if (normalized != "localhost" && normalized != "127.0.0.1" && normalized != "::1") {
    return host;
  }
  const char* alias = std::getenv("SUPERMAP_RESOURCE_LOCALHOST_ALIAS");
  return alias == nullptr || std::string(alias).empty() ? host : std::string(alias);
}

}  // namespace addp::supermap
