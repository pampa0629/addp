#include "cad_runtime.hpp"

#include "runtime_access.hpp"
#include "supermap_runtime.hpp"

#include "Engine/UGDataSourceManager.h"
#include "Engine/UGDatasetVector.h"
#include "Graphics/UGGraphicsManager.h"
#include "GraphicsQT/UGQGraphicsFactory.h"
#include "Map/UGMap.h"
#include "Toolkit/UGErrorObj.h"
#include "Toolkit/UGStyle.h"
#include "Workspace/UGWorkspace.h"

#include <algorithm>
#include <array>
#include <cctype>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <limits>
#include <stdexcept>
#include <string>
#include <vector>

namespace addp::supermap {
namespace {

using addp::workflow::Json;

[[gnu::used]] UGC::UGGraphicsFactory* (*const qt_provider_link_anchor)() =
    &UGC::CreateGraphicsFactory;

const Json* required_object(
    const Json& object, const std::string& name, const std::string& context) {
  const auto value = object.find(name);
  if (value == object.end() || !value->is_object()) {
    throw std::invalid_argument(context + "." + name + " must be an object");
  }
  return &*value;
}

std::string required_string(
    const Json& object, const std::string& name, const std::string& context) {
  const auto value = object.find(name);
  if (value == object.end() || !value->is_string() || value->get<std::string>().empty()) {
    throw std::invalid_argument(context + "." + name + " must be a non-empty string");
  }
  return value->get<std::string>();
}

std::string trim(std::string value) {
  const auto first = std::find_if_not(value.begin(), value.end(), [](unsigned char character) {
    return std::isspace(character) != 0;
  });
  const auto last = std::find_if_not(value.rbegin(), value.rend(), [](unsigned char character) {
    return std::isspace(character) != 0;
  }).base();
  return first < last ? std::string(first, last) : std::string();
}

std::string lower(std::string value) {
  std::transform(value.begin(), value.end(), value.begin(), [](unsigned char character) {
    return static_cast<char>(std::tolower(character));
  });
  return value;
}

std::string cad_format(const Json& source) {
  const std::string format = lower(trim(required_string(source, "format", "access_plan.source")));
  if (format != "dwg" && format != "dxf") {
    throw std::invalid_argument("access_plan.source.format must be dwg or dxf");
  }
  return format;
}

std::string dwg_version(const std::filesystem::path& path) {
  std::ifstream input(path, std::ios::binary);
  std::array<char, 6> header {};
  if (!input.read(header.data(), static_cast<std::streamsize>(header.size()))) {
    throw std::invalid_argument("invalid DWG header: " + path.string());
  }
  const std::string version(header.data(), header.size());
  if (version.size() != 6 || version.rfind("AC10", 0) != 0 ||
      !std::all_of(version.begin() + 4, version.end(), [](unsigned char character) {
        return std::isdigit(character) != 0;
      })) {
    throw std::invalid_argument("invalid DWG AC10xx header: " + path.string());
  }
  return version;
}

std::string dxf_version(const std::filesystem::path& path) {
  constexpr std::array<unsigned char, 22> binary_signature = {
      'A', 'u', 't', 'o', 'C', 'A', 'D', ' ', 'B', 'i', 'n', 'a', 'r', 'y', ' ', 'D',
      'X', 'F', '\r', '\n', 0x1a, 0x00};
  {
    std::ifstream binary(path, std::ios::binary);
    std::array<unsigned char, binary_signature.size()> header {};
    binary.read(
        reinterpret_cast<char*>(header.data()),
        static_cast<std::streamsize>(header.size()));
    if (header == binary_signature) {
      return "";
    }
  }

  std::ifstream input(path);
  std::string first;
  std::string second;
  if (!std::getline(input, first) || !std::getline(input, second)) {
    throw std::invalid_argument("invalid ASCII DXF header: " + path.string());
  }
  if (first.rfind("\xEF\xBB\xBF", 0) == 0) {
    first.erase(0, 3);
  }
  if (trim(first) != "0" || lower(trim(second)) != "section") {
    throw std::invalid_argument("invalid ASCII DXF SECTION header: " + path.string());
  }
  std::string line;
  for (int line_number = 2; line_number < 4096 && std::getline(input, line); ++line_number) {
    if (lower(trim(line)) != "$acadver") {
      continue;
    }
    std::string group_code;
    std::string value;
    if (std::getline(input, group_code) && std::getline(input, value) &&
        trim(group_code) == "1") {
      return trim(value);
    }
    break;
  }
  return "";
}

std::string dataset_type_name(UGC::UGDataset::DatasetType type) {
  switch (type) {
    case UGC::UGDataset::CAD:
      return "CAD";
    case UGC::UGDataset::Point:
      return "Point";
    case UGC::UGDataset::Line:
      return "Line";
    case UGC::UGDataset::Region:
      return "Region";
    case UGC::UGDataset::Text:
      return "Text";
    default:
      return std::to_string(static_cast<int>(type));
  }
}

std::string last_error_detail() {
  const auto error = UGC::UGErrorObj::GetInstance().GetLast(false);
  return "error_id=" + std::to_string(error.m_nID) + "; message=" +
      to_utf8(error.m_strMessage);
}

int optional_bounded_int(
    const Json& params,
    const std::string& name,
    int fallback,
    int minimum,
    int maximum) {
  const auto value = params.find(name);
  if (value == params.end()) {
    return fallback;
  }
  if (!value->is_number_integer()) {
    throw std::invalid_argument("params." + name + " must be an integer");
  }
  return std::clamp(value->get<int>(), minimum, maximum);
}

class TemporaryDirectory {
 public:
  explicit TemporaryDirectory(const std::string& prefix) {
    std::string pattern =
        (std::filesystem::temp_directory_path() / (prefix + "XXXXXX")).string();
    std::vector<char> writable(pattern.begin(), pattern.end());
    writable.push_back('\0');
    const char* created = ::mkdtemp(writable.data());
    if (created == nullptr) {
      throw std::runtime_error("failed to create CAD preview temporary directory");
    }
    path_ = created;
  }

  ~TemporaryDirectory() {
    std::error_code error;
    std::filesystem::remove_all(path_, error);
  }

  TemporaryDirectory(const TemporaryDirectory&) = delete;
  TemporaryDirectory& operator=(const TemporaryDirectory&) = delete;

  const std::filesystem::path& path() const noexcept { return path_; }

 private:
  std::filesystem::path path_;
};

UGC::UGDataSourcePtr open_cad_datasource(
    const std::filesystem::path& path, const std::string& alias) {
  UGC::UGDataSourcePtr datasource =
      UGC::UGDataSourceManager::CreateDataSource(UGC::UGEngineType::VectorFile);
  if (datasource == nullptr) {
    throw std::runtime_error("failed to create CAD VectorFile datasource");
  }
  UGC::UGDsConnection& connection = datasource->GetConnectionInfo();
  connection.m_nType = static_cast<UGC::UGint>(UGC::UGEngineType::VectorFile);
  connection.m_strServer = to_ug_string(path.string());
  connection.m_strAlias = to_ug_string(alias);
  connection.m_bReadOnly = true;
  if (!datasource->Open() || !datasource->IsOpen()) {
    throw std::runtime_error(
        "SuperMap failed to open CAD datasource: " + path.string() + "; " +
        last_error_detail());
  }
  return datasource;
}

void ensure_qt_graphics() {
  if (UGC::UGGraphicsManager::GetGraphicsCount() <= 0) {
    UGC::g_GraphicsManager.LoadGraphics();
  }
  const int provider_count = UGC::UGGraphicsManager::GetGraphicsCount();
  const bool selected =
      UGC::UGGraphicsManager::SetCurGraphicsType(UGC::UGGraphics::GT_QT);
  if (provider_count <= 0 || !selected) {
    throw std::runtime_error(
        "SuperMap Qt graphics provider is unavailable: count=" +
        std::to_string(provider_count) + ", selected=" + (selected ? "true" : "false"));
  }
}

void render_webp(
    UGC::UGMap& map,
    const std::filesystem::path& output,
    const UGC::UGRect2D& bounds,
    int tile_size) {
  std::filesystem::create_directories(output.parent_path());
  map.SetViewBounds(bounds);
  const bool rendered = map.OutputMapToFile(
      nullptr,
      UGC::UGSize(tile_size, tile_size),
      to_ug_string(output.string()),
      UGC::UGFileType::WEBP,
      false,
      90,
      false);
  if (!rendered || !std::filesystem::is_regular_file(output) ||
      std::filesystem::file_size(output) == 0) {
    throw std::runtime_error(
        "SuperMap failed to render CAD WebP: " + output.string() + "; " +
        last_error_detail());
  }
}

}  // namespace

Json inspect_cad(const Json& params) {
  if (!params.is_object()) {
    throw std::invalid_argument("params must be an object");
  }
  const Json& plan = *required_object(params, "access_plan", "params");
  if (required_string(plan, "schema_version", "access_plan") !=
      "addp.workflow.access-plan/v1") {
    throw std::invalid_argument("unsupported access_plan.schema_version");
  }
  const Json& source = *required_object(plan, "source", "access_plan");
  if (required_string(source, "kind", "access_plan.source") != "file") {
    throw std::invalid_argument("access_plan.source must be a CAD file");
  }
  const std::string format = cad_format(source);
  WorkflowAccessFile source_file = resolve_workflow_file(
      *required_object(source, "access", "access_plan.source"));
  const std::filesystem::path& path = source_file.path();
  if (!std::filesystem::is_regular_file(path)) {
    throw std::invalid_argument("CAD file does not exist: " + path.string());
  }
  const std::string format_version = format == "dwg" ? dwg_version(path) : dxf_version(path);

  UGC::UGDataSourcePtr datasource = open_cad_datasource(path, "cad_inspect");

  try {
    const UGC::UGint dataset_count = datasource->GetDatasetCount();
    std::int64_t record_count = 0;
    bool has_bounds = false;
    double min_x = std::numeric_limits<double>::infinity();
    double min_y = std::numeric_limits<double>::infinity();
    double max_x = -std::numeric_limits<double>::infinity();
    double max_y = -std::numeric_limits<double>::infinity();
    Json datasets = Json::array();
    for (UGC::UGint index = 0; index < dataset_count; ++index) {
      const UGC::UGDatasetPtr dataset = datasource->GetDataset(index);
      if (dataset == nullptr) {
        continue;
      }
      Json summary = {
          {"name", to_utf8(dataset->GetName())},
          {"dataset_type", dataset_type_name(dataset->GetType())},
      };
      const auto vector = std::dynamic_pointer_cast<UGC::UGDatasetVector>(dataset);
      if (vector != nullptr) {
        const UGC::UGint count = vector->GetObjectCount();
        summary["record_count"] = count;
        record_count += count;
      }
      const UGC::UGRect2D bounds = dataset->GetBounds();
      if (!bounds.IsEmpty()) {
        has_bounds = true;
        min_x = std::min(min_x, bounds.left);
        min_y = std::min(min_y, bounds.bottom);
        max_x = std::max(max_x, bounds.right);
        max_y = std::max(max_y, bounds.top);
      }
      datasets.push_back(std::move(summary));
    }

    Json drawing = {
        {"drawing_kind", "2d"},
        {"has_model_space", dataset_count > 0},
        {"layer_count", dataset_count},
    };
    if (has_bounds) {
      drawing["bounds_2d"] = {
          {"min_x", min_x}, {"min_y", min_y}, {"max_x", max_x}, {"max_y", max_y}};
    }
    Json result = {
        {"schema_version", "addp.cad.inspect/v1"},
        {"format", format},
        {"format_version", format_version},
        {"drawing", std::move(drawing)},
        {"interpretation",
         {
             {"dataset_count", dataset_count},
             {"interpreted_record_count", record_count},
             {"provider", "supermap_iobjects_cpp"},
             {"provider_version", "12.1"},
             {"normalized_geometry", true},
             {"geometry_traversed", false},
             {"scan_complete", true},
             {"datasets", std::move(datasets)},
             {"warnings", Json::array()},
         }},
    };
    datasource->Close();
    return result;
  } catch (...) {
    datasource->Close();
    throw;
  }
}

Json render_cad_preview(const Json& params) {
  if (!params.is_object()) {
    throw std::invalid_argument("params must be an object");
  }
  const Json& plan = *required_object(params, "access_plan", "params");
  if (required_string(plan, "schema_version", "access_plan") !=
      "addp.workflow.access-plan/v1") {
    throw std::invalid_argument("unsupported access_plan.schema_version");
  }
  const Json& source = *required_object(plan, "source", "access_plan");
  const Json& target = *required_object(plan, "target", "access_plan");
  if (required_string(source, "kind", "access_plan.source") != "file") {
    throw std::invalid_argument("access_plan.source must be a CAD file");
  }
  const std::string format = cad_format(source);
  if (required_string(target, "kind", "access_plan.target") != "directory" ||
      required_string(target, "format", "access_plan.target") != "cad_preview") {
    throw std::invalid_argument("access_plan.target must be directory/cad_preview");
  }
  const int tile_size = optional_bounded_int(params, "tile_size", 512, 128, 1024);
  const int max_zoom = optional_bounded_int(params, "max_zoom", 4, 0, 8);
  std::int64_t expected_tile_count = 0;
  for (int zoom = 0; zoom <= max_zoom; ++zoom) {
    const std::int64_t side = std::int64_t{1} << zoom;
    expected_tile_count += side * side;
  }
  if (expected_tile_count > 25000) {
    throw std::invalid_argument("CAD preview max_zoom produces more than 25000 tiles");
  }

  WorkflowAccessFile source_file = resolve_workflow_file(
      *required_object(source, "access", "access_plan.source"));
  const std::filesystem::path& source_path = source_file.path();
  if (!std::filesystem::is_regular_file(source_path)) {
    throw std::invalid_argument("CAD file does not exist: " + source_path.string());
  }
  if (format == "dwg") {
    static_cast<void>(dwg_version(source_path));
  } else {
    static_cast<void>(dxf_version(source_path));
  }

  TemporaryDirectory render_root("addp-supermap-cad-preview-");
  ensure_qt_graphics();
  UGC::UGDataSourcePtr datasource = open_cad_datasource(source_path, "cad_preview");
  try {
    Json result;
    {
      UGC::UGWorkspace workspace;
      UGC::UGMap map;
      map.SetWorkspace(&workspace);
      bool has_bounds = false;
      double min_x = std::numeric_limits<double>::infinity();
      double min_y = std::numeric_limits<double>::infinity();
      double max_x = -std::numeric_limits<double>::infinity();
      double max_y = -std::numeric_limits<double>::infinity();
      const UGC::UGint dataset_count = datasource->GetDatasetCount();
      for (UGC::UGint index = 0; index < dataset_count; ++index) {
        const UGC::UGDatasetPtr dataset = datasource->GetDataset(index);
        if (dataset == nullptr) {
          continue;
        }
        if (map.m_Layers.AddDataset(dataset, true) == nullptr) {
          throw std::runtime_error(
              "SuperMap failed to add CAD dataset to map; " + last_error_detail());
        }
        const UGC::UGRect2D bounds = dataset->GetBounds();
        if (!bounds.IsEmpty()) {
          has_bounds = true;
          min_x = std::min(min_x, bounds.left);
          min_y = std::min(min_y, bounds.bottom);
          max_x = std::max(max_x, bounds.right);
          max_y = std::max(max_y, bounds.top);
        }
      }
      if (!has_bounds || max_x <= min_x || max_y <= min_y) {
        throw std::runtime_error("CAD datasource has no renderable 2D bounds");
      }

      const double span = std::max(max_x - min_x, max_y - min_y);
      const double center_x = (min_x + max_x) / 2.0;
      const double center_y = (min_y + max_y) / 2.0;
      const UGC::UGRect2D render_bounds(
          center_x - span / 2.0,
          center_y + span / 2.0,
          center_x + span / 2.0,
          center_y - span / 2.0);
      map.SetInflateBounds(false);
      UGC::UGStyle background;
      background.SetFillForeColor(UGRGB(30, 30, 30));
      background.SetFillBackColor(UGRGB(30, 30, 30));
      background.SetFillBackOpaque(true);
      background.SetFillOpaqueRate(100);
      map.SetPaintBackground(true);
      map.SetBkStyle(background);

      render_webp(
          map, render_root.path() / "thumbnail.webp", render_bounds, tile_size);
      std::int64_t tile_count = 0;
      for (int zoom = 0; zoom <= max_zoom; ++zoom) {
        const int side = 1 << zoom;
        const double tile_width = span / side;
        const double tile_height = span / side;
        for (int x = 0; x < side; ++x) {
          for (int y = 0; y < side; ++y) {
            const double left = render_bounds.left + x * tile_width;
            const double right = left + tile_width;
            const double top = render_bounds.top - y * tile_height;
            const double bottom = top - tile_height;
            render_webp(
                map,
                render_root.path() / "model-space" / std::to_string(zoom) /
                    std::to_string(x) / (std::to_string(y) + ".webp"),
                UGC::UGRect2D(left, top, right, bottom),
                tile_size);
            ++tile_count;
          }
        }
      }

      const Json bounds = {
          {"min_x", render_bounds.left},
          {"min_y", render_bounds.bottom},
          {"max_x", render_bounds.right},
          {"max_y", render_bounds.top},
      };
      const Json manifest = {
          {"schema_version", "addp.cad.preview-manifest/v1"},
          {"tile_size", tile_size},
          {"min_zoom", 0},
          {"max_zoom", max_zoom},
          {"tile_format", "webp"},
          {"tile_template", "model-space/{z}/{x}/{y}.webp"},
          {"bounds_2d", bounds},
          {"drawing_bounds_2d",
           {{"min_x", min_x}, {"min_y", min_y}, {"max_x", max_x}, {"max_y", max_y}}},
          {"spaces",
           Json::array({{{"id", "model-space"},
                         {"kind", "model_space"},
                         {"title", "Model Space"}}})},
      };
      std::ofstream manifest_file(render_root.path() / "manifest.json");
      if (!manifest_file || !(manifest_file << manifest.dump(2) << '\n')) {
        throw std::runtime_error("failed to write CAD preview manifest");
      }
      manifest_file.close();
      publish_workflow_directory(
          render_root.path(),
          *required_object(target, "access", "access_plan.target"),
          target.value("write_mode", "create"));
      result = {
          {"schema_version", "addp.cad.render-preview/v1"},
          {"format", format},
          {"manifest_ref", "manifest.json"},
          {"thumbnail_ref", "thumbnail.webp"},
          {"tile_count", tile_count},
          {"dataset_count", dataset_count},
          {"bounds_2d", bounds},
      };
    }
    datasource->Close();
    return result;
  } catch (...) {
    datasource->Close();
    throw;
  }
}

}  // namespace addp::supermap
