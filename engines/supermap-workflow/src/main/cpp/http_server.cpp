#include "http_server.hpp"

#include <httplib.h>
#include <nlohmann/json.hpp>

#include <algorithm>
#include <stdexcept>
#include <string>

namespace addp::supermap {
namespace {

void send_json(httplib::Response& response, int status, const std::string& body) {
  response.status = status;
  response.set_content(body, "application/json; charset=utf-8");
}

void send_runtime_response(
    httplib::Response& response,
    AddpRuntimeResponse runtime_response) {
  if (runtime_response.body == nullptr) {
    send_json(
        response,
        500,
        R"({"status":"failed","error_code":"EXECUTION_FAILED","error":"SuperMap runtime returned an empty response"})");
  } else {
    response.status = runtime_response.status;
    response.set_content(
        runtime_response.body,
        runtime_response.body_size,
        "application/json; charset=utf-8");
  }
  addp_supermap_runtime_free_response(runtime_response);
}

bool known_path_with_method_mismatch(const httplib::Request& request) {
  const std::string& path = request.path;
  if (path == "/health" || path == "/api/operators" ||
      path.rfind("/api/executions/", 0) == 0) {
    return request.method != "GET";
  }
  if (path == "/api/workflow" ||
      (path.rfind("/api/operators/", 0) == 0 && path.ends_with("/invoke"))) {
    return request.method != "POST";
  }
  return false;
}

}  // namespace

HttpServer::HttpServer(
    std::string operators_config,
    std::string sdk_root,
    int thread_count)
    : thread_count_(std::max(2, thread_count)) {
  char* error_message = nullptr;
  runtime_ = addp_supermap_runtime_create(
      operators_config.c_str(), sdk_root.c_str(), &error_message);
  if (runtime_ == nullptr) {
    const std::string message =
        error_message == nullptr ? "failed to create SuperMap runtime" : error_message;
    addp_supermap_runtime_free_string(error_message);
    throw std::runtime_error(message);
  }
}

HttpServer::~HttpServer() {
  addp_supermap_runtime_destroy(runtime_);
}

void HttpServer::listen(int port) {
  httplib::Server server;
  server.new_task_queue = [threads = thread_count_] {
    return new httplib::ThreadPool(static_cast<std::size_t>(threads));
  };

  server.Get("/health", [this](const httplib::Request&, httplib::Response& response) {
    send_runtime_response(response, addp_supermap_runtime_health(runtime_));
  });

  server.Get(
      "/api/operators",
      [this](const httplib::Request& request, httplib::Response& response) {
        const std::string category =
            request.has_param("category") ? request.get_param_value("category") : "";
        send_runtime_response(
            response,
            addp_supermap_runtime_operators(runtime_, category.c_str()));
      });

  server.Post(
      "/api/workflow",
      [this](const httplib::Request& request, httplib::Response& response) {
        send_runtime_response(
            response,
            addp_supermap_runtime_workflow(
                runtime_, request.body.data(), request.body.size()));
      });

  server.Post(
      R"(/api/operators/([^/]+)/invoke)",
      [this](const httplib::Request& request, httplib::Response& response) {
        const std::string name = request.matches[1].str();
        send_runtime_response(
            response,
            addp_supermap_runtime_invoke(
                runtime_, name.c_str(), request.body.data(), request.body.size()));
      });

  server.Get(
      R"(/api/executions/([^/]+))",
      [this](const httplib::Request& request, httplib::Response& response) {
        const std::string id = request.matches[1].str();
        send_runtime_response(
            response,
            addp_supermap_runtime_execution(runtime_, id.c_str()));
      });

  server.set_error_handler([](const httplib::Request& request, httplib::Response& response) {
    if (!response.body.empty()) {
      return;
    }
    if (known_path_with_method_mismatch(request)) {
      send_json(
          response,
          405,
          R"({"status":"failed","error_code":"METHOD_NOT_ALLOWED","error":"Method is not supported"})");
      return;
    }
    send_json(
        response,
        response.status,
        R"({"status":"failed","error_code":"NOT_FOUND","error":"Endpoint not found"})");
  });
  server.set_exception_handler(
      [](const httplib::Request&, httplib::Response& response, std::exception_ptr error) {
        std::string details = "unknown HTTP handler failure";
        try {
          if (error != nullptr) {
            std::rethrow_exception(error);
          }
        } catch (const std::exception& exception) {
          details = exception.what();
        }
        send_json(
            response,
            500,
            std::string(
                R"({"status":"failed","error_code":"EXECUTION_FAILED","error":"SuperMap HTTP handler failed","details":)") +
                nlohmann::json(details).dump() + "}");
      });

  if (!server.listen("0.0.0.0", port)) {
    throw std::runtime_error("failed to listen on port " + std::to_string(port));
  }
}

}  // namespace addp::supermap
