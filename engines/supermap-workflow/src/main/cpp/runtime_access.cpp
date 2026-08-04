#include "runtime_access.hpp"

#include "Base3D/UGTinyxml.h"

#include <curl/curl.h>

#include <array>
#include <algorithm>
#include <cerrno>
#include <cctype>
#include <cstdint>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <iomanip>
#include <mutex>
#include <spawn.h>
#include <sstream>
#include <stdexcept>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>
#include <utility>
#include <vector>

extern char** environ;

namespace addp::supermap {
namespace {

using addp::workflow::Json;

struct ProcessResult {
  int exit_code;
  std::string output;
};

struct ObjectStoreConfig {
  std::string endpoint;
  std::string credentials;
  std::string region;
  std::string bucket;
};

std::string connection_string(
    const Json& connection_info, const std::string& field, bool required) {
  const auto value = connection_info.find(field);
  if (value == connection_info.end() || value->is_null()) {
    if (required) {
      throw std::invalid_argument("connection_info." + field + " is required");
    }
    return "";
  }
  if (!value->is_string() || (required && value->get<std::string>().empty())) {
    throw std::invalid_argument(
        "connection_info." + field + " must be a non-empty string");
  }
  return value->get<std::string>();
}

std::string normalize_resource_host(const std::string& host) {
  if (host != "localhost" && host != "127.0.0.1" && host != "::1") {
    return host;
  }
  const char* alias = std::getenv("SUPERMAP_RESOURCE_LOCALHOST_ALIAS");
  return alias == nullptr || std::string(alias).empty() ? host : std::string(alias);
}

bool connection_bool(const Json& connection_info, const std::string& field, bool fallback) {
  const auto value = connection_info.find(field);
  if (value == connection_info.end() || value->is_null()) {
    return fallback;
  }
  if (!value->is_boolean()) {
    throw std::invalid_argument("connection_info." + field + " must be a boolean");
  }
  return value->get<bool>();
}

std::string trim_slashes(std::string value) {
  std::replace(value.begin(), value.end(), '\\', '/');
  const std::size_t first = value.find_first_not_of('/');
  if (first == std::string::npos) {
    return "";
  }
  const std::size_t last = value.find_last_not_of('/');
  return value.substr(first, last - first + 1);
}

std::string percent_encode_path(const std::string& value) {
  static constexpr char hexadecimal[] = "0123456789ABCDEF";
  std::string result;
  result.reserve(value.size());
  for (const unsigned char character : value) {
    const bool unreserved =
        (character >= 'a' && character <= 'z') ||
        (character >= 'A' && character <= 'Z') ||
        (character >= '0' && character <= '9') || character == '-' || character == '_' ||
        character == '.' || character == '~';
    if (unreserved || character == '/') {
      result.push_back(static_cast<char>(character));
    } else {
      result.push_back('%');
      result.push_back(hexadecimal[character >> 4]);
      result.push_back(hexadecimal[character & 0x0f]);
    }
  }
  return result;
}

std::string percent_encode_query(const std::string& value) {
  static constexpr char hexadecimal[] = "0123456789ABCDEF";
  std::string result;
  result.reserve(value.size());
  for (const unsigned char character : value) {
    const bool unreserved =
        (character >= 'a' && character <= 'z') ||
        (character >= 'A' && character <= 'Z') ||
        (character >= '0' && character <= '9') || character == '-' || character == '_' ||
        character == '.' || character == '~';
    if (unreserved) {
      result.push_back(static_cast<char>(character));
    } else {
      result.push_back('%');
      result.push_back(hexadecimal[character >> 4]);
      result.push_back(hexadecimal[character & 0x0f]);
    }
  }
  return result;
}

std::string normalize_object_store_endpoint(const Json& access) {
  std::string endpoint = connection_string(access, "endpoint", true);
  if (endpoint.find("://") == std::string::npos) {
    endpoint = connection_bool(access, "use_ssl", false) ? "https://" + endpoint
                                                          : "http://" + endpoint;
  }
  CURLU* parsed = curl_url();
  if (parsed == nullptr) {
    throw std::runtime_error("failed to initialize object_store endpoint parser");
  }
  const auto cleanup = [&] { curl_url_cleanup(parsed); };
  if (curl_url_set(parsed, CURLUPART_URL, endpoint.c_str(), 0) != CURLUE_OK) {
    cleanup();
    throw std::invalid_argument("invalid object_store endpoint");
  }
  char* scheme = nullptr;
  char* host = nullptr;
  char* path = nullptr;
  if (curl_url_get(parsed, CURLUPART_SCHEME, &scheme, 0) != CURLUE_OK ||
      curl_url_get(parsed, CURLUPART_HOST, &host, 0) != CURLUE_OK) {
    curl_free(scheme);
    curl_free(host);
    cleanup();
    throw std::invalid_argument("invalid object_store endpoint host");
  }
  const std::string scheme_text = scheme;
  const std::string host_text = host;
  curl_free(scheme);
  curl_free(host);
  if (scheme_text != "http" && scheme_text != "https") {
    cleanup();
    throw std::invalid_argument("object_store endpoint scheme must be http or https");
  }
  for (const CURLUPart part : {CURLUPART_USER, CURLUPART_PASSWORD, CURLUPART_QUERY,
                              CURLUPART_FRAGMENT}) {
    char* unexpected = nullptr;
    if (curl_url_get(parsed, part, &unexpected, 0) == CURLUE_OK) {
      const bool present = unexpected != nullptr && unexpected[0] != '\0';
      curl_free(unexpected);
      if (present) {
        cleanup();
        throw std::invalid_argument(
            "object_store endpoint must not contain credentials, query, or fragment");
      }
    }
  }
  if (curl_url_get(parsed, CURLUPART_PATH, &path, 0) == CURLUE_OK) {
    const std::string path_text = path;
    curl_free(path);
    if (!path_text.empty() && path_text != "/") {
      cleanup();
      throw std::invalid_argument("object_store endpoint must not contain a path");
    }
  }
  const std::string normalized_host = normalize_resource_host(host_text);
  if (curl_url_set(parsed, CURLUPART_HOST, normalized_host.c_str(), 0) != CURLUE_OK ||
      curl_url_set(parsed, CURLUPART_PATH, "/", 0) != CURLUE_OK) {
    cleanup();
    throw std::invalid_argument("failed to normalize object_store endpoint");
  }
  char* normalized = nullptr;
  if (curl_url_get(parsed, CURLUPART_URL, &normalized, CURLU_NO_DEFAULT_PORT) != CURLUE_OK) {
    cleanup();
    throw std::invalid_argument("failed to normalize object_store endpoint URL");
  }
  std::string result = normalized;
  curl_free(normalized);
  cleanup();
  while (!result.empty() && result.back() == '/') {
    result.pop_back();
  }
  return result;
}

ObjectStoreConfig object_store_config(const Json& access) {
  const std::string access_key = connection_string(access, "access_key", true);
  const std::string secret_key = connection_string(access, "secret_key", true);
  const std::string bucket = trim_slashes(connection_string(access, "bucket", true));
  if (bucket.empty() || bucket.find('/') != std::string::npos) {
    throw std::invalid_argument("object_store bucket must be a non-empty bucket name");
  }
  std::string region = connection_string(access, "region", false);
  if (region.empty()) {
    region = "us-east-1";
  }
  return {
      normalize_object_store_endpoint(access),
      access_key + ":" + secret_key,
      std::move(region),
      bucket,
  };
}

void ensure_curl_initialized() {
  static std::once_flag init_once;
  std::call_once(init_once, [] {
    if (curl_global_init(CURL_GLOBAL_DEFAULT) != CURLE_OK) {
      throw std::runtime_error("failed to initialize libcurl");
    }
  });
}

size_t discard_response(char* data, size_t size, size_t count, void*) {
  static_cast<void>(data);
  return size * count;
}

size_t append_response(char* data, size_t size, size_t count, void* target) {
  const std::size_t bytes = size * count;
  static_cast<std::string*>(target)->append(data, bytes);
  return bytes;
}

void configure_s3_request(
    CURL* request,
    const ObjectStoreConfig& config,
    const std::string& object,
    char* error_buffer) {
  const std::string url = config.endpoint + "/" + percent_encode_path(config.bucket) + "/" +
      percent_encode_path(object);
  const std::string signature = "aws:amz:" + config.region + ":s3";
  curl_easy_setopt(request, CURLOPT_URL, url.c_str());
  curl_easy_setopt(request, CURLOPT_USERPWD, config.credentials.c_str());
  curl_easy_setopt(request, CURLOPT_AWS_SIGV4, signature.c_str());
  curl_easy_setopt(request, CURLOPT_ERRORBUFFER, error_buffer);
  curl_easy_setopt(request, CURLOPT_FAILONERROR, 1L);
  curl_easy_setopt(request, CURLOPT_NOSIGNAL, 1L);
  curl_easy_setopt(request, CURLOPT_CONNECTTIMEOUT, 30L);
}

void check_s3_result(CURL* request, CURLcode code, const char* error_buffer, const char* action) {
  long status = 0;
  curl_easy_getinfo(request, CURLINFO_RESPONSE_CODE, &status);
  if (code != CURLE_OK || status < 200 || status >= 300) {
    const std::string detail = error_buffer[0] == '\0' ? curl_easy_strerror(code) : error_buffer;
    throw std::runtime_error(
        std::string("object_store ") + action + " failed: HTTP " +
        std::to_string(status) + "; " + detail);
  }
}

void download_object(
    const ObjectStoreConfig& config,
    const std::string& object,
    const std::filesystem::path& destination) {
  ensure_curl_initialized();
  CURL* request = curl_easy_init();
  if (request == nullptr) {
    throw std::runtime_error("failed to initialize object_store download");
  }
  std::filesystem::create_directories(destination.parent_path());
  FILE* output = std::fopen(destination.c_str(), "wb");
  if (output == nullptr) {
    curl_easy_cleanup(request);
    throw std::runtime_error("failed to create materialized workflow file");
  }
  std::array<char, CURL_ERROR_SIZE> error_buffer {};
  try {
    configure_s3_request(request, config, object, error_buffer.data());
    curl_easy_setopt(request, CURLOPT_WRITEDATA, output);
    const CURLcode code = curl_easy_perform(request);
    std::fclose(output);
    output = nullptr;
    check_s3_result(request, code, error_buffer.data(), "download");
    curl_easy_cleanup(request);
  } catch (...) {
    if (output != nullptr) {
      std::fclose(output);
    }
    curl_easy_cleanup(request);
    std::error_code error;
    std::filesystem::remove(destination, error);
    throw;
  }
}

void upload_object(
    const ObjectStoreConfig& config,
    const std::string& object,
    const std::filesystem::path& source) {
  ensure_curl_initialized();
  CURL* request = curl_easy_init();
  if (request == nullptr) {
    throw std::runtime_error("failed to initialize object_store upload");
  }
  FILE* input = std::fopen(source.c_str(), "rb");
  if (input == nullptr) {
    curl_easy_cleanup(request);
    throw std::runtime_error("failed to open workflow artifact for upload");
  }
  std::array<char, CURL_ERROR_SIZE> error_buffer {};
  try {
    configure_s3_request(request, config, object, error_buffer.data());
    curl_easy_setopt(request, CURLOPT_UPLOAD, 1L);
    curl_easy_setopt(request, CURLOPT_READDATA, input);
    curl_easy_setopt(
        request,
        CURLOPT_INFILESIZE_LARGE,
        static_cast<curl_off_t>(std::filesystem::file_size(source)));
    curl_easy_setopt(request, CURLOPT_WRITEFUNCTION, discard_response);
    const CURLcode code = curl_easy_perform(request);
    std::fclose(input);
    input = nullptr;
    check_s3_result(request, code, error_buffer.data(), "upload");
    curl_easy_cleanup(request);
  } catch (...) {
    if (input != nullptr) {
      std::fclose(input);
    }
    curl_easy_cleanup(request);
    throw;
  }
}

std::vector<std::string> list_objects(
    const ObjectStoreConfig& config, const std::string& prefix) {
  ensure_curl_initialized();
  std::vector<std::string> result;
  std::string continuation_token;
  while (true) {
    CURL* request = curl_easy_init();
    if (request == nullptr) {
      throw std::runtime_error("failed to initialize object_store list");
    }
    std::array<char, CURL_ERROR_SIZE> error_buffer {};
    std::string response;
    try {
      std::string url =
          config.endpoint + "/" + percent_encode_path(config.bucket) +
          "/?list-type=2&prefix=" + percent_encode_query(prefix);
      if (!continuation_token.empty()) {
        url += "&continuation-token=" + percent_encode_query(continuation_token);
      }
      const std::string signature = "aws:amz:" + config.region + ":s3";
      curl_easy_setopt(request, CURLOPT_URL, url.c_str());
      curl_easy_setopt(request, CURLOPT_USERPWD, config.credentials.c_str());
      curl_easy_setopt(request, CURLOPT_AWS_SIGV4, signature.c_str());
      curl_easy_setopt(request, CURLOPT_ERRORBUFFER, error_buffer.data());
      curl_easy_setopt(request, CURLOPT_FAILONERROR, 1L);
      curl_easy_setopt(request, CURLOPT_NOSIGNAL, 1L);
      curl_easy_setopt(request, CURLOPT_CONNECTTIMEOUT, 30L);
      curl_easy_setopt(request, CURLOPT_WRITEFUNCTION, append_response);
      curl_easy_setopt(request, CURLOPT_WRITEDATA, &response);
      const CURLcode code = curl_easy_perform(request);
      check_s3_result(request, code, error_buffer.data(), "list");
      curl_easy_cleanup(request);
      request = nullptr;

      UGTiXmlDocument document;
      document.Parse(response.c_str(), nullptr, TIXML_ENCODING_UTF8);
      if (document.Error()) {
        throw std::runtime_error(
            "failed to parse object_store list response: " +
            std::string(document.ErrorDesc()));
      }
      const UGTiXmlElement* root = document.RootElement();
      if (root == nullptr) {
        throw std::runtime_error("object_store list response is empty");
      }
      for (const UGTiXmlElement* contents = root->FirstChildElement("Contents");
           contents != nullptr;
           contents = contents->NextSiblingElement("Contents")) {
        const UGTiXmlElement* key = contents->FirstChildElement("Key");
        if (key != nullptr && key->GetText() != nullptr) {
          result.emplace_back(key->GetText());
        }
      }
      const UGTiXmlElement* truncated = root->FirstChildElement("IsTruncated");
      if (truncated == nullptr || truncated->GetText() == nullptr ||
          std::string(truncated->GetText()) != "true") {
        return result;
      }
      const UGTiXmlElement* next = root->FirstChildElement("NextContinuationToken");
      if (next == nullptr || next->GetText() == nullptr ||
          std::string(next->GetText()).empty()) {
        throw std::runtime_error(
            "object_store list response is truncated without a continuation token");
      }
      continuation_token = next->GetText();
    } catch (...) {
      if (request != nullptr) {
        curl_easy_cleanup(request);
      }
      throw;
    }
  }
}

void delete_object(const ObjectStoreConfig& config, const std::string& object) {
  ensure_curl_initialized();
  CURL* request = curl_easy_init();
  if (request == nullptr) {
    throw std::runtime_error("failed to initialize object_store delete");
  }
  std::array<char, CURL_ERROR_SIZE> error_buffer {};
  try {
    configure_s3_request(request, config, object, error_buffer.data());
    curl_easy_setopt(request, CURLOPT_CUSTOMREQUEST, "DELETE");
    curl_easy_setopt(request, CURLOPT_WRITEFUNCTION, discard_response);
    const CURLcode code = curl_easy_perform(request);
    check_s3_result(request, code, error_buffer.data(), "delete");
    curl_easy_cleanup(request);
  } catch (...) {
    curl_easy_cleanup(request);
    throw;
  }
}

std::filesystem::path create_temporary_directory(const std::string& prefix) {
  std::string pattern =
      (std::filesystem::temp_directory_path() / (prefix + "XXXXXX")).string();
  std::vector<char> writable(pattern.begin(), pattern.end());
  writable.push_back('\0');
  const char* created = ::mkdtemp(writable.data());
  if (created == nullptr) {
    throw std::runtime_error("failed to create workflow materialization directory");
  }
  return created;
}

std::string stable_hash(const std::string& value) {
  constexpr std::uint64_t offset_basis = 14695981039346656037ULL;
  constexpr std::uint64_t prime = 1099511628211ULL;
  std::uint64_t hash = offset_basis;
  for (const unsigned char character : value) {
    hash ^= character;
    hash *= prime;
  }
  std::ostringstream result;
  result << std::hex << std::setfill('0') << std::setw(16) << hash;
  return result.str();
}

std::filesystem::path normalize_nfs_relative_path(const std::string& path) {
  if (path.empty()) {
    throw std::invalid_argument("params.path is required for NFS UDBX output");
  }
  std::string normalized_text = path;
  for (char& character : normalized_text) {
    if (character == '\\') {
      character = '/';
    }
  }
  if (normalized_text.front() == '/' || normalized_text.find("://") != std::string::npos) {
    throw std::invalid_argument(
        "NFS UDBX output path must be relative to the selected ADDP NFS root: " + path);
  }
  const std::filesystem::path normalized =
      std::filesystem::path(normalized_text).lexically_normal();
  const auto first = normalized.begin();
  if (normalized.empty() || normalized.is_absolute() ||
      (first != normalized.end() && *first == "..")) {
    throw std::invalid_argument(
        "NFS UDBX output path escapes the selected ADDP NFS root: " + path);
  }
  return normalized;
}

bool is_mount_point(const std::filesystem::path& path) {
  struct stat current {};
  struct stat parent {};
  if (::stat(path.c_str(), &current) != 0 || ::stat(path.parent_path().c_str(), &parent) != 0) {
    return false;
  }
  return current.st_dev != parent.st_dev ||
      (current.st_dev == parent.st_dev && current.st_ino == parent.st_ino);
}

ProcessResult run_process(const std::vector<std::string>& arguments) {
  int output_pipe[2];
  if (::pipe(output_pipe) != 0) {
    throw std::runtime_error("failed to create mount output pipe: " + std::string(std::strerror(errno)));
  }

  posix_spawn_file_actions_t actions;
  posix_spawn_file_actions_init(&actions);
  posix_spawn_file_actions_adddup2(&actions, output_pipe[1], STDOUT_FILENO);
  posix_spawn_file_actions_adddup2(&actions, output_pipe[1], STDERR_FILENO);
  posix_spawn_file_actions_addclose(&actions, output_pipe[0]);
  posix_spawn_file_actions_addclose(&actions, output_pipe[1]);

  std::vector<char*> argv;
  argv.reserve(arguments.size() + 1);
  for (const std::string& argument : arguments) {
    argv.push_back(const_cast<char*>(argument.c_str()));
  }
  argv.push_back(nullptr);

  pid_t pid = -1;
  const int spawn_error =
      posix_spawnp(&pid, argv.front(), &actions, nullptr, argv.data(), environ);
  posix_spawn_file_actions_destroy(&actions);
  ::close(output_pipe[1]);
  if (spawn_error != 0) {
    ::close(output_pipe[0]);
    throw std::runtime_error(
        "failed to start mount command: " + std::string(std::strerror(spawn_error)));
  }

  std::string output;
  std::array<char, 4096> buffer {};
  while (true) {
    const ssize_t count = ::read(output_pipe[0], buffer.data(), buffer.size());
    if (count > 0) {
      output.append(buffer.data(), static_cast<std::size_t>(count));
      continue;
    }
    if (count < 0 && errno == EINTR) {
      continue;
    }
    break;
  }
  ::close(output_pipe[0]);

  int status = 0;
  while (::waitpid(pid, &status, 0) < 0) {
    if (errno != EINTR) {
      throw std::runtime_error("failed to wait for mount command");
    }
  }
  return {
      WIFEXITED(status) ? WEXITSTATUS(status) : 128,
      std::move(output),
  };
}

std::vector<std::string> nfs_versions(const std::string& configured) {
  return configured.empty() ? std::vector<std::string>{"4", "3"}
                            : std::vector<std::string>{configured};
}

void ensure_nfs_mounted(
    const std::string& server,
    const std::string& export_path,
    const std::string& configured_version,
    const std::filesystem::path& mount_root) {
  static std::mutex mount_mutex;
  const std::lock_guard<std::mutex> lock(mount_mutex);
  std::filesystem::create_directories(mount_root);
  if (is_mount_point(mount_root)) {
    return;
  }

  std::vector<std::string> attempts;
  for (const std::string& version : nfs_versions(configured_version)) {
    const std::string options = "vers=" + version + ",tcp,nolock,proto=tcp";
    const ProcessResult result = run_process({
        "mount", "-t", "nfs", "-o", options, server + ":" + export_path,
        mount_root.string(),
    });
    attempts.push_back(
        "options=" + options + ", exit=" + std::to_string(result.exit_code) +
        ", output=" + result.output);
    if (result.exit_code == 0 || is_mount_point(mount_root)) {
      return;
    }
  }

  std::ostringstream message;
  message << "failed to dynamically mount NFS export " << server << ':' << export_path
          << " to " << mount_root
          << ". The SuperMap workflow container must include nfs-common and run with mount "
             "permission. mount attempts: ";
  for (std::size_t index = 0; index < attempts.size(); ++index) {
    if (index > 0) {
      message << " | ";
    }
    message << attempts[index];
  }
  throw std::runtime_error(message.str());
}

}  // namespace

std::filesystem::path resolve_udbx_path(const Json& connection_info, const std::string& path) {
  if (!connection_info.is_object()) {
    throw std::invalid_argument("connection_info must be an object");
  }
  std::string engine_type = connection_string(connection_info, "engine_type", false);
  std::transform(
      engine_type.begin(), engine_type.end(), engine_type.begin(),
      [](unsigned char character) { return static_cast<char>(std::tolower(character)); });
  if (engine_type != "nfs") {
    return std::filesystem::path(path);
  }

  const std::string server = normalize_resource_host(
      connection_string(connection_info, "server", true));
  const std::string export_path = connection_string(connection_info, "export_path", true);
  std::string version = connection_string(connection_info, "nfs_version", false);
  if (version.empty()) {
    version = connection_string(connection_info, "version", false);
  }
  const std::filesystem::path relative_path = normalize_nfs_relative_path(path);
  const char* configured_base = std::getenv("SUPERMAP_DYNAMIC_NFS_MOUNT_BASE");
  const std::filesystem::path mount_base =
      configured_base == nullptr || std::string(configured_base).empty()
      ? std::filesystem::path("/mnt/addp-dynamic-nfs")
      : std::filesystem::path(configured_base);
  const std::filesystem::path mount_root =
      mount_base / stable_hash(server + "|" + export_path);
  ensure_nfs_mounted(server, export_path, version, mount_root);
  return (mount_root / relative_path).lexically_normal();
}

std::filesystem::path resolve_workflow_mounted_path(const Json& access) {
  if (!access.is_object()) {
    throw std::invalid_argument("workflow access must be an object");
  }
  if (connection_string(access, "method", true) != "mounted_path") {
    throw std::invalid_argument("workflow access method must be mounted_path");
  }
  const std::filesystem::path configured_path =
      std::filesystem::path(connection_string(access, "path", true)).lexically_normal();
  const std::string server = connection_string(access, "server", false);
  const std::string export_path_text = connection_string(access, "export_path", false);
  if (server.empty() || export_path_text.empty()) {
    return configured_path;
  }

  const std::filesystem::path export_path =
      std::filesystem::path(export_path_text).lexically_normal();
  const std::filesystem::path relative = configured_path.lexically_relative(export_path);
  const auto first = relative.begin();
  if (relative.empty() || relative.is_absolute() ||
      (first != relative.end() && *first == "..")) {
    throw std::invalid_argument(
        "mounted_path is outside the declared NFS export: " + configured_path.string());
  }

  const std::string normalized_server = normalize_resource_host(server);
  std::string version = connection_string(access, "nfs_version", false);
  const char* configured_base = std::getenv("SUPERMAP_DYNAMIC_NFS_MOUNT_BASE");
  const std::filesystem::path mount_base =
      configured_base == nullptr || std::string(configured_base).empty()
      ? std::filesystem::path("/mnt/addp-dynamic-nfs")
      : std::filesystem::path(configured_base);
  const std::filesystem::path mount_root =
      mount_base / stable_hash(normalized_server + "|" + export_path_text);
  ensure_nfs_mounted(
      normalized_server, export_path_text, version, mount_root);
  return (mount_root / relative).lexically_normal();
}

WorkflowAccessFile::WorkflowAccessFile(
    std::filesystem::path path, std::filesystem::path temporary_root)
    : path_(std::move(path)), temporary_root_(std::move(temporary_root)) {}

WorkflowAccessFile::~WorkflowAccessFile() {
  if (!temporary_root_.empty()) {
    std::error_code error;
    std::filesystem::remove_all(temporary_root_, error);
  }
}

WorkflowAccessFile::WorkflowAccessFile(WorkflowAccessFile&& other) noexcept
    : path_(std::move(other.path_)), temporary_root_(std::move(other.temporary_root_)) {
  other.temporary_root_.clear();
}

WorkflowAccessFile& WorkflowAccessFile::operator=(WorkflowAccessFile&& other) noexcept {
  if (this != &other) {
    if (!temporary_root_.empty()) {
      std::error_code error;
      std::filesystem::remove_all(temporary_root_, error);
    }
    path_ = std::move(other.path_);
    temporary_root_ = std::move(other.temporary_root_);
    other.temporary_root_.clear();
  }
  return *this;
}

const std::filesystem::path& WorkflowAccessFile::path() const noexcept {
  return path_;
}

WorkflowAccessFile resolve_workflow_file(const Json& access) {
  const std::string method = connection_string(access, "method", true);
  if (method == "mounted_path") {
    return WorkflowAccessFile(resolve_workflow_mounted_path(access));
  }
  if (method == "object_store") {
    const ObjectStoreConfig config = object_store_config(access);
    const std::string object = trim_slashes(connection_string(access, "object", true));
    const std::filesystem::path filename = std::filesystem::path(object).filename();
    if (object.empty() || filename.empty() || filename == "." || filename == "..") {
      throw std::invalid_argument("object_store workflow source requires a file object");
    }
    const std::filesystem::path temporary_root =
        create_temporary_directory("addp-supermap-workflow-file-");
    const std::filesystem::path local_file = temporary_root / filename;
    try {
      download_object(config, object, local_file);
      return WorkflowAccessFile(local_file, temporary_root);
    } catch (...) {
      std::error_code error;
      std::filesystem::remove_all(temporary_root, error);
      throw;
    }
  }
  throw std::invalid_argument(
      "workflow file access method must be mounted_path or object_store");
}

void publish_workflow_directory(
    const std::filesystem::path& source_root,
    const Json& access,
    const std::string& write_mode) {
  if (!std::filesystem::is_directory(source_root)) {
    throw std::invalid_argument("workflow publish source must be a directory");
  }
  if (write_mode != "create" && write_mode != "replace") {
    throw std::invalid_argument("workflow directory write_mode must be create or replace");
  }
  const std::string method = connection_string(access, "method", true);
  if (method == "object_store") {
    const ObjectStoreConfig config = object_store_config(access);
    const std::string prefix = trim_slashes(connection_string(access, "prefix", false));
    if (prefix.empty()) {
      throw std::invalid_argument("object_store directory target requires a non-empty prefix");
    }
    const std::vector<std::string> existing = list_objects(config, prefix + "/");
    if (write_mode == "create" && !existing.empty()) {
      throw std::invalid_argument("object_store directory target already exists: " + prefix);
    }
    if (write_mode == "replace") {
      for (const std::string& object : existing) {
        delete_object(config, object);
      }
    }
    for (const auto& entry : std::filesystem::recursive_directory_iterator(source_root)) {
      if (!entry.is_regular_file()) {
        continue;
      }
      const std::string relative = entry.path().lexically_relative(source_root).generic_string();
      const std::string object = prefix.empty() ? relative : prefix + "/" + relative;
      upload_object(config, object, entry.path());
    }
    return;
  }
  if (method != "mounted_path") {
    throw std::invalid_argument(
        "workflow directory access method must be mounted_path or object_store");
  }
  const std::filesystem::path target_root = resolve_workflow_mounted_path(access);
  if (target_root.empty() || target_root == target_root.root_path()) {
    throw std::invalid_argument("refusing to publish workflow directory to a filesystem root");
  }
  if (write_mode == "create" && std::filesystem::exists(target_root)) {
    throw std::invalid_argument(
        "workflow directory target already exists: " + target_root.string());
  }
  std::error_code error;
  if (write_mode == "replace") {
    std::filesystem::remove_all(target_root, error);
  }
  if (error) {
    throw std::runtime_error(
        "failed to replace workflow target directory " + target_root.string() + ": " +
        error.message());
  }
  std::filesystem::create_directories(target_root);
  for (const auto& entry : std::filesystem::recursive_directory_iterator(source_root)) {
    const std::filesystem::path relative = entry.path().lexically_relative(source_root);
    const std::filesystem::path target = target_root / relative;
    if (entry.is_directory()) {
      std::filesystem::create_directories(target);
    } else if (entry.is_regular_file()) {
      std::filesystem::create_directories(target.parent_path());
      std::filesystem::copy_file(
          entry.path(), target, std::filesystem::copy_options::overwrite_existing);
    }
  }
}

}  // namespace addp::supermap
