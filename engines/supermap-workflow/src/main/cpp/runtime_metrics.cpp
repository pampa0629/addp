// The metrics listener and its renewable declaration belong to this server process.
#include <httplib.h>
#include <nlohmann/json.hpp>
#include <openssl/ssl.h>
#include <unistd.h>

#include <chrono>
#include <cstdlib>
#include <cstdio>
#include <ctime>
#include <fstream>
#include <iostream>
#include <string>
#include <thread>

namespace {
using Clock = std::chrono::system_clock;
using namespace std::chrono_literals;

std::string setting(const char* name) {
  const auto* value = std::getenv(name);
  return value ? value : "";
}

std::string utc(Clock::time_point started) {
  const auto whole = std::chrono::floor<std::chrono::seconds>(started);
  const auto seconds = Clock::to_time_t(whole);
  std::tm parts{};
  gmtime_r(&seconds, &parts);
  char buffer[32]{};
  auto size = std::strftime(buffer, sizeof(buffer), "%Y-%m-%dT%H:%M:%S", &parts);
  const auto fraction = std::chrono::duration_cast<std::chrono::microseconds>(started - whole).count();
  std::snprintf(buffer + size, sizeof(buffer) - size, ".%06dZ", static_cast<int>(fraction));
  return buffer;
}

void run_source(const std::string& endpoint, Clock::time_point started) {
  const auto tls = setting("ADDP_SUPERMAP_METRICS_TLS_DIR");
  const auto id = setting("ADDP_SUPERMAP_METRICS_ENGINE_ID");
  const auto secret = setting("SUPERMAP_METRICS_SERVICE_CLIENT_SECRET");
  if (id.empty() || id.front() == '0' || id.find_first_not_of("0123456789") != std::string::npos ||
      secret.empty() || tls.empty()) throw std::runtime_error("missing deployment");
  std::string instance;
  std::ifstream("/proc/sys/kernel/random/uuid") >> instance;
  if (instance.size() != 36) throw std::runtime_error("missing instance");
  const auto cert = tls + "/server.crt", key = tls + "/server.key", ca = tls + "/ca.crt";
  httplib::SSLServer source(cert.c_str(), key.c_str(), ca.c_str());
  if (!source.is_valid()) throw std::runtime_error("private listener unavailable");
  SSL_CTX_set_verify(source.ssl_context(), SSL_VERIFY_PEER | SSL_VERIFY_FAIL_IF_NO_PEER_CERT, nullptr);
  SSL_CTX_set_min_proto_version(source.ssl_context(), TLS1_2_VERSION);
  source.new_task_queue = [] { return new httplib::ThreadPool(1); };
  source.set_read_timeout(3,0);
  source.set_write_timeout(3,0);
  source.Get("/metrics", [started](const httplib::Request& request, httplib::Response& response) {
    if (request.target != "/metrics" || !request.body.empty()) {response.status = 400; return;}
    const auto now = Clock::now();
    const auto uptime = std::chrono::duration_cast<std::chrono::seconds>(now-started).count();
    response.set_header("Cache-Control","no-store");
    response.set_content("addp_supermap_observed_at_seconds "+std::to_string(Clock::to_time_t(now))+
        "\naddp_supermap_uptime_seconds "+std::to_string(uptime)+"\n","text/plain; version=0.0.4");
  });
  char hostname[256]{};
  const int port = std::stoi(setting("ADDP_SUPERMAP_METRICS_CONTAINER_PORT"));
  if (port < 1 || port > 65535 || gethostname(hostname,sizeof(hostname)-1) != 0 ||
      !source.bind_to_port(hostname,port)) throw std::runtime_error("cannot bind private source");

  const auto claim = nlohmann::json{{"started_at",utc(started)},{"instance_id",instance},{"endpoint",endpoint}}.dump();
  const std::string path = "/api/v1/system/runtime/engines/"+id+"/metrics";
  const auto system = setting("SYSTEM_URL");
  httplib::Client issuer(system), registry(system);
  for (auto* client : {&issuer, &registry}) {
    client->set_connection_timeout(3,0);
    client->set_read_timeout(5,0);
    client->set_write_timeout(3,0);
  }
  issuer.set_basic_auth("addp-supermap",secret);
  std::jthread renew([&](std::stop_token stop) {
    std::string token;
    auto expires = Clock::time_point{};
    while (!stop.stop_requested()) {
      try {
        if (token.empty() || Clock::now()+30s >= expires) {
          const auto grant = issuer.Post("/api/v1/system/oauth/token",
            "grant_type=client_credentials&scope=addp.api&audience=addp.api&context_type=platform",
            "application/x-www-form-urlencoded");
          if (grant && grant->status==200) {
            const auto body=nlohmann::json::parse(grant->body);
            token=body.at("access_token").get<std::string>();
            expires=Clock::now()+std::chrono::seconds(body.at("expires_in").get<int>());
          } else { token.clear(); }
        }
        if (!token.empty()) {
          registry.set_bearer_token_auth(token);
          const auto result=registry.Put(path,claim,"application/json");
          if (!result || result->status!=204) {
            if (result && result->status==401) token.clear();
            std::cerr<<"SuperMap metrics lease unavailable\n";
          }
        }
      } catch (const std::exception&) {std::cerr<<"SuperMap metrics lease unavailable\n";}
      for (int i=0;i<30 && !stop.stop_requested();++i) std::this_thread::sleep_for(1s);
    }
  });
  source.listen_after_bind();
}
}

void start_supermap_metrics() {
  const auto endpoint=setting("ADDP_SUPERMAP_METRICS_ENDPOINT");
  if (endpoint.empty()) return;
  std::thread([endpoint, started=Clock::now()] {
    try {run_source(endpoint,started);}
    catch (const std::exception&) {std::cerr<<"SuperMap private metrics unavailable\n";}
  }).detach();
}
