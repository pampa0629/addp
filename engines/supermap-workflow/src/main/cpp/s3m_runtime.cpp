#include "s3m_runtime.hpp"

#include "runtime_access.hpp"
#include "supermap_runtime.hpp"

#include "Base3D/UGTinyxml.h"
#include "CacheBuilder/UGObliquePhotogrammetryBuilder.h"
#include "CacheBuilder/UGOSGBCacheBuilder.h"
#include "FileParser/UGExchangeFileType.h"
#include "Projection/UGPrjCoordSys.h"
#include "Projection/UGRefTranslator.h"

#include <algorithm>
#include <array>
#include <cmath>
#include <filesystem>
#include <fstream>
#include <iomanip>
#include <stdexcept>
#include <string>
#include <system_error>
#include <unistd.h>
#include <utility>
#include <vector>

namespace addp::supermap {
namespace {

using addp::workflow::Json;

struct OSGBSceneMetadata {
  int epsg;
  double origin_x;
  double origin_y;
  double origin_z;
};

class TemporaryDirectory {
 public:
  explicit TemporaryDirectory(const std::string& prefix) {
    std::string pattern = (std::filesystem::temp_directory_path() / (prefix + "XXXXXX")).string();
    std::vector<char> writable(pattern.begin(), pattern.end());
    writable.push_back('\0');
    const char* created = ::mkdtemp(writable.data());
    if (created == nullptr) {
      throw std::runtime_error("failed to create temporary S3M directory");
    }
    path_ = created;
  }

  ~TemporaryDirectory() {
    std::error_code error;
    std::filesystem::remove_all(path_, error);
  }

  TemporaryDirectory(const TemporaryDirectory&) = delete;
  TemporaryDirectory& operator=(const TemporaryDirectory&) = delete;

  const std::filesystem::path& path() const noexcept {
    return path_;
  }

 private:
  std::filesystem::path path_;
};

Json required_object(const Json& value, const std::string& name) {
  const auto found = value.find(name);
  if (found == value.end() || !found->is_object()) {
    throw std::invalid_argument(name + " must be an object");
  }
  return *found;
}

std::string required_string(const Json& value, const std::string& name) {
  const auto found = value.find(name);
  if (found == value.end() || !found->is_string() || found->get<std::string>().empty()) {
    throw std::invalid_argument(name + " must be a non-empty string");
  }
  return found->get<std::string>();
}

std::string optional_string(
    const Json& value, const std::string& name, const std::string& fallback = "") {
  const auto found = value.find(name);
  if (found == value.end() || found->is_null()) {
    return fallback;
  }
  if (!found->is_string()) {
    throw std::invalid_argument(name + " must be a string");
  }
  return found->get<std::string>();
}

std::string trim(std::string value) {
  const auto first = value.find_first_not_of(" \t\r\n");
  if (first == std::string::npos) {
    return "";
  }
  const auto last = value.find_last_not_of(" \t\r\n");
  return value.substr(first, last - first + 1);
}

const char* required_element_text(
    const UGTiXmlElement* root, const char* name, const std::filesystem::path& metadata_path) {
  const UGTiXmlElement* element = root == nullptr ? nullptr : root->FirstChildElement(name);
  const char* text = element == nullptr ? nullptr : element->GetText();
  if (text == nullptr || trim(text).empty()) {
    throw std::invalid_argument(
        "OSGB scene metadata.xml is missing " + std::string(name) + ": " +
        metadata_path.string());
  }
  return text;
}

OSGBSceneMetadata read_scene_metadata(const std::filesystem::path& metadata_path) {
  if (!std::filesystem::is_regular_file(metadata_path)) {
    throw std::invalid_argument(
        "OSGB scene metadata.xml does not exist: " + metadata_path.string());
  }
  UGTiXmlDocument document(metadata_path.c_str());
  if (!document.LoadFile(TIXML_ENCODING_UTF8)) {
    throw std::invalid_argument(
        "failed to parse OSGB scene metadata.xml: " + std::string(document.ErrorDesc()));
  }
  const UGTiXmlElement* root = document.RootElement();
  if (root == nullptr || std::string(root->Value()) != "ModelMetadata") {
    throw std::invalid_argument("OSGB scene metadata.xml root must be ModelMetadata");
  }

  const std::string srs = trim(required_element_text(root, "SRS", metadata_path));
  std::string normalized_srs = srs;
  std::transform(
      normalized_srs.begin(), normalized_srs.end(), normalized_srs.begin(),
      [](unsigned char character) { return static_cast<char>(std::toupper(character)); });
  if (normalized_srs.rfind("EPSG:", 0) != 0) {
    throw std::invalid_argument("OSGB scene SRS must use EPSG:<code>: " + srs);
  }

  const std::string origin = trim(required_element_text(root, "SRSOrigin", metadata_path));
  std::array<double, 3> values {};
  std::size_t start = 0;
  for (std::size_t index = 0; index < values.size(); ++index) {
    const std::size_t separator = origin.find(',', start);
    if ((index < values.size() - 1 && separator == std::string::npos) ||
        (index == values.size() - 1 && separator != std::string::npos)) {
      throw std::invalid_argument("OSGB scene SRSOrigin must contain x,y,z");
    }
    const std::string component = trim(origin.substr(start, separator - start));
    std::size_t consumed = 0;
    try {
      values[index] = std::stod(component, &consumed);
    } catch (const std::exception&) {
      throw std::invalid_argument("OSGB scene SRSOrigin must contain numeric x,y,z");
    }
    if (consumed != component.size() || !std::isfinite(values[index])) {
      throw std::invalid_argument("OSGB scene SRSOrigin must contain finite numeric x,y,z");
    }
    start = separator == std::string::npos ? origin.size() : separator + 1;
  }

  std::size_t consumed = 0;
  int epsg = 0;
  const std::string epsg_text = trim(srs.substr(srs.find(':') + 1));
  try {
    epsg = std::stoi(epsg_text, &consumed);
  } catch (const std::exception&) {
    throw std::invalid_argument("OSGB scene SRS contains an invalid EPSG code: " + srs);
  }
  if (consumed != epsg_text.size() || epsg <= 0) {
    throw std::invalid_argument("OSGB scene SRS contains an invalid EPSG code: " + srs);
  }
  return {epsg, values[0], values[1], values[2]};
}

std::vector<std::filesystem::path> find_root_tiles(const std::filesystem::path& data_root) {
  std::vector<std::filesystem::path> result;
  if (!std::filesystem::is_directory(data_root)) {
    return result;
  }
  for (const auto& entry : std::filesystem::directory_iterator(data_root)) {
    if (!entry.is_directory()) {
      continue;
    }
    const std::filesystem::path root =
        entry.path() / (entry.path().filename().string() + ".osgb");
    if (std::filesystem::is_regular_file(root)) {
      result.push_back(root);
    }
  }
  std::sort(result.begin(), result.end());
  return result;
}

void copy_scene_data(
    const std::filesystem::path& source_data, const std::filesystem::path& staged_data) {
  if (!std::filesystem::is_directory(source_data)) {
    throw std::invalid_argument(
        "OSGB scene Data directory does not exist: " + source_data.string());
  }
  std::filesystem::create_directories(staged_data);
  for (const auto& entry : std::filesystem::recursive_directory_iterator(source_data)) {
    const std::filesystem::path relative = entry.path().lexically_relative(source_data);
    const std::filesystem::path staged = staged_data / relative;
    if (entry.is_directory()) {
      std::filesystem::create_directories(staged);
    } else if (entry.is_regular_file()) {
      std::filesystem::create_directories(staged.parent_path());
      std::filesystem::copy_file(entry.path(), staged);
    }
  }
}

std::filesystem::path find_single_manifest(const std::filesystem::path& root) {
  std::vector<std::filesystem::path> manifests;
  for (const auto& entry : std::filesystem::recursive_directory_iterator(root)) {
    if (entry.is_regular_file() && entry.path().extension() == ".scp") {
      manifests.push_back(entry.path());
    }
  }
  if (manifests.size() != 1) {
    throw std::runtime_error(
        "S3M output must contain exactly one SCP manifest, got " +
        std::to_string(manifests.size()));
  }
  return manifests.front();
}

Json read_json(const std::filesystem::path& path) {
  std::ifstream input(path);
  if (!input) {
    throw std::runtime_error("failed to open S3M manifest: " + path.string());
  }
  Json result;
  try {
    input >> result;
  } catch (const std::exception& error) {
    throw std::runtime_error("failed to parse S3M manifest: " + std::string(error.what()));
  }
  return result;
}

void write_json(const std::filesystem::path& path, const Json& value) {
  std::ofstream output(path, std::ios::trunc);
  if (!output) {
    throw std::runtime_error("failed to write S3M manifest: " + path.string());
  }
  output << std::setw(2) << value << '\n';
}

double finite_number(const Json& value, const std::string& name) {
  if (!value.is_number()) {
    throw std::runtime_error("S3M manifest " + name + " must be numeric");
  }
  const double result = value.get<double>();
  if (!std::isfinite(result)) {
    throw std::runtime_error("S3M manifest " + name + " must be finite");
  }
  return result;
}

void normalize_manifest_georeference(
    const std::filesystem::path& manifest, int source_epsg) {
  Json config = read_json(manifest);
  if (!config.is_object() || !config["position"].is_object() ||
      !config["position"]["point3D"].is_object()) {
    throw std::runtime_error("S3M manifest position is missing");
  }
  Json& position = config["position"];
  Json& point = position["point3D"];
  std::vector<UGC::UGPoint2D> points;
  points.emplace_back(
      finite_number(point["x"], "position.point3D.x"),
      finite_number(point["y"], "position.point3D.y"));

  Json* bounds = config["geoBounds"].is_object() ? &config["geoBounds"] : nullptr;
  if (bounds != nullptr) {
    const double left = finite_number((*bounds)["left"], "geoBounds.left");
    const double bottom = finite_number((*bounds)["bottom"], "geoBounds.bottom");
    const double right = finite_number((*bounds)["right"], "geoBounds.right");
    const double top = finite_number((*bounds)["top"], "geoBounds.top");
    points.emplace_back(left, bottom);
    points.emplace_back(left, top);
    points.emplace_back(right, bottom);
    points.emplace_back(right, top);
  }

  const UGC::UGPrjCoordSys source_crs(source_epsg);
  const UGC::UGPrjCoordSys target_crs(4326);
  if (source_crs.GetEPSGCode() == 0 || target_crs.GetEPSGCode() == 0) {
    throw std::invalid_argument("unsupported OSGB scene source or target CRS");
  }
  UGC::UGRefTranslator translator;
  if (translator.SetPrjCoordSysSrc(source_crs) < 0 ||
      translator.SetPrjCoordSysDes(target_crs) < 0) {
    throw std::runtime_error("failed to configure S3M georeference transformation");
  }
  translator.SetGeoTransMethod(UGC::MTH_Prj4);
  if (!translator.Translate(points.data(), static_cast<UGC::UGlong>(points.size()))) {
    throw std::runtime_error("failed to transform S3M manifest georeference to EPSG:4326");
  }

  point["x"] = points[0].x;
  point["y"] = points[0].y;
  position["unit"] = "Degree";
  position.erase("units");
  config["crs"] = "epsg:4326";
  if (bounds != nullptr) {
    double left = points[1].x;
    double bottom = points[1].y;
    double right = points[1].x;
    double top = points[1].y;
    for (std::size_t index = 2; index < points.size(); ++index) {
      left = std::min(left, points[index].x);
      bottom = std::min(bottom, points[index].y);
      right = std::max(right, points[index].x);
      top = std::max(top, points[index].y);
    }
    (*bounds)["left"] = left;
    (*bounds)["bottom"] = bottom;
    (*bounds)["right"] = right;
    (*bounds)["top"] = top;
  }
  write_json(manifest, config);
}

std::string lowercase(std::string value) {
  std::transform(value.begin(), value.end(), value.begin(), [](unsigned char character) {
    return static_cast<char>(std::tolower(character));
  });
  return value;
}

bool path_is_within(
    const std::filesystem::path& root, const std::filesystem::path& candidate) {
  const std::filesystem::path relative =
      std::filesystem::weakly_canonical(candidate).lexically_relative(
          std::filesystem::weakly_canonical(root));
  const auto first = relative.begin();
  return !relative.empty() && !relative.is_absolute() &&
      (first == relative.end() || *first != "..");
}

int validate_s3m_output(
    const std::filesystem::path& target_root,
    const std::filesystem::path& manifest,
    std::size_t source_root_count) {
  const Json config = read_json(manifest);
  if (config.value("version", "") != "3.01") {
    throw std::runtime_error("S3M manifest version must be 3.01");
  }
  if (lowercase(config.value("crs", "")) != "epsg:4326") {
    throw std::runtime_error("S3M manifest CRS must be EPSG:4326");
  }
  const Json position = config.value("position", Json::object());
  if (lowercase(position.value("unit", "")) != "degree") {
    throw std::runtime_error("S3M manifest position unit must be Degree");
  }
  const Json point = position.value("point3D", Json::object());
  const double longitude = finite_number(point.value("x", Json()), "position.point3D.x");
  const double latitude = finite_number(point.value("y", Json()), "position.point3D.y");
  if (longitude < -180.0 || longitude > 180.0 || latitude < -90.0 || latitude > 90.0) {
    throw std::runtime_error("S3M manifest position must contain WGS84 longitude and latitude");
  }
  const Json extensions = config.value("extensions", Json::object());
  if (lowercase(extensions.value("s3m:TextureCompressionType", "")) != "dxt") {
    throw std::runtime_error("S3M manifest texture compression must be DXT");
  }
  if (lowercase(extensions.value("s3m:VertexCompressionType", "")) != "draco") {
    throw std::runtime_error("S3M manifest geometry compression must be DRACO");
  }
  const Json root_tiles = config.value("rootTiles", Json());
  if (!root_tiles.is_array()) {
    throw std::runtime_error("S3M manifest rootTiles must be an array");
  }
  int referenced = 0;
  for (const Json& tile : root_tiles) {
    const std::string ref = trim(tile.value("url", ""));
    if (lowercase(std::filesystem::path(ref).extension().string()) != ".s3mb") {
      throw std::runtime_error("S3M 3.01 root tile must use .s3mb: " + ref);
    }
    const std::filesystem::path path = (manifest.parent_path() / ref).lexically_normal();
    if (!path_is_within(target_root, path) || !std::filesystem::is_regular_file(path)) {
      throw std::runtime_error("S3M manifest references a missing tile: " + ref);
    }
    ++referenced;
  }
  if (referenced == 0 || static_cast<std::size_t>(referenced) > source_root_count) {
    throw std::runtime_error(
        "S3M output has invalid root tile count: referenced=" + std::to_string(referenced) +
        ", source candidates=" + std::to_string(source_root_count));
  }
  return referenced;
}

void prepare_mounted_target(
    const std::filesystem::path& target_root, const std::string& write_mode) {
  if (target_root.empty() || target_root == target_root.root_path()) {
    throw std::invalid_argument("refusing to use a filesystem root as S3M target");
  }
  if (std::filesystem::exists(target_root)) {
    if (write_mode == "create") {
      throw std::invalid_argument("S3M target already exists: " + target_root.string());
    }
    std::filesystem::remove_all(target_root);
  }
  std::filesystem::create_directories(target_root / "config");
}

void generate_s3m(
    const std::filesystem::path& staged_scene,
    const std::filesystem::path& target_root,
    const OSGBSceneMetadata& metadata) {
  const std::vector<std::filesystem::path> root_paths =
      find_root_tiles(staged_scene / "Data");
  std::vector<UGC::UGString> root_names;
  root_names.reserve(root_paths.size());
  for (const auto& path : root_paths) {
    root_names.push_back(to_ug_string(path.string()));
  }

  const std::filesystem::path source_scp = staged_scene / "scene.scp";
  UGC::UGPrjCoordSys source_crs(metadata.epsg);
  UGC::UGGenerateOSGBConfigParams generate_params;
  generate_params.m_pPrjCoordSys = &source_crs;
  if (!UGC::UGOSGBCacheBuilder::GenerateOSGBConfigFile(
          to_ug_string(source_scp.string()),
          UGC::UGVector3d(metadata.origin_x, metadata.origin_y, metadata.origin_z),
          root_names,
          generate_params)) {
    throw std::runtime_error("SuperMap failed to generate OSGB scene SCP");
  }

  UGC::UGMergeParams merge;
  merge.m_strSCPFile = to_ug_string(source_scp.string());
  merge.m_strOutputFolder = to_ug_string((target_root / "config").string());
  merge.m_nThreadCount = 1;
  merge.m_nFileType = UGC::UGFileType::S3MB;
  merge.m_eStoreType = UGC::StoreType::PURE_FILES;
  merge.m_fVersion = 3.01f;
  merge.m_eVertexOptimizationType = UGC::VO_Draco;
  merge.m_eTexCompressionType = UGC::UGDataCodec::enrS3TCDXTN;
  merge.m_eGlobeType = UGC::Ellipsoid_WGS84;

  UGC::UGCompressTextureParam compress;
  compress.m_bTexCompress = TRUE;
  compress.m_nTexCompressionType = UGC::UGDataCodec::enrS3TCDXTN;

  UGC::UGObliquePhotogrammetryBuilder builder;
  if (!builder.ProcessOSGB(
          merge,
          UGC::UGModifyParam(),
          UGC::UGCombineParam(),
          compress,
          UGC::UGDiscretParam(),
          UGC::UGClipParam(),
          UGC::UGTextureRemappingParam(),
          UGC::UGComputeNormalParam(),
          UGC::UGLightWeightParam(),
          UGC::SV_DracoCompressed)) {
    throw std::runtime_error("SuperMap OSGB to S3M conversion returned false");
  }
}

}  // namespace

Json convert_osgb_scene_to_s3m(const Json& params) {
  if (!params.is_object()) {
    throw std::invalid_argument("params must be an object");
  }
  const Json plan = required_object(params, "access_plan");
  if (required_string(plan, "schema_version") != "addp.workflow.access-plan/v1") {
    throw std::invalid_argument("unsupported access_plan.schema_version");
  }
  const Json source = required_object(plan, "source");
  const Json target = required_object(plan, "target");
  if (required_string(source, "kind") != "directory" ||
      required_string(source, "format") != "osgb_scene") {
    throw std::invalid_argument("access_plan.source must be directory/osgb_scene");
  }
  if (required_string(target, "kind") != "directory" ||
      required_string(target, "format") != "s3m") {
    throw std::invalid_argument("access_plan.target must be directory/s3m");
  }

  const Json source_access = required_object(source, "access");
  const Json target_access = required_object(target, "access");
  const std::filesystem::path source_root = resolve_workflow_mounted_path(source_access);
  if (!std::filesystem::is_directory(source_root)) {
    throw std::invalid_argument("OSGB scene directory does not exist: " + source_root.string());
  }
  const std::string write_mode = optional_string(target, "write_mode", "create");
  if (write_mode != "create" && write_mode != "replace") {
    throw std::invalid_argument("target write_mode must be create or replace");
  }
  const std::string target_method = required_string(target_access, "method");
  const bool object_store_target = target_method == "object_store";
  if (!object_store_target && target_method != "mounted_path") {
    throw std::invalid_argument(
        "S3M target access method must be mounted_path or object_store");
  }

  const OSGBSceneMetadata metadata = read_scene_metadata(source_root / "metadata.xml");
  const std::vector<std::filesystem::path> source_roots = find_root_tiles(source_root / "Data");
  if (source_roots.empty()) {
    throw std::invalid_argument(
        "OSGB scene does not contain Data/Tile_*/Tile_*.osgb roots");
  }

  TemporaryDirectory work("addp-supermap-s3m-work-");
  TemporaryDirectory object_store_output("addp-supermap-s3m-output-");
  const std::filesystem::path target_root = object_store_target
      ? object_store_output.path()
      : resolve_workflow_mounted_path(target_access);
  bool mounted_target_prepared = false;
  try {
    if (object_store_target) {
      std::filesystem::create_directories(target_root / "config");
    } else {
      prepare_mounted_target(target_root, write_mode);
      mounted_target_prepared = true;
    }

    const std::filesystem::path staged_scene = work.path() / "scene";
    copy_scene_data(source_root / "Data", staged_scene / "Data");
    const std::vector<std::filesystem::path> staged_roots =
        find_root_tiles(staged_scene / "Data");
    if (staged_roots.size() != source_roots.size()) {
      throw std::runtime_error("failed to stage all OSGB root tiles");
    }

    generate_s3m(staged_scene, target_root, metadata);
    const std::filesystem::path generated_manifest = find_single_manifest(target_root);
    const std::filesystem::path manifest = generated_manifest.parent_path() / "scene.scp";
    if (generated_manifest != manifest) {
      std::filesystem::rename(generated_manifest, manifest);
    }
    normalize_manifest_georeference(manifest, metadata.epsg);
    const int root_tile_count =
        validate_s3m_output(target_root, manifest, source_roots.size());

    std::uintmax_t size_bytes = 0;
    std::size_t file_count = 0;
    for (const auto& entry : std::filesystem::recursive_directory_iterator(target_root)) {
      if (entry.is_regular_file()) {
        ++file_count;
        size_bytes += entry.file_size();
      }
    }
    const std::string manifest_ref = manifest.lexically_relative(target_root).generic_string();
    if (object_store_target) {
      publish_workflow_directory(target_root, target_access, write_mode);
    }
    return {
        {"kind", "supermap_s3m_dataset"},
        {"target_format", "s3m"},
        {"target_path",
         object_store_target ? optional_string(target_access, "prefix") : target_root.string()},
        {"manifest_ref", manifest_ref},
        {"texture_compression", "dxt"},
        {"geometry_compression", "draco"},
        {"s3m_version", "3.01"},
        {"crs", "EPSG:4326"},
        {"manifest_encoding", "json"},
        {"tile_extension", ".s3mb"},
        {"root_tile_count", root_tile_count},
        {"source_root_candidate_count", source_roots.size()},
        {"file_count", file_count},
        {"size_bytes", size_bytes},
    };
  } catch (...) {
    if (mounted_target_prepared) {
      std::error_code error;
      std::filesystem::remove_all(target_root, error);
    }
    throw;
  }
}

}  // namespace addp::supermap
