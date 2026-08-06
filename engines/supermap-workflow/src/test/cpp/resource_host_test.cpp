#include "resource_host.hpp"

#include <cstdlib>
#include <iostream>
#include <string>

namespace {

int failures = 0;

void expect_equal(
    const std::string& actual, const std::string& expected, const std::string& message) {
  if (actual == expected) {
    return;
  }
  std::cerr << message << ": expected " << expected << ", got " << actual << '\n';
  ++failures;
}

}  // namespace

int main() {
  setenv("SUPERMAP_RESOURCE_LOCALHOST_ALIAS", "host.docker.internal", 1);
  expect_equal(
      addp::supermap::normalize_resource_host("localhost"),
      "host.docker.internal",
      "localhost alias");
  expect_equal(
      addp::supermap::normalize_resource_host("LOCALHOST"),
      "host.docker.internal",
      "case-insensitive localhost alias");
  expect_equal(
      addp::supermap::normalize_resource_host("127.0.0.1"),
      "host.docker.internal",
      "IPv4 loopback alias");
  expect_equal(
      addp::supermap::normalize_resource_host("::1"),
      "host.docker.internal",
      "IPv6 loopback alias");
  expect_equal(
      addp::supermap::normalize_resource_host("postgres.internal"),
      "postgres.internal",
      "non-loopback host");

  unsetenv("SUPERMAP_RESOURCE_LOCALHOST_ALIAS");
  expect_equal(
      addp::supermap::normalize_resource_host("localhost"),
      "localhost",
      "localhost without alias");

  return failures == 0 ? EXIT_SUCCESS : EXIT_FAILURE;
}
