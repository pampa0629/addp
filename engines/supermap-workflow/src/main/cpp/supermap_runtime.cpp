#include "supermap_runtime.hpp"

#include "Engine/UGDataSourceManager.h"
#include "Engine/UGDataset.h"
#include "Engine/UGQueryDef.h"
#include "Element/OgdcFieldInfo.h"
#include "Geometry/UGGeometry.h"
#include "Projection/UGRefTranslator.h"
#include "Toolkit/UGErrorObj.h"

#include <algorithm>
#include <cctype>
#include <filesystem>
#include <memory>
#include <stdexcept>

namespace addp::supermap {
namespace {

std::string dataset_type_name(UGC::UGDataset::DatasetType type) {
  switch (type) {
    case UGC::UGDataset::Tabular:
      return "Tabular";
    case UGC::UGDataset::Point:
      return "Point";
    case UGC::UGDataset::Line:
      return "Line";
    case UGC::UGDataset::Region:
      return "Region";
    case UGC::UGDataset::Text:
      return "Text";
    case UGC::UGDataset::CAD:
      return "CAD";
    default:
      return std::to_string(static_cast<int>(type));
  }
}

std::string engine_type_name(UGC::UGEngineType type) {
  switch (type) {
    case UGC::UGEngineType::UDB:
      return "UDB";
    case UGC::UGEngineType::Spatialite:
      return "UDBX";
    case UGC::UGEngineType::PostgreSQLGis:
      return "PGGIS";
    case UGC::UGEngineType::VectorFile:
      return "VectorFile";
    default:
      return std::to_string(static_cast<int>(type));
  }
}

std::string field_type_name(OGDC::OgdcFieldInfo::FieldType type) {
  switch (type) {
    case OGDC::OgdcFieldInfo::Boolean:
      return "boolean field";
    case OGDC::OgdcFieldInfo::Byte:
      return "byte field";
    case OGDC::OgdcFieldInfo::INT16:
      return "16-bit integer field";
    case OGDC::OgdcFieldInfo::INT32:
      return "32-bit integer field";
    case OGDC::OgdcFieldInfo::INT64:
      return "64-bit integer field";
    case OGDC::OgdcFieldInfo::Float:
      return "32-bit precision floating-point field";
    case OGDC::OgdcFieldInfo::Double:
      return "64-bit precision floating-point field";
    case OGDC::OgdcFieldInfo::Date:
      return "date type field";
    case OGDC::OgdcFieldInfo::LongBinary:
    case OGDC::OgdcFieldInfo::Geometry:
      return "binary field";
    case OGDC::OgdcFieldInfo::Text:
      return "variable-length text field";
    case OGDC::OgdcFieldInfo::Char:
      return "fixed-length text type field";
    case OGDC::OgdcFieldInfo::Time:
      return "time type field";
    case OGDC::OgdcFieldInfo::TimeStamp:
      return "datetime type field";
    case OGDC::OgdcFieldInfo::NText:
      return "wide character type field";
    case OGDC::OgdcFieldInfo::JsonB:
      return "JSONB type field";
    case OGDC::OgdcFieldInfo::GeoGridCode:
      return "GeoGridCode";
    case OGDC::OgdcFieldInfo::GeoGridCodeArray:
      return "GeoGridCodeArray";
    default:
      return std::to_string(static_cast<int>(type));
  }
}

std::string projection_type_name(UGC::EmPrjCoordSysType type) {
  switch (type) {
    case UGC::PCS_CHINA_2000_3_DEGREE_GK_40N:
      return "PCS_CHINA_2000_3_DEGREE_GK_40N";
    default:
      return std::to_string(static_cast<int>(type));
  }
}

std::string unit_name(UGC::UGint unit) {
  switch (unit) {
    case AU_MILIMETER:
      return "mm";
    case AU_CENTIMETER:
      return "cm";
    case AU_DECIMETER:
      return "dm";
    case AU_METER:
      return "m";
    case AU_KILOMETER:
      return "km";
    case AU_INCH:
      return "Inch";
    case AU_FOOT:
      return "Foot";
    case AU_YARD:
      return "Yard";
    case AU_MILE:
      return "Mile";
    case AU_RADIAN:
      return "Radian";
    default:
      return std::to_string(unit);
  }
}

std::string effective_alias(const std::string& path, const std::string& alias) {
  if (!alias.empty()) {
    return alias;
  }
  const auto filename = std::filesystem::path(path).filename().string();
  return filename.empty() ? "supermap" : filename;
}

void prepare_output_dataset(
    const UGC::UGDataSourcePtr& datasource, const std::string& name, bool overwrite) {
  const UGC::UGString dataset_name = to_ug_string(name);
  if (datasource->GetDataset(dataset_name) == nullptr) {
    return;
  }
  if (!overwrite) {
    throw std::invalid_argument("output dataset already exists: " + name);
  }
  if (!datasource->DeleteDataset(dataset_name)) {
    throw std::runtime_error("failed to delete existing output dataset: " + name);
  }
}

UGC::UGQueryDef static_query(const std::string& attribute_filter) {
  UGC::UGQueryDef query;
  query.m_strFilter = to_ug_string(attribute_filter);
  query.m_nCursorType = UGC::UGQueryDef::OpenStatic;
  return query;
}

UGC::EmGeoTransMethod coordinate_transform_method(std::string value) {
  std::transform(value.begin(), value.end(), value.begin(), [](unsigned char character) {
    return character == '-' ? '_' : static_cast<char>(std::tolower(character));
  });
  if (value.empty() || value == "geocentric_translation" ||
      value == "mth_geocentric_translation") {
    return UGC::MTH_GEOCENTRIC_TRANSLATION;
  }
  if (value == "molodensky" || value == "mth_molodensky") {
    return UGC::MTH_MOLODENSKY;
  }
  if (value == "molodensky_abridged" || value == "mth_molodensky_abridged") {
    return UGC::MTH_MOLODENSKY_ABRIDGED;
  }
  if (value == "position_vector" || value == "mth_position_vector") {
    return UGC::MTH_POSITION_VECTOR;
  }
  if (value == "coordinate_frame" || value == "mth_coordinate_frame") {
    return UGC::MTH_COORDINATE_FRAME;
  }
  if (value == "bursa_wolf" || value == "mth_bursa_wolf") {
    return UGC::MTH_BURSA_WOLF;
  }
  if (value == "prj4" || value == "mth_prj4") {
    return UGC::MTH_Prj4;
  }
  if (value == "bd09_to_gcj02") {
    return UGC::MTH_BD09TOGCJ02;
  }
  if (value == "gcj02_to_bd09") {
    return UGC::MTH_GCJ02TOBD09;
  }
  if (value == "gcj02_to_wgs84") {
    return UGC::MTH_GCJ02TOWGS84;
  }
  if (value == "wgs84_to_gcj02") {
    return UGC::MTH_WGS84TOGCJ02;
  }
  throw std::invalid_argument("unsupported coordinate transform method: " + value);
}

}  // namespace

std::string to_utf8(const UGC::UGString& value) {
  std::string result;
  value.ToStd(result, OGDC::OGDCCharset::UTF8);
  return result;
}

UGC::UGString to_ug_string(const std::string& value) {
  UGC::UGString result;
  result.FromStd(value, OGDC::OGDCCharset::UTF8);
  return result;
}

Json DatasourceRef::summary() const {
  return {
      {"kind", "supermap_datasource"},
      {"path", path},
      {"alias", to_utf8(datasource->GetAlias())},
      {"engine_type", engine_type_name(datasource->GetEngineType())},
      {"dataset_count", datasource->GetDatasetCount()},
  };
}

Json DatasetRef::summary() const {
  return {
      {"kind", "supermap_dataset"},
      {"datasource_path", datasource_ref->path},
      {"datasource_alias", to_utf8(datasource_ref->datasource->GetAlias())},
      {"dataset_name", to_utf8(dataset->GetName())},
      {"dataset_type", dataset_type_name(dataset->GetType())},
      {"record_count", dataset->GetObjectCount()},
  };
}

Json summarize(const RuntimeValue& value) {
  if (const auto* json = std::get_if<Json>(&value)) {
    return *json;
  }
  if (const auto* datasource = std::get_if<std::shared_ptr<DatasourceRef>>(&value)) {
    return (*datasource)->summary();
  }
  return std::get<std::shared_ptr<DatasetRef>>(value)->summary();
}

Json dataset_info(const std::shared_ptr<DatasetRef>& dataset) {
  const UGC::UGRect2D& bounds = dataset->dataset->GetBounds();
  Json bounds_json = {{"empty", static_cast<bool>(bounds.IsEmpty())}};
  if (!bounds.IsEmpty()) {
    bounds_json.update({
        {"left", bounds.left},
        {"bottom", bounds.bottom},
        {"right", bounds.right},
        {"top", bounds.top},
        {"width", bounds.Width()},
        {"height", bounds.Height()},
    });
  }

  const UGC::UGPrjCoordSys& projection = dataset->dataset->GetPrjCoordSys();
  UGC::UGFieldInfos field_infos;
  if (!dataset->dataset->GetFieldInfos(field_infos)) {
    throw std::runtime_error("failed to read SuperMap field metadata");
  }
  Json fields = Json::array();
  const UGC::UGint field_count = field_infos.GetSize();
  for (UGC::UGint index = 0; index < field_count; ++index) {
    OGDC::OgdcFieldInfo field = field_infos[index];
    fields.push_back({
        {"name", to_utf8(field.m_strName)},
        {"caption",
         field.m_strForeignName.IsEmpty() ? to_utf8(field.m_strName)
                                          : to_utf8(field.m_strForeignName)},
        {"type", field_type_name(field.m_nType)},
        {"required", static_cast<bool>(field.m_bRequired)},
        {"system", static_cast<bool>(field.IsSystemField())},
        {"max_length", field.m_nSize},
        {"precision", field.m_nPrecision},
        {"scale", field.m_nScale},
    });
  }

  return {
      {"kind", "supermap_dataset_info"},
      {"dataset_name", to_utf8(dataset->dataset->GetName())},
      {"dataset_type", dataset_type_name(dataset->dataset->GetType())},
      {"record_count", dataset->dataset->GetObjectCount()},
      {"field_count", field_count},
      {"bounds", std::move(bounds_json)},
      {"prj_coord_sys",
       {
           {"defined", static_cast<bool>(projection.IsValid())},
           {"name", to_utf8(projection.GetName())},
           {"epsg", projection.GetEPSGCode()},
           {"type", projection_type_name(projection.GetTypeID())},
           {"coord_unit", unit_name(projection.GetUnit())},
           {"distance_unit", unit_name(projection.GetDistUnit())},
       }},
      {"fields", std::move(fields)},
  };
}

ExecutionContext::~ExecutionContext() {
  for (auto iterator = datasources_.rbegin(); iterator != datasources_.rend(); ++iterator) {
    if ((*iterator)->datasource != nullptr && (*iterator)->datasource->IsOpen()) {
      (*iterator)->datasource->Close();
    }
  }
  datasources_.clear();
}

std::shared_ptr<DatasourceRef> ExecutionContext::open_udbx(
    const std::string& path, const std::string& alias, bool read_only) {
  if (!std::filesystem::is_regular_file(path)) {
    throw std::invalid_argument("UDBX file does not exist: " + path);
  }
  return connect(
      UGC::UGEngineType::Spatialite,
      path,
      path,
      "",
      "",
      "",
      "",
      effective_alias(path, alias),
      read_only,
      false);
}

std::shared_ptr<DatasourceRef> ExecutionContext::create_udbx(
    const std::string& path, const std::string& alias, bool overwrite) {
  const std::filesystem::path output(path);
  if (output.has_parent_path()) {
    std::filesystem::create_directories(output.parent_path());
  }
  if (std::filesystem::exists(output)) {
    if (!overwrite) {
      throw std::invalid_argument("UDBX file already exists: " + path);
    }
    std::filesystem::remove(output);
  }
  return connect(
      UGC::UGEngineType::Spatialite,
      path,
      path,
      "",
      "",
      "",
      "",
      effective_alias(path, alias),
      false,
      true);
}

std::shared_ptr<DatasourceRef> ExecutionContext::open_postgis(
    const std::string& server,
    const std::string& database,
    const std::string& user,
    const std::string& password,
    const std::string& schema,
    const std::string& table,
    const std::string& alias,
    bool read_only) {
  auto datasource = connect(
      UGC::UGEngineType::PostgreSQLGis,
      "postgis://" + server + "/" + database,
      server,
      database,
      user,
      password,
      schema,
      alias.empty() ? "postgis" : alias,
      read_only,
      false);
  if (read_only && !table.empty() && datasource->datasource->GetDataset(to_ug_string(table)) == nullptr) {
    throw std::invalid_argument("PostGIS dataset not found: " + schema + "." + table);
  }
  return datasource;
}

std::shared_ptr<DatasourceRef> ExecutionContext::enable_postgis(
    const std::string& server,
    const std::string& database,
    const std::string& user,
    const std::string& password,
    const std::string& alias) {
  return connect(
      UGC::UGEngineType::PostgreSQLGis,
      "postgis://" + server + "/" + database,
      server,
      database,
      user,
      password,
      "",
      alias.empty() ? "supermap_sdx" : alias,
      false,
      true);
}

std::shared_ptr<DatasetRef> ExecutionContext::select_dataset(
    const std::shared_ptr<DatasourceRef>& datasource, const std::string& name) {
  const UGC::UGDatasetPtr dataset = datasource->datasource->GetDataset(to_ug_string(name));
  const auto vector = std::dynamic_pointer_cast<UGC::UGDatasetVector>(dataset);
  if (vector == nullptr) {
    throw std::invalid_argument("dataset is not a DatasetVector: " + name);
  }
  return std::make_shared<DatasetRef>(DatasetRef{datasource, vector});
}

std::shared_ptr<DatasetRef> ExecutionContext::save_dataset(
    const std::shared_ptr<DatasetRef>& dataset,
    const std::shared_ptr<DatasourceRef>& target_datasource,
    const std::string& output_dataset_name,
    bool overwrite) {
  prepare_output_dataset(target_datasource->datasource, output_dataset_name, overwrite);
  const UGC::UGDatasetPtr copied = target_datasource->datasource->CopyDataset(
      dataset->dataset, to_ug_string(output_dataset_name), UGC::UGDataCodec::encNONE);
  const auto vector = std::dynamic_pointer_cast<UGC::UGDatasetVector>(copied);
  if (vector == nullptr) {
    throw std::runtime_error("CopyDataset did not return DatasetVector: " + output_dataset_name);
  }
  return std::make_shared<DatasetRef>(DatasetRef{target_datasource, vector});
}

std::shared_ptr<DatasetRef> ExecutionContext::project_dataset(
    const std::shared_ptr<DatasetRef>& dataset,
    const std::shared_ptr<DatasourceRef>& output_datasource,
    const std::string& output_dataset_name,
    int target_epsg,
    const std::string& method,
    bool overwrite) {
  UGC::UGPrjCoordSys target_projection;
  if (!target_projection.FromEpsgCode(target_epsg) || !target_projection.IsValid()) {
    throw std::invalid_argument("unsupported target_epsg: " + std::to_string(target_epsg));
  }

  UGC::UGRefTranslator translator;
  if (translator.SetPrjCoordSysSrc(dataset->dataset->GetPrjCoordSys()) < 0 ||
      translator.SetPrjCoordSysDes(target_projection) < 0) {
    throw std::runtime_error("failed to configure coordinate translator");
  }
  translator.SetGeoTransMethod(coordinate_transform_method(method));
  if (!translator.IsValid()) {
    throw std::runtime_error("coordinate translator is invalid");
  }

  prepare_output_dataset(output_datasource->datasource, output_dataset_name, overwrite);
  const UGC::UGDatasetPtr copied = output_datasource->datasource->CopyDataset(
      dataset->dataset, to_ug_string(output_dataset_name), UGC::UGDataCodec::encNONE);
  auto result = std::dynamic_pointer_cast<UGC::UGDatasetVector>(copied);
  if (result == nullptr) {
    throw std::runtime_error("CopyDataset did not return DatasetVector: " + output_dataset_name);
  }

  try {
    UGC::UGQueryDef query;
    query.m_nCursorType = UGC::UGQueryDef::OpenDynamic;
    const UGC::UGRecordsetPtr recordset = result->Query(query);
    if (recordset == nullptr || !recordset->CanUpdate()) {
      throw std::runtime_error("project output dataset is not editable: " + output_dataset_name);
    }
    recordset->MoveFirst();
    while (!recordset->IsEOF()) {
      UGC::UGGeometry* raw_geometry = nullptr;
      if (!recordset->GetGeometry(raw_geometry) || raw_geometry == nullptr) {
        throw std::runtime_error("failed to read geometry while projecting dataset");
      }
      std::unique_ptr<UGC::UGGeometry> geometry(raw_geometry);
      geometry->PJConvert(&translator);
      if (!recordset->Edit() || !recordset->SetGeometry(*geometry) || recordset->Update() < 0) {
        throw std::runtime_error("failed to update projected geometry");
      }
      recordset->MoveNext();
    }
    if (!result->SetPrjCoordSys(target_projection)) {
      throw std::runtime_error("failed to set target projection: " + output_dataset_name);
    }
  } catch (...) {
    result.reset();
    output_datasource->datasource->DeleteDataset(to_ug_string(output_dataset_name));
    throw;
  }

  return std::make_shared<DatasetRef>(DatasetRef{output_datasource, result});
}

std::shared_ptr<DatasetRef> ExecutionContext::filter_dataset(
    const std::shared_ptr<DatasetRef>& dataset,
    const std::shared_ptr<DatasourceRef>& output_datasource,
    const std::string& output_dataset_name,
    const std::string& attribute_filter,
    bool overwrite) {
  prepare_output_dataset(output_datasource->datasource, output_dataset_name, overwrite);
  const UGC::UGRecordsetPtr recordset = dataset->dataset->Query(static_query(attribute_filter));
  if (recordset == nullptr) {
    throw std::runtime_error("dataset query returned null recordset");
  }
  const UGC::UGDatasetVectorPtr result = output_datasource->datasource->RecordsetToDataset(
      recordset, to_ug_string(output_dataset_name));
  if (result == nullptr) {
    throw std::runtime_error("RecordsetToDataset returned null: " + output_dataset_name);
  }
  if (!result->SetPrjCoordSys(dataset->dataset->GetPrjCoordSys())) {
    throw std::runtime_error("failed to inherit output projection: " + output_dataset_name);
  }
  return std::make_shared<DatasetRef>(DatasetRef{output_datasource, result});
}

std::shared_ptr<DatasetRef> ExecutionContext::merge_datasets(
    const std::shared_ptr<DatasetRef>& primary_dataset,
    const std::shared_ptr<DatasetRef>& append_dataset,
    const std::shared_ptr<DatasourceRef>& output_datasource,
    const std::string& output_dataset_name,
    bool overwrite) {
  auto result = save_dataset(
      primary_dataset, output_datasource, output_dataset_name, overwrite);
  const UGC::UGRecordsetPtr recordset = append_dataset->dataset->Query(static_query(""));
  if (recordset == nullptr) {
    throw std::runtime_error("append dataset returned null recordset");
  }
  if (!result->dataset->Append(recordset, false)) {
    throw std::runtime_error("DatasetVector.Append returned false: " + output_dataset_name);
  }
  return result;
}

Json ExecutionContext::query_dataset(
    const std::shared_ptr<DatasetRef>& dataset, const std::string& attribute_filter) {
  UGC::UGint record_count = dataset->dataset->GetObjectCount();
  if (!attribute_filter.empty()) {
    const UGC::UGRecordsetPtr recordset = dataset->dataset->Query(static_query(attribute_filter));
    record_count = recordset == nullptr ? 0 : recordset->GetRecordCount();
  }
  return {
      {"kind", "supermap_query_result"},
      {"dataset_name", to_utf8(dataset->dataset->GetName())},
      {"attribute_filter", attribute_filter},
      {"record_count", record_count},
  };
}

std::shared_ptr<DatasourceRef> ExecutionContext::connect(
    UGC::UGEngineType engine,
    const std::string& path,
    const std::string& server,
    const std::string& database,
    const std::string& user,
    const std::string& password,
    const std::string& schema,
    const std::string& alias,
    bool read_only,
    bool create) {
  UGC::UGDataSourcePtr datasource = UGC::UGDataSourceManager::CreateDataSource(engine);
  if (datasource == nullptr) {
    throw std::runtime_error("failed to create SuperMap datasource provider");
  }
  UGC::UGDsConnection& connection = datasource->GetConnectionInfo();
  connection.m_nType = static_cast<UGC::UGint>(engine);
  connection.m_strServer = to_ug_string(server);
  connection.m_strDatabase = to_ug_string(database);
  connection.m_strUser = to_ug_string(user);
  connection.m_strPassword = to_ug_string(password);
  connection.m_strAlias = to_ug_string(alias);
  connection.m_bReadOnly = read_only;
  if (!schema.empty()) {
    connection.m_dicExAttribute[to_ug_string("Schema")] = to_ug_string(schema);
  }
  const bool connected = create ? datasource->Create() : datasource->Open();
  if (!connected || !datasource->IsOpen()) {
    const auto error = UGC::UGErrorObj::GetInstance().GetLast(false);
    throw std::runtime_error(
        "failed to open SuperMap datasource: " + path + "; error_id=" +
        std::to_string(error.m_nID) + "; message=" + to_utf8(error.m_strMessage));
  }
  auto result = std::make_shared<DatasourceRef>(DatasourceRef{path, datasource});
  datasources_.push_back(result);
  return result;
}

}  // namespace addp::supermap
