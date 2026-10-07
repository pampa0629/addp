#include "sgm_runtime.hpp"
#include "runtime_access.hpp"
#include "FileParser/UGFileParseManager.h"
#include "FileParser/UGFileParseModel.h"
#include "FileParser/UGModelConfigParams.h"
#include "FileParser/UGSgmConfigParams.h"
#include <filesystem>
#include <memory>
#include <stdexcept>
#include <utility>

namespace addp::supermap {
namespace {
using addp::workflow::Json;
struct ParserDeleter {
  void operator()(UGC::UGFileParser* parser) const {
    parser->Close();
    UGC::UGFileParseManager::DestroyFileParser(parser);
  }
};
using Parser = std::unique_ptr<UGC::UGFileParser, ParserDeleter>;
class OSGBExportParams final : public UGC::UGModelExportParams {
 public:
  UGC::UGint GetFileType() const override { return UGC::UGFileType::ModelOSG; }
};
UGC::UGString sdk_path(const std::filesystem::path& path) {
  UGC::UGString result;
  result.FromStd(path.string(), OGDC::OGDCCharset::UTF8);
  return result;
}
UGC::UGFileParseModel& model_parser(const Parser& parser) {
  auto* model = dynamic_cast<UGC::UGFileParseModel*>(parser.get());
  if (!model) throw std::runtime_error("SuperMap model parser is unavailable");
  model->SetParseModelNode(true);
  model->SetParseSkeleton(true);
  model->SetReadOnlyShell(false);
  return *model;
}
std::pair<UGC::UGint, UGC::UGint> geometry_counts(UGC::UGModelNode& node) {
  UGC::UGint vertices = 0, triangles = 0;
  if (!node.GetObjectCount(-1, vertices, triangles) || vertices <= 0 || triangles <= 0)
    throw std::runtime_error("SuperMap model does not contain a non-empty mesh");
  return {vertices, triangles};
}
const Json& object(const Json& value, const char* name) {
  const auto found = value.find(name);
  if (found == value.end() || !found->is_object())
    throw std::invalid_argument(std::string(name) + " must be an object");
  return *found;
}
}

Json convert_sgm_to_osgb(const Json& params) {
  const auto& plan = object(params, "access_plan");
  if (plan.value("schema_version", "") != "addp.workflow.access-plan/v1")
    throw std::invalid_argument("unsupported access_plan.schema_version");
  const auto& source = object(plan, "source");
  const auto& target = object(plan, "target");
  if (source.value("kind", "") != "file" || source.value("format", "") != "sgm")
    throw std::invalid_argument("access_plan.source must be file/sgm");
  if (target.value("kind", "") != "file" || target.value("format", "") != "osgb")
    throw std::invalid_argument("access_plan.target must be file/osgb");
  const auto mode = target.value("write_mode", "create");
  if (mode != "create" && mode != "replace")
    throw std::invalid_argument("target write_mode must be create or replace");
  const auto& target_access = object(target, "access");
  if (target_access.value("method", "") != "mounted_path" &&
      target_access.value("method", "") != "object_store")
    throw std::invalid_argument("SGM target access must be mounted_path or object_store");
  auto input = resolve_workflow_file(object(source, "access"));
  if (!std::filesystem::is_regular_file(input.path()))
    throw std::invalid_argument("SGM source must be a regular file");
  auto output = make_workflow_temporary_file("model.osgb");
  Parser reader(UGC::UGFileParseManager::CreateFileParser(UGC::UGFileType::SGM));
  auto& model = model_parser(reader);
  UGC::UGSgmImportParams import_params;
  import_params.SetFilePathName(sdk_path(input.path()));
  if (!reader->Open(import_params)) throw std::runtime_error("SuperMap could not open SGM source");
  std::unique_ptr<UGC::UGModelNode> node(model.GetModelNode());
  if (!node) throw std::runtime_error("SGM source has no model node");
  const auto counts = geometry_counts(*node);
  Parser writer(UGC::UGFileParseManager::CreateFileParser(UGC::UGFileType::ModelOSG));
  auto& export_model = model_parser(writer);
  OSGBExportParams export_params;
  export_params.SetFilePathName(sdk_path(output.path()));
  if (!export_model.InitWiter() || !export_model.Save(export_params, node.get()))
    throw std::runtime_error("SuperMap SGM to OSGB export failed");
  if (!std::filesystem::is_regular_file(output.path()) || std::filesystem::file_size(output.path()) == 0)
    throw std::runtime_error("SuperMap produced no OSGB output");
  Parser verifier(UGC::UGFileParseManager::CreateFileParser(UGC::UGFileType::ModelOSG));
  auto& verify_model = model_parser(verifier);
  UGC::UGModelImportParams verify_params;
  verify_params.SetFilePathName(sdk_path(output.path()));
  if (!verifier->Open(verify_params)) throw std::runtime_error("OSGB output cannot be reopened");
  std::unique_ptr<UGC::UGModelNode> verified(verify_model.GetModelNode());
  if (!verified || geometry_counts(*verified) != counts)
    throw std::runtime_error("OSGB output geometry count differs from SGM source");
  const auto size = std::filesystem::file_size(output.path());
  publish_workflow_file(output.path(), target_access, mode);
  Json result{{"format", "osgb"}, {"vertex_count", counts.first}, {"triangle_count", counts.second},
              {"size_bytes", size}, {"publish", {{"uploaded_files", 1}, {"uploaded_bytes", size}}}};
  if (target_access.value("method", "") == "object_store") {
    result["osgb_ref"] = target_access.at("object");
    result["osgb_uri"] = "s3://" + target_access.at("bucket").get<std::string>() + "/" + target_access.at("object").get<std::string>();
  } else {
    result["osgb_uri"] = target_access.at("path");
  }
  return result;
}
}
