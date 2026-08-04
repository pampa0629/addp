#pragma once

#include "workflow.hpp"

#include <filesystem>
#include <string>

namespace addp::supermap {

std::filesystem::path resolve_udbx_path(
    const addp::workflow::Json& connection_info, const std::string& path);
std::filesystem::path resolve_workflow_mounted_path(
    const addp::workflow::Json& access);

class WorkflowAccessFile {
 public:
  WorkflowAccessFile(std::filesystem::path path, std::filesystem::path temporary_root = {});
  ~WorkflowAccessFile();

  WorkflowAccessFile(const WorkflowAccessFile&) = delete;
  WorkflowAccessFile& operator=(const WorkflowAccessFile&) = delete;
  WorkflowAccessFile(WorkflowAccessFile&& other) noexcept;
  WorkflowAccessFile& operator=(WorkflowAccessFile&& other) noexcept;

  const std::filesystem::path& path() const noexcept;

 private:
  std::filesystem::path path_;
  std::filesystem::path temporary_root_;
};

WorkflowAccessFile resolve_workflow_file(const addp::workflow::Json& access);
void publish_workflow_directory(
    const std::filesystem::path& source_root,
    const addp::workflow::Json& access,
    const std::string& write_mode);

}  // namespace addp::supermap
