#include "runtime_bridge.h"

#include "operator_runtime.hpp"
#include "workflow.hpp"

#include "Toolkit/UGErrorObj.h"
#include "Toolkit/UGLicense.h"

#include <algorithm>
#include <array>
#include <chrono>
#include <cstdlib>
#include <cstring>
#include <ctime>
#include <filesystem>
#include <iomanip>
#include <memory>
#include <mutex>
#include <new>
#include <random>
#include <sstream>
#include <stdexcept>
#include <string>
#include <unordered_map>
#include <utility>
#include <vector>

namespace {

using addp::supermap::Json;

constexpr const char* service_name = "supermap-workflow-engine";
constexpr const char* service_version = "0.4.0";

Json failed(const std::string& code, const std::string& message) {
  return {{"status", "failed"}, {"error_code", code}, {"error", message}};
}

double elapsed_ms(std::chrono::steady_clock::time_point started) {
  return std::chrono::duration<double, std::milli>(
             std::chrono::steady_clock::now() - started)
      .count();
}

std::string utc_now() {
  const std::time_t now = std::time(nullptr);
  std::tm value {};
  gmtime_r(&now, &value);
  std::ostringstream result;
  result << std::put_time(&value, "%Y-%m-%dT%H:%M:%SZ");
  return result.str();
}

std::string new_execution_id() {
  std::array<unsigned char, 16> bytes {};
  std::random_device random;
  for (unsigned char& byte : bytes) {
    byte = static_cast<unsigned char>(random());
  }
  bytes[6] = static_cast<unsigned char>((bytes[6] & 0x0fU) | 0x40U);
  bytes[8] = static_cast<unsigned char>((bytes[8] & 0x3fU) | 0x80U);
  std::ostringstream result;
  result << std::hex << std::setfill('0');
  for (std::size_t index = 0; index < bytes.size(); ++index) {
    if (index == 4 || index == 6 || index == 8 || index == 10) {
      result << '-';
    }
    result << std::setw(2) << static_cast<unsigned int>(bytes[index]);
  }
  return result.str();
}

Json dependency(
    const std::string& path,
    const std::vector<std::string>& required,
    bool require_license = false) {
  Json missing = Json::array();
  const std::filesystem::path root = path;
  if (!std::filesystem::is_directory(root)) {
    missing.push_back("directory does not exist");
  } else {
    for (const std::string& name : required) {
      if (!std::filesystem::is_regular_file(root / name)) {
        missing.push_back(name);
      }
    }
    if (require_license) {
      bool found = false;
      for (const auto& entry : std::filesystem::directory_iterator(root)) {
        if (entry.is_regular_file() && entry.path().extension() == ".lic12") {
          found = true;
          break;
        }
      }
      if (!found) {
        missing.push_back("*.lic12");
      }
    }
  }
  return {
      {"path", path},
      {"available", missing.empty()},
      {"missing", std::move(missing)},
  };
}

Json parse_body(const char* body, std::size_t body_size) {
  if (body == nullptr || body_size == 0) {
    throw std::invalid_argument("request body must be a JSON object");
  }
  try {
    return Json::parse(body, body + body_size);
  } catch (const Json::parse_error& error) {
    throw std::invalid_argument(
        "request body is invalid JSON: " + std::string(error.what()));
  }
}

char* copy_string(const std::string& value) {
  char* result = static_cast<char*>(std::malloc(value.size() + 1));
  if (result == nullptr) {
    throw std::bad_alloc();
  }
  std::memcpy(result, value.data(), value.size());
  result[value.size()] = '\0';
  return result;
}

AddpRuntimeResponse response(int status, const Json& body) {
  const std::string serialized = body.dump();
  return {status, copy_string(serialized), serialized.size()};
}

AddpRuntimeResponse bridge_failure(const std::exception& error) noexcept {
  try {
    Json body = failed("EXECUTION_FAILED", "SuperMap runtime bridge failed");
    body["details"] = error.what();
    return response(500, body);
  } catch (...) {
    return {500, nullptr, 0};
  }
}

struct ExecutionRecord {
  std::string status;
  Json result;
  Json all_results;
  std::string error;
  std::string error_code;
  std::string details;
  std::string started_at;
  double execution_time_ms = 0.0;
  std::string message;
};

class RuntimeService final {
 public:
  RuntimeService(std::string operators_config, std::string sdk_root)
      : runtime_(addp::workflow::OperatorCatalog::load(operators_config)),
        sdk_root_(std::move(sdk_root)),
        started_at_(std::chrono::steady_clock::now()) {
    UGC::UGErrorObj::GetInstance().Startup();
    if (!UGC::UGLicense::VerifyLicense(UGLicense_iObjectsCppCore)) {
      throw std::runtime_error("SuperMap C++ Core license is unavailable");
    }
  }

  AddpRuntimeResponse health() const {
    const std::filesystem::path runtime_dir =
        std::filesystem::path(sdk_root_) / "bin" / "bin";
    Json dependencies = {
        {"iobjects_cpp",
         dependency(
             runtime_dir.string(),
             {"libSuEngine.so", "libSuCacheBuilder.so", "libSuGraphicsQT.so",
              "libsqlite328.so.0"},
             true)},
        {"freetype", dependency("/lib/aarch64-linux-gnu", {"libfreetype.so.6"})},
        {"nfs", dependency("/usr/sbin", {"mount.nfs"})},
    };
    bool healthy = true;
    for (const auto& item : dependencies.items()) {
      healthy = healthy && item.value().value("available", false);
    }
    return response(
        200,
        {
            {"status", healthy ? "healthy" : "degraded"},
            {"service", service_name},
            {"version", service_version},
            {"uptime",
             std::max<long long>(
                 0,
                 std::chrono::duration_cast<std::chrono::seconds>(
                     std::chrono::steady_clock::now() - started_at_)
                     .count())},
            {"operators_count", runtime_.catalog().descriptors().size()},
            {"dependencies", std::move(dependencies)},
        });
  }

  AddpRuntimeResponse operators(const std::string& category) const {
    Json operators = Json::array();
    for (const Json& descriptor : runtime_.catalog().descriptors()) {
      if (category.empty() || descriptor.value("category", "") == category) {
        operators.push_back(descriptor);
      }
    }
    return response(
        200,
        {{"status", "success"}, {"operators", operators}, {"count", operators.size()}});
  }

  AddpRuntimeResponse workflow(const char* body, std::size_t body_size) {
    const auto started = std::chrono::steady_clock::now();
    const std::string id = new_execution_id();
    const std::string started_at = utc_now();
    try {
      const Json request = parse_body(body, body_size);
      Json result;
      {
        const std::lock_guard<std::mutex> lock(supermap_mutex_);
        result = runtime_.execute_workflow(id, request);
      }
      result["execution_time_ms"] = elapsed_ms(started);
      store_execution(
          id,
          {
              "success",
              result.value("final_result", Json()),
              result.value("all_results", Json::object()),
              "",
              "",
              "",
              started_at,
              result["execution_time_ms"].get<double>(),
              "Execution completed",
          });
      return response(200, result);
    } catch (const addp::workflow::ValidationError& error) {
      return workflow_error(id, started_at, started, "WORKFLOW_INVALID", error.what(), 400);
    } catch (const std::invalid_argument& error) {
      return workflow_error(id, started_at, started, "WORKFLOW_INVALID", error.what(), 400);
    } catch (const std::exception& error) {
      Json result = failed("EXECUTION_FAILED", "SuperMap workflow execution failed");
      result["details"] = error.what();
      result["execution_id"] = id;
      result["execution_time_ms"] = elapsed_ms(started);
      store_execution(
          id,
          {"failed", Json(), Json::object(), result["error"].get<std::string>(),
           "EXECUTION_FAILED", error.what(), started_at,
           result["execution_time_ms"].get<double>(), "Execution failed"});
      return response(500, result);
    }
  }

  AddpRuntimeResponse invoke(
      const std::string& operator_name,
      const char* body,
      std::size_t body_size) {
    const auto& operators = runtime_.catalog().by_id();
    if (operators.find(operator_name) == operators.end()) {
      return response(
          404,
          failed("OPERATOR_NOT_FOUND", "Operator not found: " + operator_name));
    }
    if (!runtime_.catalog().supports_mode(operator_name, "direct")) {
      return response(
          403,
          failed(
              "DIRECT_NOT_SUPPORTED",
              "operator does not support direct execution: " + operator_name));
    }
    const auto started = std::chrono::steady_clock::now();
    try {
      const Json request = parse_body(body, body_size);
      if (!request.contains("params") || !request.at("params").is_object()) {
        throw std::invalid_argument("params must be an object");
      }
      Json result;
      {
        const std::lock_guard<std::mutex> lock(supermap_mutex_);
        result = runtime_.invoke_direct(operator_name, request.at("params"));
      }
      if (!result.is_object()) {
        throw std::runtime_error("direct operator result must be an object");
      }
      result["status"] = "success";
      result["execution_time_ms"] = elapsed_ms(started);
      return response(200, result);
    } catch (const std::invalid_argument& error) {
      Json result = failed("INVALID_PARAMS", error.what());
      result["execution_time_ms"] = elapsed_ms(started);
      return response(400, result);
    } catch (const std::exception& error) {
      Json result = failed(
          "EXECUTION_FAILED", "SuperMap direct operator execution failed");
      result["details"] = error.what();
      result["execution_time_ms"] = elapsed_ms(started);
      return response(500, result);
    }
  }

  AddpRuntimeResponse execution(const std::string& id) const {
    ExecutionRecord record;
    if (!load_execution(id, record)) {
      return response(404, failed("EXECUTION_NOT_FOUND", "Execution not found"));
    }
    Json result = {
        {"status", record.status},
        {"execution_id", id},
        {"result", record.result},
        {"all_results", record.all_results},
        {"progress", 100},
        {"started_at", record.started_at},
        {"execution_time_ms", record.execution_time_ms},
        {"message", record.message},
    };
    if (!record.error.empty()) {
      result["error"] = record.error;
      result["error_code"] = record.error_code;
      if (!record.details.empty()) {
        result["details"] = record.details;
      }
    }
    return response(200, result);
  }

 private:
  AddpRuntimeResponse workflow_error(
      const std::string& id,
      const std::string& started_at,
      std::chrono::steady_clock::time_point started,
      const std::string& code,
      const std::string& message,
      int status) {
    Json result = failed(code, message);
    result["execution_id"] = id;
    result["execution_time_ms"] = elapsed_ms(started);
    store_execution(
        id,
        {"failed", Json(), Json::object(), message, code, "", started_at,
         result["execution_time_ms"].get<double>(), "Execution failed"});
    return response(status, result);
  }

  void store_execution(const std::string& id, ExecutionRecord record) {
    const std::lock_guard<std::mutex> lock(executions_mutex_);
    executions_[id] = std::move(record);
  }

  bool load_execution(const std::string& id, ExecutionRecord& record) const {
    const std::lock_guard<std::mutex> lock(executions_mutex_);
    const auto found = executions_.find(id);
    if (found == executions_.end()) {
      return false;
    }
    record = found->second;
    return true;
  }

  addp::supermap::OperatorRuntime runtime_;
  std::string sdk_root_;
  std::chrono::steady_clock::time_point started_at_;
  mutable std::mutex executions_mutex_;
  std::unordered_map<std::string, ExecutionRecord> executions_;
  std::mutex supermap_mutex_;
};

}  // namespace

struct AddpSuperMapRuntime {
  explicit AddpSuperMapRuntime(std::string operators_config, std::string sdk_root)
      : service(std::move(operators_config), std::move(sdk_root)) {}

  RuntimeService service;
};

extern "C" AddpSuperMapRuntime* addp_supermap_runtime_create(
    const char* operators_config,
    const char* sdk_root,
    char** error_message) {
  if (error_message != nullptr) {
    *error_message = nullptr;
  }
  try {
    if (operators_config == nullptr || operators_config[0] == '\0') {
      throw std::invalid_argument("operators config path must not be empty");
    }
    if (sdk_root == nullptr || sdk_root[0] == '\0') {
      throw std::invalid_argument("SuperMap SDK root must not be empty");
    }
    return new AddpSuperMapRuntime(operators_config, sdk_root);
  } catch (const std::exception& error) {
    if (error_message != nullptr) {
      try {
        *error_message = copy_string(error.what());
      } catch (...) {
        *error_message = nullptr;
      }
    }
    return nullptr;
  }
}

extern "C" void addp_supermap_runtime_destroy(AddpSuperMapRuntime* runtime) {
  delete runtime;
}

extern "C" void addp_supermap_runtime_free_string(char* value) {
  std::free(value);
}

extern "C" void addp_supermap_runtime_free_response(AddpRuntimeResponse value) {
  std::free(value.body);
}

extern "C" AddpRuntimeResponse addp_supermap_runtime_health(
    AddpSuperMapRuntime* runtime) {
  try {
    if (runtime == nullptr) {
      throw std::invalid_argument("runtime is unavailable");
    }
    return runtime->service.health();
  } catch (const std::exception& error) {
    return bridge_failure(error);
  }
}

extern "C" AddpRuntimeResponse addp_supermap_runtime_operators(
    AddpSuperMapRuntime* runtime,
    const char* category) {
  try {
    if (runtime == nullptr) {
      throw std::invalid_argument("runtime is unavailable");
    }
    return runtime->service.operators(category == nullptr ? "" : category);
  } catch (const std::exception& error) {
    return bridge_failure(error);
  }
}

extern "C" AddpRuntimeResponse addp_supermap_runtime_workflow(
    AddpSuperMapRuntime* runtime,
    const char* body,
    size_t body_size) {
  try {
    if (runtime == nullptr) {
      throw std::invalid_argument("runtime is unavailable");
    }
    return runtime->service.workflow(body, body_size);
  } catch (const std::exception& error) {
    return bridge_failure(error);
  }
}

extern "C" AddpRuntimeResponse addp_supermap_runtime_invoke(
    AddpSuperMapRuntime* runtime,
    const char* operator_name,
    const char* body,
    size_t body_size) {
  try {
    if (runtime == nullptr) {
      throw std::invalid_argument("runtime is unavailable");
    }
    if (operator_name == nullptr || operator_name[0] == '\0') {
      throw std::invalid_argument("operator name must not be empty");
    }
    return runtime->service.invoke(operator_name, body, body_size);
  } catch (const std::exception& error) {
    return bridge_failure(error);
  }
}

extern "C" AddpRuntimeResponse addp_supermap_runtime_execution(
    AddpSuperMapRuntime* runtime,
    const char* execution_id) {
  try {
    if (runtime == nullptr) {
      throw std::invalid_argument("runtime is unavailable");
    }
    if (execution_id == nullptr || execution_id[0] == '\0') {
      throw std::invalid_argument("execution id must not be empty");
    }
    return runtime->service.execution(execution_id);
  } catch (const std::exception& error) {
    return bridge_failure(error);
  }
}
