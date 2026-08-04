#include "operator_runtime.hpp"
#include "cad_runtime.hpp"
#include "runtime_access.hpp"
#include "s3m_runtime.hpp"
#include "udbx_runtime.hpp"

#include <algorithm>
#include <cctype>
#include <chrono>
#include <cstdlib>
#include <set>
#include <stdexcept>
#include <utility>

namespace addp::supermap {
namespace {

using addp::workflow::Json;

const Json& required_json(const ResolvedParams& params, const std::string& name) {
  const auto value = params.find(name);
  if (value == params.end()) {
    throw std::invalid_argument("missing required parameter: " + name);
  }
  const auto* json = std::get_if<Json>(&value->second);
  if (json == nullptr) {
    throw std::invalid_argument("parameter must be a JSON value: " + name);
  }
  return *json;
}

std::string required_string(const ResolvedParams& params, const std::string& name) {
  const Json& value = required_json(params, name);
  if (!value.is_string() || value.get<std::string>().empty()) {
    throw std::invalid_argument("parameter must be a non-empty string: " + name);
  }
  return value.get<std::string>();
}

std::string optional_string(
    const ResolvedParams& params, const std::string& name, const std::string& fallback) {
  const auto value = params.find(name);
  if (value == params.end()) {
    return fallback;
  }
  const Json& json = required_json(params, name);
  if (!json.is_string()) {
    throw std::invalid_argument("parameter must be a string: " + name);
  }
  return json.get<std::string>();
}

bool optional_bool(const ResolvedParams& params, const std::string& name, bool fallback) {
  const auto value = params.find(name);
  if (value == params.end()) {
    return fallback;
  }
  const Json& json = required_json(params, name);
  if (!json.is_boolean()) {
    throw std::invalid_argument("parameter must be a boolean: " + name);
  }
  return json.get<bool>();
}

double required_double(const ResolvedParams& params, const std::string& name) {
  const Json& value = required_json(params, name);
  if (!value.is_number()) {
    throw std::invalid_argument("parameter must be a number: " + name);
  }
  return value.get<double>();
}

int required_int(const ResolvedParams& params, const std::string& name) {
  const Json& value = required_json(params, name);
  if (!value.is_number_integer()) {
    throw std::invalid_argument("parameter must be an integer: " + name);
  }
  return value.get<int>();
}

int optional_int(const ResolvedParams& params, const std::string& name, int fallback) {
  if (params.find(name) == params.end()) {
    return fallback;
  }
  return required_int(params, name);
}

std::vector<std::string> optional_string_array(
    const ResolvedParams& params, const std::string& name) {
  const auto value = params.find(name);
  if (value == params.end()) {
    return {};
  }
  const auto* json = std::get_if<Json>(&value->second);
  if (json == nullptr) {
    throw std::invalid_argument("parameter must be a string array: " + name);
  }
  std::vector<std::string> result;
  if (json->is_string()) {
    std::string text = json->get<std::string>();
    std::size_t start = 0;
    while (start <= text.size()) {
      const std::size_t separator = text.find(',', start);
      std::string item = text.substr(start, separator - start);
      const auto first = item.find_first_not_of(" \t\r\n");
      const auto last = item.find_last_not_of(" \t\r\n");
      if (first != std::string::npos) {
        result.push_back(item.substr(first, last - first + 1));
      }
      if (separator == std::string::npos) {
        break;
      }
      start = separator + 1;
    }
    return result;
  }
  if (!json->is_array()) {
    throw std::invalid_argument("parameter must be a string array: " + name);
  }
  for (const Json& item : *json) {
    if (!item.is_string()) {
      throw std::invalid_argument("parameter must be a string array: " + name);
    }
    const std::string text = item.get<std::string>();
    if (!text.empty()) {
      result.push_back(text);
    }
  }
  return result;
}

Json required_object(const ResolvedParams& params, const std::string& name) {
  const Json& value = required_json(params, name);
  if (!value.is_object()) {
    throw std::invalid_argument("parameter must be an object: " + name);
  }
  return value;
}

std::string object_string(
    const Json& object, const std::string& object_name, const std::string& field, bool required) {
  const auto value = object.find(field);
  if (value == object.end() || value->is_null()) {
    if (required) {
      throw std::invalid_argument(object_name + "." + field + " is required");
    }
    return "";
  }
  if (!value->is_string() || (required && value->get<std::string>().empty())) {
    throw std::invalid_argument(object_name + "." + field + " must be a non-empty string");
  }
  return value->get<std::string>();
}

std::string normalize_resource_host(std::string host) {
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

std::string postgis_server(const Json& connection_info) {
  const std::string host = normalize_resource_host(
      object_string(connection_info, "connection_info", "host", true));
  const std::string port = object_string(connection_info, "connection_info", "port", false);
  return port.empty() ? host : host + ":" + port;
}

std::string default_postgis_alias(const ResolvedParams& params) {
  const std::string schema = optional_string(params, "schema", "");
  const std::string table = optional_string(params, "table", "");
  if (!schema.empty() && !table.empty()) {
    return schema + "_" + table;
  }
  return table.empty() ? "postgis" : table;
}

template <typename Ref>
std::shared_ptr<Ref> required_ref(const ResolvedParams& params, const std::string& name) {
  const auto value = params.find(name);
  if (value == params.end()) {
    throw std::invalid_argument("missing required input: " + name);
  }
  const auto* reference = std::get_if<std::shared_ptr<Ref>>(&value->second);
  if (reference == nullptr || *reference == nullptr) {
    throw std::invalid_argument("input has incompatible runtime type: " + name);
  }
  return *reference;
}

bool is_reference(const Json& value) {
  return value.is_object() && value.contains("$ref");
}

Json resolve_nested_json(
    const Json& value, const std::unordered_map<std::string, RuntimeValue>& outputs) {
  if (is_reference(value)) {
    const std::string task_id = value.at("$ref").get<std::string>();
    const auto output = outputs.find(task_id);
    if (output == outputs.end()) {
      throw std::logic_error("workflow output is unavailable: " + task_id);
    }
    const auto* json = std::get_if<Json>(&output->second);
    if (json == nullptr) {
      throw std::invalid_argument(
          "SuperMap runtime object reference must occupy the complete parameter value");
    }
    return *json;
  }
  if (value.is_object()) {
    Json result = Json::object();
    for (const auto& [key, nested] : value.items()) {
      result[key] = resolve_nested_json(nested, outputs);
    }
    return result;
  }
  if (value.is_array()) {
    Json result = Json::array();
    for (const auto& nested : value) {
      result.push_back(resolve_nested_json(nested, outputs));
    }
    return result;
  }
  return value;
}

ResolvedParams resolve_params(
    const Json& params, const std::unordered_map<std::string, RuntimeValue>& outputs) {
  ResolvedParams result;
  for (const auto& [name, value] : params.items()) {
    if (is_reference(value)) {
      const std::string task_id = value.at("$ref").get<std::string>();
      const auto output = outputs.find(task_id);
      if (output == outputs.end()) {
        throw std::logic_error("workflow output is unavailable: " + task_id);
      }
      result.emplace(name, output->second);
    } else {
      result.emplace(name, resolve_nested_json(value, outputs));
    }
  }
  return result;
}

std::string storage_for(const std::string& operator_id) {
  static const std::set<std::string> datasource_storage = {
      "dataset.project",        "dataset.save",          "datasource.create",
      "datasource.enable_postgis", "datasource.open",       "datasource.open_postgis",
      "osgb_scene_to_s3m",     "overlay.clip",          "overlay.erase",
      "overlay.intersect",     "overlay.union",         "vector.buffer",
      "vector.dissolve",       "vector.feature_envelope", "vector.filter",
      "vector.inner_point",    "vector.merge",          "vector.spatial_filter",
  };
  return datasource_storage.find(operator_id) != datasource_storage.end() ? "datasource"
                                                                          : "memory";
}

std::string asset_ref_for(const Json& task) {
  const Json& params = task.at("params");
  if (params.contains("path") && params.at("path").is_string()) {
    return params.at("path").get<std::string>();
  }
  if (params.contains("output_path") && params.at("output_path").is_string()) {
    return params.at("output_path").get<std::string>();
  }
  const auto access_plan = params.find("access_plan");
  if (access_plan != params.end() && access_plan->is_object()) {
    return access_plan->value("target", Json::object())
        .value("access", Json::object())
        .value("path", "");
  }
  return "";
}

}  // namespace

OperatorRuntime::OperatorRuntime(addp::workflow::OperatorCatalog catalog)
    : catalog_(std::move(catalog)) {
  direct_handlers_.emplace("cad.inspect", inspect_cad);
  direct_handlers_.emplace("cad.render_preview", render_cad_preview);
  direct_handlers_.emplace("datasource.upgrade_udbx", upgrade_udbx);
  direct_handlers_.emplace("osgb_scene_to_s3m", convert_osgb_scene_to_s3m);
  handlers_.emplace(
      "osgb_scene_to_s3m",
      [](const ResolvedParams& params, ExecutionContext&) -> RuntimeValue {
        return convert_osgb_scene_to_s3m(
            Json{{"access_plan", required_object(params, "access_plan")}});
      });
  handlers_.emplace(
      "datasource.open",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.open_udbx(
            required_string(params, "path"),
            optional_string(params, "alias", ""),
            optional_bool(params, "read_only", true));
      });
  handlers_.emplace(
      "datasource.open_postgis",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        const Json connection_info = required_object(params, "connection_info");
        return context.open_postgis(
            postgis_server(connection_info),
            object_string(connection_info, "connection_info", "database", true),
            object_string(connection_info, "connection_info", "user", true),
            object_string(connection_info, "connection_info", "password", false),
            optional_string(params, "schema", ""),
            optional_string(params, "table", ""),
            optional_string(params, "alias", default_postgis_alias(params)),
            optional_bool(params, "read_only", true));
      });
  handlers_.emplace(
      "datasource.enable_postgis",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        const Json connection_info = required_object(params, "connection_info");
        return context.enable_postgis(
            postgis_server(connection_info),
            object_string(connection_info, "connection_info", "database", true),
            object_string(connection_info, "connection_info", "user", true),
            object_string(connection_info, "connection_info", "password", false),
            optional_string(params, "alias", "supermap_sdx"));
      });
  handlers_.emplace(
      "datasource.create",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        const Json connection_info = required_object(params, "connection_info");
        return context.create_udbx(
            resolve_udbx_path(connection_info, required_string(params, "path")).string(),
            optional_string(params, "alias", ""),
            optional_bool(params, "overwrite", false));
      });
  handlers_.emplace(
      "dataset.select",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.select_dataset(
            required_ref<DatasourceRef>(params, "datasource"),
            required_string(params, "dataset_name"));
      });
  handlers_.emplace(
      "dataset.info",
      [](const ResolvedParams& params, ExecutionContext&) -> RuntimeValue {
        return dataset_info(required_ref<DatasetRef>(params, "dataset"));
      });
  handlers_.emplace(
      "dataset.save",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.save_dataset(
            required_ref<DatasetRef>(params, "dataset"),
            required_ref<DatasourceRef>(params, "target_datasource"),
            required_string(params, "output_dataset_name"),
            optional_bool(params, "overwrite", false));
      });
  handlers_.emplace(
      "dataset.project",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.project_dataset(
            required_ref<DatasetRef>(params, "dataset"),
            required_ref<DatasourceRef>(params, "output_datasource"),
            required_string(params, "output_dataset_name"),
            required_int(params, "target_epsg"),
            optional_string(params, "method", "geocentric_translation"),
            optional_bool(params, "overwrite", false));
      });
  handlers_.emplace(
      "vector.filter",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.filter_dataset(
            required_ref<DatasetRef>(params, "dataset"),
            required_ref<DatasourceRef>(params, "output_datasource"),
            required_string(params, "output_dataset_name"),
            required_string(params, "attribute_filter"),
            optional_bool(params, "overwrite", false));
      });
  handlers_.emplace(
      "vector.merge",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.merge_datasets(
            required_ref<DatasetRef>(params, "primary_dataset"),
            required_ref<DatasetRef>(params, "append_dataset"),
            required_ref<DatasourceRef>(params, "output_datasource"),
            required_string(params, "output_dataset_name"),
            optional_bool(params, "overwrite", false));
      });
  handlers_.emplace(
      "vector.feature_envelope",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.feature_envelope(
            required_ref<DatasetRef>(params, "input_dataset"),
            required_ref<DatasourceRef>(params, "output_datasource"),
            required_string(params, "output_dataset_name"),
            optional_bool(params, "overwrite", false));
      });
  handlers_.emplace(
      "vector.inner_point",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.inner_point_dataset(
            required_ref<DatasetRef>(params, "input_dataset"),
            required_ref<DatasourceRef>(params, "output_datasource"),
            required_string(params, "output_dataset_name"),
            optional_bool(params, "overwrite", false));
      });
  handlers_.emplace(
      "vector.buffer",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.buffer_dataset(
            required_ref<DatasetRef>(params, "input_dataset"),
            required_ref<DatasourceRef>(params, "output_datasource"),
            required_string(params, "output_dataset_name"),
            required_double(params, "distance"),
            optional_string(params, "radius_unit", "meter"),
            optional_string(params, "end_type", "round"),
            optional_int(params, "semicircle_segments", 10),
            optional_bool(params, "dissolve", false),
            optional_bool(params, "keep_attributes", true),
            optional_bool(params, "overwrite", false));
      });
  handlers_.emplace(
      "vector.spatial_filter",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.spatial_filter_dataset(
            required_ref<DatasetRef>(params, "input_dataset"),
            required_ref<DatasetRef>(params, "filter_dataset"),
            required_ref<DatasourceRef>(params, "output_datasource"),
            required_string(params, "output_dataset_name"),
            required_string(params, "relation"),
            optional_bool(params, "overwrite", false));
      });
  handlers_.emplace(
      "vector.dissolve",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.dissolve_dataset(
            required_ref<DatasetRef>(params, "input_dataset"),
            required_ref<DatasourceRef>(params, "output_datasource"),
            required_string(params, "output_dataset_name"),
            optional_string_array(params, "field_names"),
            optional_string(params, "dissolve_type", "multipart"),
            params.find("tolerance") == params.end() ? 0.0
                                                       : required_double(params, "tolerance"),
            optional_bool(params, "save_all_fields", true),
            optional_bool(params, "overwrite", false));
      });
  for (const std::string operator_id : {
           "overlay.clip", "overlay.erase", "overlay.intersect", "overlay.union"}) {
    handlers_.emplace(
        operator_id,
        [operator_id](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
          return context.overlay_datasets(
              operator_id,
              required_ref<DatasetRef>(params, "input_dataset"),
              required_ref<DatasetRef>(params, "overlay_dataset"),
              required_ref<DatasourceRef>(params, "output_datasource"),
              required_string(params, "output_dataset_name"),
              params.find("tolerance") == params.end() ? 0.0
                                                         : required_double(params, "tolerance"),
              optional_bool(params, "overwrite", false));
        });
  }
  handlers_.emplace(
      "vector.query",
      [](const ResolvedParams& params, ExecutionContext& context) -> RuntimeValue {
        return context.query_dataset(
            required_ref<DatasetRef>(params, "dataset"),
            optional_string(params, "attribute_filter", ""));
      });
}

Json OperatorRuntime::execute_workflow(const std::string& execution_id, const Json& request) const {
  const auto started = std::chrono::steady_clock::now();
  const auto plan = addp::workflow::validate_and_plan(request, catalog_.by_id());
  ExecutionContext context;
  std::unordered_map<std::string, RuntimeValue> outputs;
  Json all_results = Json::object();
  Json lineage_events = Json::array();
  Json final_result;

  for (const Json& task : plan.tasks) {
    const std::string task_id = task.at("id").get<std::string>();
    const std::string operator_id = task.at("operator").get<std::string>();
    RuntimeValue output = execute_operator(
        operator_id, resolve_params(task.at("params"), outputs), context);
    final_result = summarize(output);
    all_results[task_id] = final_result;
    outputs.emplace(task_id, std::move(output));
    lineage_events.push_back({
        {"event_type", "workflow.operator.executed"},
        {"task_id", task_id},
        {"operator", operator_id},
        {"storage", storage_for(operator_id)},
        {"asset_ref", asset_ref_for(task)},
    });
  }

  const auto elapsed = std::chrono::duration<double, std::milli>(
      std::chrono::steady_clock::now() - started);
  return {
      {"status", "success"},
      {"execution_id", execution_id},
      {"final_result", std::move(final_result)},
      {"all_results", std::move(all_results)},
      {"execution_time_ms", elapsed.count()},
      {"lineage_events", std::move(lineage_events)},
  };
}

const addp::workflow::OperatorCatalog& OperatorRuntime::catalog() const { return catalog_; }

Json OperatorRuntime::invoke_direct(const std::string& id, const Json& params) const {
  if (!catalog_.supports_mode(id, "direct")) {
    throw std::invalid_argument("operator does not support direct execution: " + id);
  }
  const auto handler = direct_handlers_.find(id);
  if (handler == direct_handlers_.end()) {
    throw std::runtime_error("C++ direct operator is not implemented yet: " + id);
  }
  return handler->second(params);
}

RuntimeValue OperatorRuntime::execute_operator(
    const std::string& id, const ResolvedParams& params, ExecutionContext& context) const {
  const auto handler = handlers_.find(id);
  if (handler == handlers_.end()) {
    throw std::runtime_error("C++ operator is not implemented yet: " + id);
  }
  return handler->second(params, context);
}

}  // namespace addp::supermap
