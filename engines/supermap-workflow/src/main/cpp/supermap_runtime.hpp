#pragma once

#include "workflow.hpp"

#include "Engine/UGDataSource.h"
#include "Engine/UGDatasetVector.h"

#include <memory>
#include <string>
#include <variant>
#include <vector>

namespace addp::supermap {

using addp::workflow::Json;

std::string to_utf8(const UGC::UGString& value);
UGC::UGString to_ug_string(const std::string& value);

struct DatasourceRef {
  std::string path;
  UGC::UGDataSourcePtr datasource;

  Json summary() const;
};

struct DatasetRef {
  std::shared_ptr<DatasourceRef> datasource_ref;
  UGC::UGDatasetVectorPtr dataset;

  Json summary() const;
};

using RuntimeValue =
    std::variant<Json, std::shared_ptr<DatasourceRef>, std::shared_ptr<DatasetRef>>;

Json summarize(const RuntimeValue& value);
Json dataset_info(const std::shared_ptr<DatasetRef>& dataset);

class ExecutionContext final {
 public:
  ExecutionContext() = default;
  ExecutionContext(const ExecutionContext&) = delete;
  ExecutionContext& operator=(const ExecutionContext&) = delete;
  ~ExecutionContext();

  std::shared_ptr<DatasourceRef> open_udbx(
      const std::string& path, const std::string& alias, bool read_only);
  std::shared_ptr<DatasourceRef> create_udbx(
      const std::string& path, const std::string& alias, bool overwrite);
  std::shared_ptr<DatasourceRef> open_postgis(
      const std::string& server,
      const std::string& database,
      const std::string& user,
      const std::string& password,
      const std::string& schema,
      const std::string& table,
      const std::string& alias,
      bool read_only);
  std::shared_ptr<DatasourceRef> enable_postgis(
      const std::string& server,
      const std::string& database,
      const std::string& user,
      const std::string& password,
      const std::string& alias);
  std::shared_ptr<DatasetRef> select_dataset(
      const std::shared_ptr<DatasourceRef>& datasource, const std::string& name);
  std::shared_ptr<DatasetRef> save_dataset(
      const std::shared_ptr<DatasetRef>& dataset,
      const std::shared_ptr<DatasourceRef>& target_datasource,
      const std::string& output_dataset_name,
      bool overwrite);
  std::shared_ptr<DatasetRef> project_dataset(
      const std::shared_ptr<DatasetRef>& dataset,
      const std::shared_ptr<DatasourceRef>& output_datasource,
      const std::string& output_dataset_name,
      int target_epsg,
      const std::string& method,
      bool overwrite);
  std::shared_ptr<DatasetRef> filter_dataset(
      const std::shared_ptr<DatasetRef>& dataset,
      const std::shared_ptr<DatasourceRef>& output_datasource,
      const std::string& output_dataset_name,
      const std::string& attribute_filter,
      bool overwrite);
  std::shared_ptr<DatasetRef> merge_datasets(
      const std::shared_ptr<DatasetRef>& primary_dataset,
      const std::shared_ptr<DatasetRef>& append_dataset,
      const std::shared_ptr<DatasourceRef>& output_datasource,
      const std::string& output_dataset_name,
      bool overwrite);
  Json query_dataset(
      const std::shared_ptr<DatasetRef>& dataset, const std::string& attribute_filter);

 private:
  std::shared_ptr<DatasourceRef> connect(
      UGC::UGEngineType engine,
      const std::string& path,
      const std::string& server,
      const std::string& database,
      const std::string& user,
      const std::string& password,
      const std::string& schema,
      const std::string& alias,
      bool read_only,
      bool create);

  std::vector<std::shared_ptr<DatasourceRef>> datasources_;
};

}  // namespace addp::supermap
