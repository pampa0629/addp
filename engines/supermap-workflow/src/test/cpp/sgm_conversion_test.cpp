#include "operator_runtime.hpp"
#include "runtime_access.hpp"
#include "Toolkit/UGErrorObj.h"
#include "Toolkit/UGLicense.h"
#include <filesystem>
#include <fstream>
#include <iostream>
#include <stdexcept>

using addp::workflow::Json;
void require(bool value, const char* message) {
  if (!value) throw std::runtime_error(message);
}
int main(int argc, char** argv) {
  try {
    require(argc == 3, "catalog and SDK SGM sample are required");
    UGC::UGErrorObj::GetInstance().Startup();
    require(UGC::UGLicense::VerifyLicense(UGLicense_iObjectsCppCore), "Core license unavailable");
    addp::supermap::OperatorRuntime runtime(addp::workflow::OperatorCatalog::load(argv[1]));
    auto target = addp::supermap::make_workflow_temporary_file("target.osgb");
    Json params{{"access_plan", {
      {"schema_version", "addp.workflow.access-plan/v1"},
      {"source", {{"kind", "file"}, {"format", "sgm"}, {"access", {{"method", "mounted_path"}, {"path", argv[2]}}}}},
      {"target", {{"kind", "file"}, {"format", "osgb"}, {"write_mode", "create"}, {"access", {{"method", "mounted_path"}, {"path", target.path().string()}}}}}
    }}};
    auto result = runtime.invoke_direct("sgm_to_osgb", params);
    require(result.at("vertex_count") == 136 && result.at("triangle_count") == 94, "sample geometry differs");
    const auto bytes = std::filesystem::file_size(target.path());
    require(bytes > 0 && result.at("size_bytes") == bytes, "published size differs");
    bool rejected = false;
    try { runtime.invoke_direct("sgm_to_osgb", params); } catch (const std::exception&) { rejected = true; }
    require(rejected && std::filesystem::file_size(target.path()) == bytes, "create must preserve existing output");
    params["access_plan"]["target"]["write_mode"] = "replace";
    runtime.invoke_direct("sgm_to_osgb", params);
    require(std::filesystem::file_size(target.path()) == bytes, "replace output differs");
    const auto workflow = runtime.execute_workflow("sgm-workflow-test", Json{
      {"workflow_def", {{"tasks", Json::array({{
        {"id", "convert"}, {"operator", "sgm_to_osgb"}, {"params", params}, {"depends_on", Json::array()}
      }})}}},
      {"runtime", {{"execution_authorization", {{"id", 1}, {"effects", Json::array({"read", "write"})}}}}}
    });
    require(workflow.at("status") == "success" && workflow.at("final_result").at("triangle_count") == 94,
            "workflow conversion must use the same native path");
    const auto before_invalid_bytes = std::filesystem::file_size(target.path());
    auto invalid = addp::supermap::make_workflow_temporary_file("invalid.sgm");
    { std::ofstream file(invalid.path()); file << "invalid SGM"; }
    params["access_plan"]["source"]["access"]["path"] = invalid.path().string();
    rejected = false;
    try { runtime.invoke_direct("sgm_to_osgb", params); } catch (const std::exception&) { rejected = true; }
    require(rejected, "invalid SGM source must be rejected after repeated valid reads");
    require(std::filesystem::file_size(target.path()) == before_invalid_bytes, "invalid source must not replace output");
    params["access_plan"]["source"]["format"] = "skp";
    rejected = false;
    try { runtime.invoke_direct("sgm_to_osgb", params); } catch (const std::exception&) { rejected = true; }
    require(rejected, "operator must reject other source formats");
    std::cout << "SGM direct conversion and publication boundaries passed\n";
    return 0;
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n'; return 1;
  }
}
