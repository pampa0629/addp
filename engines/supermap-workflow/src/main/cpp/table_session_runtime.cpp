#include "table_session_runtime.hpp"

#include "resource_host.hpp"
#include "supermap_runtime.hpp"

#include "Base/OgdcVariant.h"
#include "Engine/UGDatasetVectorInfo.h"
#include "Engine/UGFieldsManager.h"
#include "Engine/UGMemRecordset.h"
#include "Engine/UGQueryDef.h"
#include "Element/OgdcFieldInfo.h"
#include "GeometryConverter/UGGeometryOGC.h"
#include "Stream/UGMemoryStream.h"
#include "Toolkit/UGErrorObj.h"
#include "Projection/UGPrjToolkits.h"

#include <algorithm>
#include <atomic>
#include <cctype>
#include <cstdint>
#include <iomanip>
#include <limits>
#include <memory>
#include <stdexcept>
#include <string>
#include <sstream>
#include <unordered_map>
#include <utility>
#include <vector>

namespace addp::supermap {
namespace {

constexpr const char* table_batch_protocol = "supermap.table-batch/v1";

struct TableConnection {
  std::string server;
  std::string database;
  std::string user;
  std::string password;
  std::string schema;
  std::string table;
};

std::string last_error_detail() {
  const auto error = UGC::UGErrorObj::GetInstance().GetLast(false);
  return "error_id=" + std::to_string(error.m_nID) + "; message=" +
      to_utf8(error.m_strMessage);
}

const Json& required_object(const Json& object, const char* name) {
  const auto found = object.find(name);
  if (found == object.end() || !found->is_object()) {
    throw std::invalid_argument(std::string(name) + " must be an object");
  }
  return *found;
}

const Json& required_array(const Json& object, const char* name) {
  const auto found = object.find(name);
  if (found == object.end() || !found->is_array()) {
    throw std::invalid_argument(std::string(name) + " must be an array");
  }
  return *found;
}

std::string required_string(const Json& object, const char* name) {
  const auto found = object.find(name);
  if (found == object.end() || !found->is_string()) {
    throw std::invalid_argument(std::string(name) + " must be a string");
  }
  const std::string value = found->get<std::string>();
  if (value.empty()) {
    throw std::invalid_argument(std::string(name) + " must not be empty");
  }
  return value;
}

std::string optional_string(
    const Json& object, const char* name, const std::string& fallback = "") {
  const auto found = object.find(name);
  if (found == object.end() || found->is_null()) {
    return fallback;
  }
  if (!found->is_string()) {
    throw std::invalid_argument(std::string(name) + " must be a string");
  }
  return found->get<std::string>();
}

bool optional_bool(const Json& object, const char* name, bool fallback = false) {
  const auto found = object.find(name);
  if (found == object.end() || found->is_null()) {
    return fallback;
  }
  if (!found->is_boolean()) {
    throw std::invalid_argument(std::string(name) + " must be a boolean");
  }
  return found->get<bool>();
}

int required_positive_int(const Json& object, const char* name) {
  const auto found = object.find(name);
  if (found == object.end() || !found->is_number_integer()) {
    throw std::invalid_argument(std::string(name) + " must be an integer");
  }
  const int value = found->get<int>();
  if (value <= 0) {
    throw std::invalid_argument(std::string(name) + " must be positive");
  }
  return value;
}

int optional_int(const Json& object, const char* name, int fallback = 0) {
  const auto found = object.find(name);
  if (found == object.end() || found->is_null()) {
    return fallback;
  }
  if (!found->is_number_integer()) {
    throw std::invalid_argument(std::string(name) + " must be an integer");
  }
  return found->get<int>();
}

void require_protocol(const Json& params) {
  if (required_string(params, "protocol") != table_batch_protocol) {
    throw std::invalid_argument("unsupported SuperMap table batch protocol");
  }
}

std::string connection_string(
    const Json& connection_info, const char* name, bool required) {
  const auto found = connection_info.find(name);
  if (found == connection_info.end() || found->is_null()) {
    if (required) {
      throw std::invalid_argument(std::string("connection_info.") + name + " is required");
    }
    return "";
  }
  if (!found->is_string()) {
    throw std::invalid_argument(std::string("connection_info.") + name + " must be a string");
  }
  const std::string value = found->get<std::string>();
  if (required && value.empty()) {
    throw std::invalid_argument(std::string("connection_info.") + name + " must not be empty");
  }
  return value;
}

int connection_port(const Json& connection_info) {
  const auto found = connection_info.find("port");
  if (found == connection_info.end() || found->is_null()) {
    return 5432;
  }
  if (!found->is_number_integer()) {
    throw std::invalid_argument("connection_info.port must be an integer");
  }
  return found->get<int>();
}

TableConnection parse_connection(const Json& params) {
  const Json& connection_info = required_object(params, "connection_info");
  const std::string host = normalize_resource_host(
      connection_string(connection_info, "host", true));
  const int port = connection_port(connection_info);
  if (port <= 0 || port > 65535) {
    throw std::invalid_argument("connection_info.port is invalid");
  }
  std::string schema = required_string(params, "schema");
  std::string normalized_schema = schema;
  std::transform(
      normalized_schema.begin(), normalized_schema.end(), normalized_schema.begin(),
      [](unsigned char character) { return static_cast<char>(std::tolower(character)); });
  if (normalized_schema != "sdx") {
    throw std::invalid_argument("SuperMap SDX+ for PostgreSQL table schema must be sdx");
  }
  return {
      host + ":" + std::to_string(port),
      connection_string(connection_info, "database", true),
      connection_string(connection_info, "user", true),
      connection_string(connection_info, "password", false),
      std::move(schema),
      required_string(params, "table"),
  };
}

std::string normalize_token(std::string value) {
  value.erase(
      std::remove_if(
          value.begin(), value.end(), [](unsigned char character) {
            return character == '_' || character == '-' || std::isspace(character);
          }),
      value.end());
  std::transform(value.begin(), value.end(), value.begin(), [](unsigned char character) {
    return static_cast<char>(std::tolower(character));
  });
  return value;
}

bool same_postgresql_identifier(const std::string& left, const std::string& right) {
  if (left.size() != right.size()) {
    return false;
  }
  for (std::size_t index = 0; index < left.size(); ++index) {
    const auto left_character = static_cast<unsigned char>(left[index]);
    const auto right_character = static_cast<unsigned char>(right[index]);
    if (std::tolower(left_character) != std::tolower(right_character)) {
      return false;
    }
  }
  return true;
}

const Json* find_row_value(const Json& row, const std::string& field_name) {
  const auto exact = row.find(field_name);
  if (exact != row.end()) {
    return &exact.value();
  }
  for (auto field = row.begin(); field != row.end(); ++field) {
    if (same_postgresql_identifier(field.key(), field_name)) {
      return &field.value();
    }
  }
  return nullptr;
}

bool reserved_supermap_field(const std::string& name) {
  return name.size() >= 2 && std::tolower(static_cast<unsigned char>(name[0])) == 's' &&
      std::tolower(static_cast<unsigned char>(name[1])) == 'm';
}

std::string base64_encode(const unsigned char* data, std::size_t size) {
  static constexpr char alphabet[] =
      "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  std::string result;
  result.reserve(((size + 2) / 3) * 4);
  for (std::size_t index = 0; index < size; index += 3) {
    const std::uint32_t first = data[index];
    const std::uint32_t second = index + 1 < size ? data[index + 1] : 0;
    const std::uint32_t third = index + 2 < size ? data[index + 2] : 0;
    const std::uint32_t value = (first << 16U) | (second << 8U) | third;
    result.push_back(alphabet[(value >> 18U) & 0x3fU]);
    result.push_back(alphabet[(value >> 12U) & 0x3fU]);
    result.push_back(index + 1 < size ? alphabet[(value >> 6U) & 0x3fU] : '=');
    result.push_back(index + 2 < size ? alphabet[value & 0x3fU] : '=');
  }
  return result;
}

std::string stable_crs_id(const std::string& definition) {
  std::uint64_t hash = 1469598103934665603ULL;
  for (unsigned char character : definition) {
    hash ^= character;
    hash *= 1099511628211ULL;
  }
  std::ostringstream stream;
  stream << "ADDP:CRS:" << std::hex << std::setfill('0') << std::setw(16) << hash;
  return stream.str();
}

Json spatial_projection_facts(const UGC::UGPrjCoordSys& projection) {
  const int epsg = projection.GetEPSGCode();
  UGC::UGWKT wkt;
  std::string definition;
  std::string encoding;
  if ((projection >> wkt) && !wkt.wkt.IsEmpty()) {
    definition = to_utf8(wkt.wkt);
    encoding = "wkt";
  } else {
    const auto proj4 = projection.GetPrjParams().GetPrjParamString();
    if (!proj4.IsEmpty()) {
      definition = to_utf8(proj4);
      encoding = "proj4";
    }
  }
  if (definition.empty()) {
    return Json::object();
  }
  const std::string id = epsg > 0 ? "EPSG:" + std::to_string(epsg) : stable_crs_id(definition);
  return {
      {"crs_ref", id},
      {"crs_definitions", Json::array({{
          {"id", id},
          {"definition_encoding", encoding},
          {"definition", definition},
          {"source", "supermap_runtime_sdk"},
      }})},
  };
}

std::vector<unsigned char> base64_decode(const std::string& value) {
  static const std::string alphabet =
      "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  if (value.size() % 4 != 0) {
    throw std::invalid_argument("base64 value has an invalid length");
  }
  std::vector<unsigned char> result;
  result.reserve((value.size() / 4) * 3);
  for (std::size_t index = 0; index < value.size(); index += 4) {
    std::uint32_t decoded = 0;
    int padding = 0;
    for (std::size_t offset = 0; offset < 4; ++offset) {
      const char character = value[index + offset];
      if (character == '=') {
        ++padding;
        decoded <<= 6U;
        continue;
      }
      if (padding != 0) {
        throw std::invalid_argument("base64 padding is invalid");
      }
      const std::size_t position = alphabet.find(character);
      if (position == std::string::npos) {
        throw std::invalid_argument("base64 value contains an invalid character");
      }
      decoded = (decoded << 6U) | static_cast<std::uint32_t>(position);
    }
    result.push_back(static_cast<unsigned char>((decoded >> 16U) & 0xffU));
    if (padding < 2) {
      result.push_back(static_cast<unsigned char>((decoded >> 8U) & 0xffU));
    }
    if (padding == 0) {
      result.push_back(static_cast<unsigned char>(decoded & 0xffU));
    }
  }
  return result;
}

std::string addp_field_type(OGDC::OgdcFieldInfo::FieldType type) {
  switch (type) {
    case OGDC::OgdcFieldInfo::Boolean:
      return "bool";
    case OGDC::OgdcFieldInfo::Byte:
    case OGDC::OgdcFieldInfo::INT16:
    case OGDC::OgdcFieldInfo::INT32:
      return "int";
    case OGDC::OgdcFieldInfo::INT64:
      return "bigint";
    case OGDC::OgdcFieldInfo::Float:
      return "float";
    case OGDC::OgdcFieldInfo::Double:
      return "double";
    case OGDC::OgdcFieldInfo::Date:
      return "date";
    case OGDC::OgdcFieldInfo::Time:
      return "time";
    case OGDC::OgdcFieldInfo::TimeStamp:
      return "timestamp";
    case OGDC::OgdcFieldInfo::Binary:
    case OGDC::OgdcFieldInfo::LongBinary:
      return "bytes";
    case OGDC::OgdcFieldInfo::Geometry:
      return "geometry";
    case OGDC::OgdcFieldInfo::JsonB:
      return "json";
    case OGDC::OgdcFieldInfo::Text:
    case OGDC::OgdcFieldInfo::Char:
    case OGDC::OgdcFieldInfo::NText:
      return "string";
    default:
      throw std::invalid_argument(
          "unsupported SuperMap field type: " + std::to_string(static_cast<int>(type)));
  }
}

OGDC::OgdcFieldInfo::FieldType supermap_field_type(const std::string& type) {
  const std::string normalized = normalize_token(type);
  if (normalized == "bool" || normalized == "boolean") {
    return OGDC::OgdcFieldInfo::Boolean;
  }
  if (normalized == "int" || normalized == "integer") {
    return OGDC::OgdcFieldInfo::INT32;
  }
  if (normalized == "bigint") {
    return OGDC::OgdcFieldInfo::INT64;
  }
  if (normalized == "float") {
    return OGDC::OgdcFieldInfo::Float;
  }
  if (normalized == "double") {
    return OGDC::OgdcFieldInfo::Double;
  }
  if (normalized == "decimal") {
    // SuperMap has no decimal field; use its native 64-bit floating-point field.
    return OGDC::OgdcFieldInfo::Double;
  }
  if (normalized == "string" || normalized == "uuid") {
    return OGDC::OgdcFieldInfo::NText;
  }
  if (normalized == "bytes") {
    return OGDC::OgdcFieldInfo::LongBinary;
  }
  if (normalized == "date") {
    return OGDC::OgdcFieldInfo::Date;
  }
  if (normalized == "time") {
    return OGDC::OgdcFieldInfo::Time;
  }
  if (normalized == "timestamp") {
    return OGDC::OgdcFieldInfo::TimeStamp;
  }
  if (normalized == "json") {
    return OGDC::OgdcFieldInfo::JsonB;
  }
  throw std::invalid_argument("unsupported ADDP field type for SuperMap: " + type);
}

UGC::UGDataset::DatasetType dataset_type_for_geometry(std::string geometry_type) {
  geometry_type = normalize_token(std::move(geometry_type));
  if (geometry_type == "point" || geometry_type == "multipoint") {
    return UGC::UGDataset::Point;
  }
  if (geometry_type == "line" || geometry_type == "linestring" ||
      geometry_type == "multilinestring") {
    return UGC::UGDataset::Line;
  }
  if (geometry_type == "polygon" || geometry_type == "multipolygon" ||
      geometry_type == "region") {
    return UGC::UGDataset::Region;
  }
  if (geometry_type == "geometry" || geometry_type == "geometrycollection") {
    return UGC::UGDataset::CAD;
  }
  throw std::invalid_argument("unsupported SuperMap geometry type: " + geometry_type);
}

std::string geometry_type_for_dataset(UGC::UGDataset::DatasetType type) {
  switch (type) {
    case UGC::UGDataset::Point:
      return "point";
    case UGC::UGDataset::Line:
    case UGC::UGDataset::Region:
    case UGC::UGDataset::CAD:
      return "geometry";
    default:
      throw std::invalid_argument("dataset is not a supported spatial vector dataset");
  }
}

std::string primary_geometry_field(const Json& spatial) {
  std::string primary = optional_string(spatial, "primary_geometry_column");
  const auto columns = spatial.find("geometry_columns");
  if (!primary.empty()) {
    return primary;
  }
  if (columns != spatial.end() && columns->is_array() && !columns->empty()) {
    return required_string(columns->front(), "name");
  }
  throw std::invalid_argument("spatial primary geometry column is required");
}

Json primary_geometry_column(const Json& spatial) {
  const std::string primary = primary_geometry_field(spatial);
  const Json& columns = required_array(spatial, "geometry_columns");
  for (const Json& column : columns) {
    if (required_string(column, "name") == primary) {
      return column;
    }
  }
  throw std::invalid_argument("primary geometry column is not declared in spatial geometry_columns");
}

Json variant_to_json(const UGC::UGVariant& value) {
  switch (value.GetType()) {
    case UGC::UGVariant::Null:
      return nullptr;
    case UGC::UGVariant::Boolean:
      return value.ToBoolean();
    case UGC::UGVariant::Byte:
    case UGC::UGVariant::Short:
    case UGC::UGVariant::Integer:
      return value.ToInt();
    case UGC::UGVariant::Long:
      return value.ToLong();
    case UGC::UGVariant::Float:
    case UGC::UGVariant::Double:
      return value.ToDouble();
    case UGC::UGVariant::Binary: {
      const auto& binary = value.GetValue().binVal;
      return base64_encode(
          static_cast<const unsigned char*>(binary.pVal), binary.nSize);
    }
    case UGC::UGVariant::String:
    case UGC::UGVariant::Date:
    case UGC::UGVariant::Time:
    case UGC::UGVariant::TimeStamp:
      return to_utf8(value.ToString());
    default:
      throw std::runtime_error("unsupported SuperMap variant type");
  }
}

UGC::UGVariant json_to_variant(
    const Json& value, OGDC::OgdcFieldInfo::FieldType field_type) {
  UGC::UGVariant result;
  if (value.is_null()) {
    result.SetNull();
    return result;
  }
  switch (field_type) {
    case OGDC::OgdcFieldInfo::Boolean:
      if (!value.is_boolean()) {
        throw std::invalid_argument("boolean field value must be boolean");
      }
      return UGC::UGVariant(value.get<bool>());
    case OGDC::OgdcFieldInfo::Byte:
    case OGDC::OgdcFieldInfo::INT16:
    case OGDC::OgdcFieldInfo::INT32:
      if (!value.is_number_integer()) {
        throw std::invalid_argument("integer field value must be an integer");
      }
      return UGC::UGVariant(static_cast<UGC::UGint>(value.get<std::int64_t>()));
    case OGDC::OgdcFieldInfo::INT64:
      if (!value.is_number_integer()) {
        throw std::invalid_argument("bigint field value must be an integer");
      }
      return UGC::UGVariant(static_cast<UGC::UGlong>(value.get<std::int64_t>()));
    case OGDC::OgdcFieldInfo::Float:
      if (!value.is_number()) {
        throw std::invalid_argument("float field value must be numeric");
      }
      return UGC::UGVariant(static_cast<UGC::UGfloat>(value.get<double>()));
    case OGDC::OgdcFieldInfo::Double:
      if (value.is_number()) {
        return UGC::UGVariant(static_cast<UGC::UGdouble>(value.get<double>()));
      }
      if (value.is_string()) {
        std::size_t parsed = 0;
        const double converted = std::stod(value.get<std::string>(), &parsed);
        if (parsed != value.get<std::string>().size()) {
          throw std::invalid_argument("double field value must be numeric");
        }
        return UGC::UGVariant(static_cast<UGC::UGdouble>(converted));
      }
      throw std::invalid_argument("double field value must be numeric");
    case OGDC::OgdcFieldInfo::Binary:
    case OGDC::OgdcFieldInfo::LongBinary: {
      if (!value.is_string()) {
        throw std::invalid_argument("binary field value must be base64 text");
      }
      const std::vector<unsigned char> bytes = base64_decode(value.get<std::string>());
      return UGC::UGVariant(bytes.data(), static_cast<UGC::UGint>(bytes.size()));
    }
    case OGDC::OgdcFieldInfo::Text:
    case OGDC::OgdcFieldInfo::Char:
    case OGDC::OgdcFieldInfo::NText:
      return UGC::UGVariant(to_ug_string(value.is_string() ? value.get<std::string>() : value.dump()));
    case OGDC::OgdcFieldInfo::JsonB:
      return UGC::UGVariant(to_ug_string(value.is_string() ? value.get<std::string>() : value.dump()));
    case OGDC::OgdcFieldInfo::Date:
      return UGC::UGVariant::FromString(
          to_ug_string(required_string(Json{{"value", value}}, "value")),
          UGC::UGVariant::Date);
    case OGDC::OgdcFieldInfo::Time:
      return UGC::UGVariant::FromString(
          to_ug_string(required_string(Json{{"value", value}}, "value")),
          UGC::UGVariant::Time);
    case OGDC::OgdcFieldInfo::TimeStamp:
      return UGC::UGVariant::FromString(
          to_ug_string(required_string(Json{{"value", value}}, "value")),
          UGC::UGVariant::TimeStamp);
    default:
      throw std::invalid_argument("unsupported target SuperMap field type");
  }
}

struct OpenedTable {
  std::unique_ptr<ExecutionContext> context;
  std::shared_ptr<DatasourceRef> datasource;
  std::shared_ptr<DatasetRef> dataset;
};

OpenedTable open_table(const TableConnection& connection, bool read_only) {
  auto context = std::make_unique<ExecutionContext>();
  auto datasource = context->open_postgresql(
      connection.server,
      connection.database,
      connection.user,
      connection.password,
      connection.schema,
      connection.table,
      "supermap_table_session",
      read_only);
  auto dataset = context->select_dataset(datasource, connection.table);
  return {std::move(context), std::move(datasource), std::move(dataset)};
}

std::string next_session_id(const char* prefix) {
  static std::atomic<unsigned long long> counter {0};
  return std::string(prefix) + "-" + std::to_string(++counter);
}

struct ReadSession {
  OpenedTable opened;
  UGC::UGRecordsetPtr recordset;
  UGC::UGFieldInfos fields;
  Json exported_fields = Json::array();
  Json spatial;
  std::string geometry_field;
  std::int64_t offset = 0;
};

struct WriteSession {
  OpenedTable opened;
  UGC::UGFieldInfos fields;
  Json declared_fields = Json::array();
  Json spatial;
  std::string geometry_field;
  bool replace = false;
  std::int64_t expected_count = 0;
  std::int64_t written_count = 0;
  std::int64_t geometry_count = 0;
};

Json dataset_bounds(const UGC::UGDatasetVectorPtr& dataset) {
  const UGC::UGRect2D& bounds = dataset->GetBounds();
  if (bounds.IsEmpty()) {
    return nullptr;
  }
  return Json::array({bounds.left, bounds.bottom, bounds.right, bounds.top});
}

}  // namespace

class TableSessionRuntime::Impl final {
 public:
  Json invoke(const std::string& operator_id, const Json& params) {
    if (operator_id == "table.delete") {
      return delete_table(params);
    }
    if (operator_id == "table.read_open") {
      return read_open(params);
    }
    if (operator_id == "table.read_batch") {
      return read_batch(params);
    }
    if (operator_id == "table.read_close") {
      return read_close(params);
    }
    if (operator_id == "table.write_prepare") {
      return write_prepare(params);
    }
    if (operator_id == "table.write_open") {
      return write_open(params);
    }
    if (operator_id == "table.write_batch") {
      return write_batch(params);
    }
    if (operator_id == "table.write_close") {
      return write_close(params);
    }
    if (operator_id == "table.write_abort") {
      return write_abort(params);
    }
    throw std::invalid_argument("unknown SuperMap table operator: " + operator_id);
  }

 private:
  Json delete_table(const Json& params) {
    const TableConnection connection = parse_connection(params);
    auto context = std::make_unique<ExecutionContext>();
    auto datasource = context->open_postgresql(
        connection.server,
        connection.database,
        connection.user,
        connection.password,
        connection.schema,
        "",
        "supermap_table_delete",
        false);
    const UGC::UGString name = to_ug_string(connection.table);
    if (datasource->datasource->GetDataset(name) == nullptr) {
      return {{"deleted", false}};
    }
    if (!datasource->datasource->DeleteDataset(name)) {
      throw std::runtime_error("failed to delete SuperMap dataset; " + last_error_detail());
    }
    return {{"deleted", true}};
  }

  Json read_open(const Json& params) {
    require_protocol(params);
    const TableConnection connection = parse_connection(params);
    ReadSession session;
    session.opened = open_table(connection, true);
    if (!session.opened.dataset->dataset->GetFieldInfos(session.fields)) {
      throw std::runtime_error("failed to read SuperMap fields; " + last_error_detail());
    }

    session.geometry_field = "SmGeometry";
    for (std::size_t index = 0; index < session.fields.GetSize(); ++index) {
      OGDC::OgdcFieldInfo field = session.fields[index];
      if (field.IsGeoField()) {
        continue;
      }
      if (field.IsSystemField() || reserved_supermap_field(to_utf8(field.m_strName))) {
        continue;
      }
      session.exported_fields.push_back({
          {"name", to_utf8(field.m_strName)},
          {"type", addp_field_type(field.m_nType)},
          {"native_type", std::to_string(static_cast<int>(field.m_nType))},
          {"nullable", !static_cast<bool>(field.m_bRequired)},
          {"size", field.m_nSize},
          {"precision", field.m_nPrecision},
          {"scale", field.m_nScale},
      });
    }
    session.exported_fields.push_back({
        {"name", session.geometry_field},
        {"type", "geometry"},
        {"native_type", "SuperMap Geometry"},
        {"nullable", true},
    });

    const UGC::UGPrjCoordSys& projection = session.opened.dataset->dataset->GetPrjCoordSys();
    const int srid = projection.GetEPSGCode();
    Json geometry_column = {
        {"name", session.geometry_field},
        {"geometry_type", geometry_type_for_dataset(session.opened.dataset->dataset->GetType())},
        {"dimension", 2},
        {"nullable", true},
    };
    if (srid > 0) {
      geometry_column["srid"] = srid;
    }
    session.spatial = {
        {"primary_geometry_column", session.geometry_field},
        {"geometry_columns", Json::array({geometry_column})},
        {"has_spatial_index", !session.opened.dataset->dataset->IsSpatialIndexDirty()},
    };
    if (srid > 0) {
      session.spatial["srid"] = srid;
    }
    const Json projectionFacts = spatial_projection_facts(projection);
    if (!projectionFacts.empty()) {
      if (projectionFacts.contains("crs_ref")) {
        session.spatial["crs_ref"] = projectionFacts["crs_ref"];
      }
      if (projectionFacts.contains("crs_definitions")) {
        session.spatial["crs_definitions"] = projectionFacts["crs_definitions"];
      }
    }

    UGC::UGQueryDef query;
    query.m_nCursorType = UGC::UGQueryDef::OpenStatic;
    query.m_strFilter = to_ug_string(optional_string(params, "query"));
    session.recordset = session.opened.dataset->dataset->Query(query);
    if (session.recordset == nullptr || !session.recordset->MoveFirst()) {
      if (session.opened.dataset->dataset->GetObjectCount() != 0) {
        throw std::runtime_error("failed to open SuperMap read recordset; " + last_error_detail());
      }
    }

    const std::string session_id = next_session_id("read");
    const Json fields = session.exported_fields;
    const Json spatial = session.spatial;
    const int row_count = session.recordset == nullptr ? 0 : session.recordset->GetRecordCount();
    read_sessions_.emplace(session_id, std::move(session));
    return {
        {"session_id", session_id},
        {"protocol", table_batch_protocol},
        {"fields", fields},
        {"spatial", spatial},
        {"row_count", row_count},
    };
  }

  Json read_batch(const Json& params) {
    require_protocol(params);
    const std::string session_id = required_string(params, "session_id");
    const int limit = required_positive_int(params, "limit");
    ReadSession& session = require_read_session(session_id);
    Json rows = Json::array();
    const int srid = session.opened.dataset->dataset->GetPrjCoordSys().GetEPSGCode();
    while (session.recordset != nullptr && !session.recordset->IsEOF() &&
           static_cast<int>(rows.size()) < limit) {
      Json row = Json::object();
      for (std::size_t index = 0; index < session.fields.GetSize(); ++index) {
        OGDC::OgdcFieldInfo field = session.fields[index];
        if (field.IsSystemField() || field.IsGeoField() ||
            reserved_supermap_field(to_utf8(field.m_strName))) {
          continue;
        }
        UGC::UGVariant value;
        if (!session.recordset->GetFieldValue(field.m_strName, value)) {
          throw std::runtime_error(
              "failed to read SuperMap field " + to_utf8(field.m_strName) + "; " +
              last_error_detail());
        }
        row[to_utf8(field.m_strName)] = variant_to_json(value);
      }

      UGC::UGGeometry* raw_geometry = nullptr;
      if (session.recordset->GetGeometry(raw_geometry) && raw_geometry != nullptr) {
        std::unique_ptr<UGC::UGGeometry> geometry(raw_geometry);
        std::unique_ptr<UGC::UGMemoryStream> wkb(
            UGC::UGGeometryOGC::UGGeometryToWKB(
                geometry.get(), static_cast<UGC::UGshort>(-1), srid, false));
        if (wkb == nullptr || wkb->GetData() == nullptr) {
          throw std::runtime_error("failed to encode SuperMap geometry as EWKB; " + last_error_detail());
        }
        row[session.geometry_field] = base64_encode(wkb->GetData(), wkb->GetLength());
      } else {
        row[session.geometry_field] = nullptr;
      }
      rows.push_back(std::move(row));
      session.recordset->MoveNext();
    }

    Json batch = {
        {"fields", session.exported_fields},
        {"spatial", session.spatial},
        {"rows", std::move(rows)},
        {"offset", session.offset},
    };
    session.offset += static_cast<std::int64_t>(batch["rows"].size());
    return {{"batch", std::move(batch)}};
  }

  Json read_close(const Json& params) {
    const std::string session_id = required_string(params, "session_id");
    const auto found = read_sessions_.find(session_id);
    if (found == read_sessions_.end()) {
      return {{"closed", false}};
    }
    read_sessions_.erase(found);
    return {{"closed", true}};
  }

  Json write_prepare(const Json& params) {
    require_protocol(params);
    const TableConnection connection = parse_connection(params);
    const Json& fields = required_array(params, "fields");
    const Json& spatial = required_object(params, "spatial");
    const Json geometry_column = primary_geometry_column(spatial);
    const std::string geometry_type = required_string(geometry_column, "geometry_type");

    auto context = std::make_unique<ExecutionContext>();
    auto datasource = context->open_postgresql(
        connection.server,
        connection.database,
        connection.user,
        connection.password,
        connection.schema,
        "",
        "supermap_table_prepare",
        false);
    const UGC::UGString dataset_name = to_ug_string(connection.table);
    if (datasource->datasource->GetDataset(dataset_name) != nullptr) {
      auto existing = context->select_dataset(datasource, connection.table);
      return {
          {"created", false},
          {"record_count", existing->dataset->GetObjectCount()},
      };
    }

    UGC::UGDatasetVectorInfo info;
    info.m_strName = dataset_name;
    info.m_nType = dataset_type_for_geometry(geometry_type);
    const int dimension = optional_int(geometry_column, "dimension", 2);
    info.SetZFlag(dimension >= 3);

    UGC::UGDatasetVectorPtr dataset;
    const int srid = optional_int(spatial, "srid", optional_int(geometry_column, "srid", 0));
    if (srid > 0) {
      UGC::UGPrjCoordSys projection;
      if (!projection.FromEpsgCode(srid) || !projection.IsValid()) {
        throw std::invalid_argument("unsupported target EPSG code: " + std::to_string(srid));
      }
      dataset = datasource->datasource->CreateDatasetVector(info, projection);
    } else {
      dataset = datasource->datasource->CreateDatasetVector(info);
    }
    if (dataset == nullptr) {
      throw std::runtime_error("failed to create SuperMap dataset; " + last_error_detail());
    }

    try {
      UGC::UGFieldInfos target_fields;
      const std::string geometry_field = primary_geometry_field(spatial);
      for (const Json& field : fields) {
        const std::string name = required_string(field, "name");
        const std::string type = required_string(field, "type");
        if (name == geometry_field || normalize_token(type) == "geometry" ||
            reserved_supermap_field(name)) {
          continue;
        }
        const int size = std::max(1, optional_int(field, "size", 255));
        const bool nullable = optional_bool(field, "nullable", false);
        if (!target_fields.AddField(
                to_ug_string(name), supermap_field_type(type), size, 0, !nullable)) {
          throw std::invalid_argument("invalid SuperMap target field: " + name);
        }
      }
      if (target_fields.GetSize() > 0) {
        UGC::UGFieldsManager* manager = dataset->GetFieldsManager();
        if (manager == nullptr || !manager->CreateFields(target_fields)) {
          throw std::runtime_error("failed to create SuperMap user fields; " + last_error_detail());
        }
      }
      if (!dataset->SaveInfo()) {
        throw std::runtime_error("failed to save SuperMap dataset metadata; " + last_error_detail());
      }
    } catch (...) {
      dataset.reset();
      datasource->datasource->DeleteDataset(dataset_name);
      throw;
    }
    return {{"created", true}, {"record_count", 0}};
  }

  Json write_open(const Json& params) {
    require_protocol(params);
    const TableConnection connection = parse_connection(params);
    WriteSession session;
    session.opened = open_table(connection, false);
    session.declared_fields = required_array(params, "fields");
    session.spatial = required_object(params, "spatial");
    session.geometry_field = primary_geometry_field(session.spatial);
    session.replace = optional_bool(params, "replace", false);
    session.expected_count = session.opened.dataset->dataset->GetObjectCount();
    if (!session.opened.dataset->dataset->GetFieldInfos(session.fields)) {
      throw std::runtime_error("failed to read target SuperMap fields; " + last_error_detail());
    }
    const std::string session_id = next_session_id("write");
    const std::int64_t initial_count = session.expected_count;
    write_sessions_.emplace(session_id, std::move(session));
    return {
        {"session_id", session_id},
        {"protocol", table_batch_protocol},
        {"initial_count", initial_count},
    };
  }

  Json write_batch(const Json& params) {
    require_protocol(params);
    const std::string session_id = required_string(params, "session_id");
    WriteSession& session = require_write_session(session_id);
    const Json& batch = required_object(params, "batch");
    const Json& rows = required_array(batch, "rows");
    if (rows.empty()) {
      return {{"written", 0}, {"total_written", session.written_count}};
    }

    auto memory = std::make_shared<UGC::UGMemRecordset>(
        session.fields, session.opened.dataset->dataset);
    for (std::size_t row_index = 0; row_index < rows.size(); ++row_index) {
      const Json& row = rows[row_index];
      if (!row.is_object()) {
        throw std::invalid_argument("SuperMap batch row must be an object");
      }
      std::unique_ptr<UGC::UGGeometry> geometry;
      const Json* geometry_value = find_row_value(row, session.geometry_field);
      if (geometry_value != nullptr && !geometry_value->is_null()) {
        if (!geometry_value->is_string()) {
          throw std::invalid_argument("SuperMap geometry value must be base64 EWKB text");
        }
        const std::vector<unsigned char> ewkb = base64_decode(geometry_value->get<std::string>());
        if (ewkb.empty()) {
          throw std::invalid_argument("SuperMap geometry EWKB must not be empty");
        }
        UGC::UGMemoryStream stream;
        if (!stream.Open(
                UGC::UGStreamLoad,
                static_cast<UGC::UGSizeT>(ewkb.size()),
                const_cast<UGC::UGuchar*>(ewkb.data()))) {
          throw std::runtime_error("failed to open EWKB memory stream");
        }
        geometry.reset(UGC::UGGeometryOGC::WKBToUGGeometry(
            &stream, static_cast<UGC::UGshort>(-1)));
        if (geometry == nullptr) {
          throw std::invalid_argument(
              "failed to decode EWKB at row " + std::to_string(row_index) + "; " +
              last_error_detail());
        }
      }
      if (memory->AddNew(geometry.get()) < 0) {
        throw std::runtime_error(
            "failed to add SuperMap memory row " + std::to_string(row_index) + "; " +
            last_error_detail());
      }
      if (geometry != nullptr) {
        geometry.release();
        ++session.geometry_count;
      }

      for (std::size_t field_index = 0; field_index < session.fields.GetSize(); ++field_index) {
        OGDC::OgdcFieldInfo field = session.fields[field_index];
        if (field.IsSystemField() || field.IsGeoField() ||
            reserved_supermap_field(to_utf8(field.m_strName))) {
          continue;
        }
        const std::string name = to_utf8(field.m_strName);
        const Json* value = find_row_value(row, name);
        if (value == nullptr) {
          continue;
        }
        const UGC::UGVariant variant = json_to_variant(*value, field.m_nType);
        if (!memory->SetFieldValue(field.m_strName, variant)) {
          throw std::runtime_error(
              "failed to set SuperMap field " + name + " at row " +
              std::to_string(row_index) + "; " + last_error_detail());
        }
      }
    }

    memory->MoveFirst();
    if (!session.opened.dataset->dataset->Append(memory, false)) {
      throw std::runtime_error("DatasetVector.Append failed; " + last_error_detail());
    }
    session.written_count += static_cast<std::int64_t>(rows.size());
    session.expected_count += static_cast<std::int64_t>(rows.size());
    return {
        {"written", rows.size()},
        {"total_written", session.written_count},
    };
  }

  Json write_close(const Json& params) {
    const std::string session_id = required_string(params, "session_id");
    WriteSession& session = require_write_session(session_id);
    UGC::UGDatasetVectorPtr dataset = session.opened.dataset->dataset;
    if (!dataset->ComputeBounds()) {
      throw std::runtime_error("ComputeBounds failed; " + last_error_detail());
    }
    if (!dataset->UpdateSpatialIndex()) {
      throw std::runtime_error("UpdateSpatialIndex failed; " + last_error_detail());
    }
    if (!dataset->SaveInfo() || !dataset->RefreshInfo()) {
      throw std::runtime_error("failed to refresh SuperMap dataset metadata; " + last_error_detail());
    }

    dataset->Close();
    if (!dataset->Open()) {
      throw std::runtime_error("failed to reopen SuperMap dataset; " + last_error_detail());
    }
    UGC::UGQueryDef query;
    query.m_nCursorType = UGC::UGQueryDef::OpenStatic;
    UGC::UGRecordsetPtr recordset = dataset->Query(query);
    if (recordset == nullptr || !recordset->Refresh()) {
      throw std::runtime_error("failed to refresh SuperMap verification recordset; " + last_error_detail());
    }
    const std::int64_t dataset_count = dataset->GetObjectCount();
    const std::int64_t recordset_count = recordset->GetRecordCount();
    if (dataset_count != session.expected_count || recordset_count != session.expected_count) {
      throw std::runtime_error(
          "SuperMap record count verification failed: expected=" +
          std::to_string(session.expected_count) + ", dataset=" +
          std::to_string(dataset_count) + ", recordset=" +
          std::to_string(recordset_count));
    }
    if (session.geometry_count > 0 && dataset->GetBounds().IsEmpty()) {
      throw std::runtime_error("SuperMap bounds verification failed: bounds are empty");
    }
    if (dataset->IsSpatialIndexDirty()) {
      throw std::runtime_error("SuperMap spatial index verification failed: index is dirty");
    }
    Json result = {
        {"closed", true},
        {"record_count", dataset_count},
        {"bounds", dataset_bounds(dataset)},
        {"spatial_index_dirty", false},
    };
    write_sessions_.erase(session_id);
    return result;
  }

  Json write_abort(const Json& params) {
    const std::string session_id = required_string(params, "session_id");
    const auto found = write_sessions_.find(session_id);
    if (found == write_sessions_.end()) {
      return {{"aborted", false}};
    }
    WriteSession session = std::move(found->second);
    write_sessions_.erase(found);
    const bool remove_target = session.replace;
    const UGC::UGString dataset_name = session.opened.dataset->dataset->GetName();
    session.opened.dataset.reset();
    if (remove_target &&
        !session.opened.datasource->datasource->DeleteDataset(dataset_name)) {
      throw std::runtime_error("failed to delete aborted SuperMap target; " + last_error_detail());
    }
    return {{"aborted", true}, {"target_deleted", remove_target}};
  }

  ReadSession& require_read_session(const std::string& session_id) {
    const auto found = read_sessions_.find(session_id);
    if (found == read_sessions_.end()) {
      throw std::invalid_argument("SuperMap read session not found: " + session_id);
    }
    return found->second;
  }

  WriteSession& require_write_session(const std::string& session_id) {
    const auto found = write_sessions_.find(session_id);
    if (found == write_sessions_.end()) {
      throw std::invalid_argument("SuperMap write session not found: " + session_id);
    }
    return found->second;
  }

  std::unordered_map<std::string, ReadSession> read_sessions_;
  std::unordered_map<std::string, WriteSession> write_sessions_;
};

TableSessionRuntime::TableSessionRuntime() : impl_(std::make_unique<Impl>()) {}

TableSessionRuntime::~TableSessionRuntime() = default;

Json TableSessionRuntime::invoke(
    const std::string& operator_id, const Json& params) {
  return impl_->invoke(operator_id, params);
}

}  // namespace addp::supermap
