#include "operator_runtime.hpp"
#include "runtime_access.hpp"
#include "Toolkit/UGErrorObj.h"
#include "Toolkit/UGLicense.h"
#include <filesystem>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <memory>
#include <map>
#include <mutex>
#include <set>
#include <sstream>
#include <thread>
#include <vector>
#include <arpa/inet.h>
#include <sys/socket.h>
#include <unistd.h>
#include "FileParser/UGFileParseManager.h"
#include "FileParser/UGFileParseModel.h"
#include "FileParser/UGModelConfigParams.h"

using addp::workflow::Json;
void require(bool value, const char* message) {
  if (!value) throw std::runtime_error(message);
}
std::string read_bytes(const std::filesystem::path& path) {
  std::ifstream input(path, std::ios::binary);
  return {std::istreambuf_iterator<char>(input), std::istreambuf_iterator<char>()};
}
std::set<std::filesystem::path> temporary_files() {
  std::set<std::filesystem::path> result;
  for (const auto& entry : std::filesystem::directory_iterator(std::filesystem::temp_directory_path())) {
    const auto name = entry.path().filename().string();
    if (name.find("addp-supermap-workflow-file-") == 0 || name.find("addp-supermap-workflow-output-") == 0)
      result.insert(entry.path());
  }
  return result;
}
void verify_texture(const std::filesystem::path& path) {
  auto* parser = UGC::UGFileParseManager::CreateFileParser(UGC::UGFileType::ModelOSG);
  require(parser != nullptr, "OSGB parser unavailable");
  struct Cleanup {
    UGC::UGFileParser* parser;
    ~Cleanup() { parser->Close(); UGC::UGFileParseManager::DestroyFileParser(parser); }
  } cleanup{parser};
  auto* model = dynamic_cast<UGC::UGFileParseModel*>(parser);
  require(model != nullptr, "OSGB model parser unavailable");
  model->SetParseModelNode(true);
  model->SetParseSkeleton(true);
  model->SetReadOnlyShell(false);
  UGC::UGModelImportParams params;
  UGC::UGString sdk_path;
  sdk_path.FromStd(path.string(), OGDC::OGDCCharset::UTF8);
  params.SetFilePathName(sdk_path);
  require(parser->Open(params), "published OSGB rejected");
  std::unique_ptr<UGC::UGModelNode> node(model->GetModelNode());
  require(node != nullptr, "published OSGB node missing");
  std::map<UGC::UGString, UGC::UGModelTexturePtr> textures;
  node->GetMapTextureData(textures);
  require(textures.size() == 1, "published OSGB must embed one texture after staging cleanup");
  const auto* data = textures.begin()->second->m_pTextureData;
  require(data && data->m_pBuffer && data->m_nWidth == 256 && data->m_nHeight == 256 && data->m_nSize > 0,
          "published OSGB must retain compass texture pixels");
}
// A loopback HTTP fixture exercises the real signed S3 download path, not MinIO authorization.
class ObjectFixture {
 public:
  std::map<std::string, std::pair<int, std::string>> objects;
  int port;
  ObjectFixture() {
    socket_ = socket(AF_INET, SOCK_STREAM, 0);
    require(socket_ >= 0, "fixture socket unavailable");
    sockaddr_in address{};
    address.sin_family = AF_INET;
    address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    require(bind(socket_, reinterpret_cast<sockaddr*>(&address), sizeof(address)) == 0,
            "fixture bind failed");
    socklen_t length = sizeof(address);
    require(getsockname(socket_, reinterpret_cast<sockaddr*>(&address), &length) == 0,
            "fixture port unavailable");
    port = ntohs(address.sin_port);
    require(listen(socket_, 8) == 0, "fixture listen failed");
    worker_ = std::thread([this] { serve(); });
  }
  ~ObjectFixture() { shutdown(socket_, SHUT_RDWR); close(socket_); worker_.join(); }
  void configure(std::map<std::string, std::pair<int, std::string>> next) {
    std::lock_guard<std::mutex> lock(mutex_);
    objects = std::move(next);
    requests_.clear();
  }
  std::vector<std::string> requests() {
    std::lock_guard<std::mutex> lock(mutex_);
    return requests_;
  }
 private:
  int socket_;
  std::thread worker_;
  std::mutex mutex_;
  std::vector<std::string> requests_;
  void serve() {
    while (true) {
      const int client = accept(socket_, nullptr, nullptr);
      if (client < 0) return;
      timeval timeout{10, 0};
      setsockopt(client, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout));
      std::string request;
      char buffer[4096];
      while (request.find("\r\n\r\n") == std::string::npos && request.size() < 32768) {
        const auto size = recv(client, buffer, sizeof(buffer), 0);
        if (size <= 0) break;
        request.append(buffer, static_cast<std::size_t>(size));
      }
      std::istringstream first_line(request);
      std::string method, path;
      first_line >> method >> path;
      std::pair<int, std::string> response{404, "missing"};
      {
        std::lock_guard<std::mutex> lock(mutex_);
        requests_.push_back(method + " " + path);
        const auto found = objects.find(path);
        if (found != objects.end()) response = found->second;
      }
      if (method != "GET" || request.find("AWS4-HMAC-SHA256") == std::string::npos)
        response = {400, "unsigned or unsupported request"};
      const std::string output = "HTTP/1.1 " + std::to_string(response.first) + " Fixture\r\nContent-Length: " +
          std::to_string(response.second.size()) + "\r\nConnection: close\r\n\r\n" + response.second;
      std::size_t sent = 0;
      while (sent < output.size()) {
        const auto size = send(client, output.data() + sent, output.size() - sent, MSG_NOSIGNAL);
        if (size <= 0) break;
        sent += static_cast<std::size_t>(size);
      }
      close(client);
    }
  }
};
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
    const auto replaced = runtime.invoke_direct("sgm_to_osgb", params);
    require(replaced.at("size_bytes") == std::filesystem::file_size(target.path()) &&
            replaced.at("vertex_count") == 136 && replaced.at("triangle_count") == 94,
            "replace output differs");
    verify_texture(target.path());
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
    // The SDK sample itself has a companion image; isolate the model to expose missing-texture loss.
    auto isolated = addp::supermap::make_workflow_temporary_file("source.SGM");
    std::filesystem::copy_file(argv[2], isolated.path());
    const auto image = std::filesystem::path(argv[2]).parent_path() / "Textures/compass.png";
    const auto image_bytes = read_bytes(image);
    params["access_plan"]["source"]["access"] = {{"method", "mounted_path"}, {"path", isolated.path().string()}};
    const auto stable_output = read_bytes(target.path());
    auto reject_preserving_output = [&](const char* message) {
      const auto before = temporary_files();
      bool failed = false;
      try { runtime.invoke_direct("sgm_to_osgb", params); }
      catch (const std::exception&) { failed = true; }
      require(failed, message);
      require(read_bytes(target.path()) == stable_output, "rejected conversion must preserve existing output bytes");
      require(temporary_files() == before, "rejected conversion must remove staged files");
    };
    auto convert_textured = [&] {
      const auto before = temporary_files();
      const auto converted = runtime.invoke_direct("sgm_to_osgb", params);
      require(converted.at("vertex_count") == 136 && converted.at("triangle_count") == 94,
              "textured source geometry differs");
      require(temporary_files() == before, "successful conversion must remove staged files");
      verify_texture(target.path());
    };
    reject_preserving_output("missing NFS companion image must fail");
    std::filesystem::create_directories(isolated.path().parent_path() / "Textures");
    std::filesystem::copy_file(image, isolated.path().parent_path() / "Textures/compass.png");
    convert_textured();
    // Restore the baseline output before each rejection check.
    { std::ofstream output(target.path(), std::ios::binary); output << stable_output; }
    { std::ofstream bad(isolated.path().parent_path() / "compass.png"); bad << "corrupt image"; }
    reject_preserving_output("corrupt adjacent image must not fall back to Textures");
    std::filesystem::remove(isolated.path().parent_path() / "compass.png");
    std::filesystem::create_symlink(image, isolated.path().parent_path() / "compass.png");
    reject_preserving_output("NFS image symlink outside model directory must fail");
    std::filesystem::remove(isolated.path().parent_path() / "compass.png");
    std::filesystem::copy_file(image, isolated.path().parent_path() / "compass.png");
    { std::ofstream bad(isolated.path().parent_path() / "Textures/compass.png"); bad << "corrupt fallback"; }
    convert_textured();
    { std::ofstream output(target.path(), std::ios::binary); output << stable_output; }
    ObjectFixture fixture;
    params["access_plan"]["source"]["access"] = {
      {"method", "object_store"}, {"endpoint", "http://127.0.0.1:" + std::to_string(fixture.port)},
      {"access_key", "fixture-key"}, {"secret_key", "fixture-secret"},
      {"bucket", "samples"}, {"object", "models/source.SGM"}
    };
    const std::string model_object = "/samples/models/source.SGM";
    const std::string adjacent = "/samples/models/compass.png";
    const std::string fallback = "/samples/models/Textures/compass.png";
    const auto model_bytes = read_bytes(isolated.path());
    fixture.configure({{model_object, {200, model_bytes}}, {fallback, {200, image_bytes}}});
    convert_textured();
    require(fixture.requests() == std::vector<std::string>{"GET " + model_object, "GET " + adjacent, "GET " + fallback},
            "S3 fallback must download only the exact model and two candidates in order");
    fixture.configure({{model_object, {200, model_bytes}}, {adjacent, {200, image_bytes}}, {fallback, {200, "corrupt"}}});
    convert_textured();
    require(fixture.requests() == std::vector<std::string>{"GET " + model_object, "GET " + adjacent},
            "S3 adjacent image must prevent fallback reads");
    { std::ofstream output(target.path(), std::ios::binary); output << stable_output; }
    fixture.configure({{model_object, {200, model_bytes}}});
    reject_preserving_output("missing S3 companion image must fail");
    for (const int status : {403, 500}) {
      fixture.configure({{model_object, {200, model_bytes}}, {adjacent, {status, "denied"}}, {fallback, {200, image_bytes}}});
      reject_preserving_output("S3 access or server error must fail without fallback");
      require(fixture.requests() == std::vector<std::string>{"GET " + model_object, "GET " + adjacent},
              "only a 404 may advance to the fallback object");
    }
    fixture.configure({{model_object, {200, model_bytes}}, {adjacent, {200, "corrupt"}}, {fallback, {200, image_bytes}}});
    reject_preserving_output("corrupt S3 companion image must fail");
    require(fixture.requests() == std::vector<std::string>{"GET " + model_object, "GET " + adjacent},
            "corrupt adjacent image must not trigger a fallback download");
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
