#include "http_server.hpp"

#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>

namespace {

std::string environment(const char* name, const std::string& fallback) {
  const char* value = std::getenv(name);
  return value == nullptr || std::string(value).empty() ? fallback : std::string(value);
}

int positive_integer(const char* name, int fallback, int maximum) {
  const std::string text = environment(name, std::to_string(fallback));
  std::size_t consumed = 0;
  int value = 0;
  try {
    value = std::stoi(text, &consumed);
  } catch (const std::exception&) {
    throw std::invalid_argument(std::string(name) + " must be a positive integer");
  }
  if (consumed != text.size() || value <= 0 || value > maximum) {
    throw std::invalid_argument(std::string(name) + " must be a positive integer");
  }
  return value;
}

}  // namespace

int main() {
  try {
    const std::string sdk_root = environment("SUPERMAP_CPP_SDK_ROOT", "/opt/supermap");
    const std::string operators =
        environment("SUPERMAP_OPERATORS_CONFIG", "/app/config/operators.json");
    const int port = positive_integer("PORT", 8103, 65535);
    const int threads = positive_integer("SUPERMAP_HTTP_THREADS", 4, 256);
    addp::supermap::HttpServer server(operators, sdk_root, threads);
    std::cout << "supermap-workflow-engine listening on http://0.0.0.0:" << port << '\n';
    server.listen(port);
    return 0;
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
