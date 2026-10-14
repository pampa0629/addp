#pragma once

#include <algorithm>
#include <filesystem>
#include <stdexcept>
#include <string>
#include <vector>

namespace addp::supermap {

inline std::filesystem::path workflow_relative_file(std::string reference) {
  if (reference.empty() || reference.find('\0') != std::string::npos ||
      reference.find(':') != std::string::npos)
    throw std::invalid_argument("workflow file reference must be a relative file path");
  std::replace(reference.begin(), reference.end(), '\\', '/');
  const std::filesystem::path path(reference);
  if (path.is_absolute() || path.filename().empty())
    throw std::invalid_argument("workflow file reference must be a relative file path");
  for (const auto& part : path) {
    if (part == ".." || part == ".")
      throw std::invalid_argument("workflow file reference contains a parent or current directory segment");
  }
  return path;
}

inline bool workflow_path_within(
    const std::filesystem::path& root, const std::filesystem::path& file) {
  const auto relative = file.lexically_relative(root);
  if (relative.empty() || relative.is_absolute()) return false;
  for (const auto& part : relative) {
    if (part == "..") return false;
  }
  return relative != ".";
}

// The current SGM SDK resolves a missing flat reference under Textures/.
// Only the verified adjacent / Textures layout is accepted; do not guess other layouts.
inline std::vector<std::filesystem::path> sgm_texture_candidates(
    const std::filesystem::path& model_root, const std::string& sdk_reference) {
  const std::filesystem::path resolved(sdk_reference);
  if (!resolved.is_absolute() || !workflow_path_within(model_root, resolved))
    throw std::invalid_argument("SGM texture reference resolves outside the model directory");
  const auto relative = workflow_relative_file(resolved.lexically_relative(model_root).generic_string());
  if (!relative.parent_path().empty() && relative.parent_path() != "Textures")
    throw std::invalid_argument("SGM texture layout is not supported");
  return {relative.filename(), std::filesystem::path("Textures") / relative.filename()};
}

}  // namespace addp::supermap
