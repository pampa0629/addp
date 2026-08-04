#include "udbx_runtime.hpp"

#include "runtime_access.hpp"
#include "supermap_runtime.hpp"

#define OS_ANDROID 1
#include "SQLiteCI/esqlite3/sqlite3.h"
#undef OS_ANDROID

#include <algorithm>
#include <array>
#include <filesystem>
#include <stdexcept>
#include <string>
#include <unistd.h>
#include <utility>
#include <vector>

namespace addp::supermap {
namespace {

using addp::workflow::Json;

constexpr std::array<const char*, 4> current_tables = {
    "SmAdditionalInfo", "SmAttributeRule", "SmGroupItems", "SmPyramidColumns"};
constexpr std::array<const char*, 3> current_register_columns = {
    "SmGroupID", "SmRelationship", "SmSubTypes"};

struct UdbxSchemaState {
  std::vector<std::string> missing_tables;
  std::vector<std::string> missing_register_columns;

  bool current() const noexcept {
    return missing_tables.empty() && missing_register_columns.empty();
  }
};

class SQLiteDatabase {
 public:
  explicit SQLiteDatabase(const std::filesystem::path& path) {
    const int result = sqlite3_open_v2(
        path.c_str(), &database_, SQLITE_OPEN_READONLY | SQLITE_OPEN_NOMUTEX, nullptr);
    if (result != SQLITE_OK) {
      const std::string detail = database_ == nullptr
          ? "SQLite error code " + std::to_string(result)
          : sqlite3_errmsg(database_);
      if (database_ != nullptr) {
        sqlite3_close(database_);
        database_ = nullptr;
      }
      throw std::invalid_argument("failed to inspect UDBX schema: " + detail);
    }
  }

  ~SQLiteDatabase() {
    if (database_ != nullptr) {
      sqlite3_close(database_);
    }
  }

  SQLiteDatabase(const SQLiteDatabase&) = delete;
  SQLiteDatabase& operator=(const SQLiteDatabase&) = delete;

  std::vector<std::string> query_names(const char* sql) const {
    sqlite3_stmt* statement = nullptr;
    const int prepared = sqlite3_prepare_v2(database_, sql, -1, &statement, nullptr);
    if (prepared != SQLITE_OK) {
      throw std::invalid_argument(
          "failed to inspect UDBX schema: " + std::string(sqlite3_errmsg(database_)));
    }
    std::vector<std::string> result;
    try {
      int step = SQLITE_OK;
      while ((step = sqlite3_step(statement)) == SQLITE_ROW) {
        const unsigned char* value = sqlite3_column_text(statement, 0);
        if (value != nullptr) {
          result.emplace_back(reinterpret_cast<const char*>(value));
        }
      }
      if (step != SQLITE_DONE) {
        throw std::invalid_argument(
            "failed to inspect UDBX schema: " + std::string(sqlite3_errmsg(database_)));
      }
      sqlite3_finalize(statement);
      return result;
    } catch (...) {
      sqlite3_finalize(statement);
      throw;
    }
  }

 private:
  sqlite3* database_ = nullptr;
};

template <std::size_t Size>
std::vector<std::string> missing_values(
    const std::array<const char*, Size>& required,
    const std::vector<std::string>& actual) {
  std::vector<std::string> result;
  for (const char* value : required) {
    if (std::find(actual.begin(), actual.end(), value) == actual.end()) {
      result.emplace_back(value);
    }
  }
  return result;
}

UdbxSchemaState inspect_udbx_schema(const std::filesystem::path& path) {
  if (!std::filesystem::is_regular_file(path)) {
    throw std::invalid_argument("UDBX file does not exist: " + path.string());
  }
  const SQLiteDatabase database(path);
  const std::vector<std::string> tables = database.query_names(
      "SELECT name FROM sqlite_master WHERE type='table' AND name IN "
      "('SmAdditionalInfo','SmAttributeRule','SmGroupItems','SmPyramidColumns')");
  const std::vector<std::string> register_columns = database.query_names(
      "SELECT name FROM pragma_table_info('SmRegister') WHERE name IN "
      "('SmGroupID','SmRelationship','SmSubTypes')");
  return {
      missing_values(current_tables, tables),
      missing_values(current_register_columns, register_columns),
  };
}

const Json& required_object(const Json& params, const std::string& name) {
  const auto value = params.find(name);
  if (value == params.end() || !value->is_object()) {
    throw std::invalid_argument("params." + name + " must be an object");
  }
  return *value;
}

std::string required_string(const Json& params, const std::string& name) {
  const auto value = params.find(name);
  if (value == params.end() || !value->is_string() || value->get<std::string>().empty()) {
    throw std::invalid_argument("params." + name + " is required");
  }
  return value->get<std::string>();
}

std::string optional_string(
    const Json& params, const std::string& name, const std::string& fallback) {
  const auto value = params.find(name);
  if (value == params.end() || value->is_null()) {
    return fallback;
  }
  if (!value->is_string()) {
    throw std::invalid_argument("params." + name + " must be a string");
  }
  const std::string result = value->get<std::string>();
  return result.empty() ? fallback : result;
}

}  // namespace

Json upgrade_udbx(const Json& params) {
  if (!params.is_object()) {
    throw std::invalid_argument("params must be an object");
  }
  const std::filesystem::path path = resolve_udbx_path(
      required_object(params, "connection_info"), required_string(params, "path"));
  const std::string alias = optional_string(params, "alias", path.filename().string());
  const UdbxSchemaState before = inspect_udbx_schema(path);
  if (!before.current() && ::access(path.c_str(), W_OK) != 0) {
    throw std::invalid_argument("UDBX file is not writable: " + path.string());
  }

  int dataset_count = 0;
  {
    ExecutionContext context;
    const std::shared_ptr<DatasourceRef> datasource =
        context.open_udbx(path.string(), alias, before.current());
    dataset_count = datasource->datasource->GetDatasetCount();
  }

  const UdbxSchemaState after = inspect_udbx_schema(path);
  if (!after.current()) {
    throw std::runtime_error(
        "UDBX schema upgrade did not produce the current schema; missing tables=" +
        Json(after.missing_tables).dump() + ", missing SmRegister columns=" +
        Json(after.missing_register_columns).dump());
  }
  return {
      {"kind", "supermap_udbx_upgrade"},
      {"path", path.string()},
      {"alias", alias},
      {"dataset_count", dataset_count},
      {"schema_current", true},
      {"changed", !before.current()},
  };
}

}  // namespace addp::supermap
