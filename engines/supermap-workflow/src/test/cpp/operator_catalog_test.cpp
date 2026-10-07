#include "operator_catalog.hpp"

#include <cstdlib>
#include <fstream>
#include <iostream>
#include <set>
#include <string>

namespace {

void require(bool value, const std::string& message) {
  if (!value) {
    std::cerr << "FAILED: " << message << '\n';
    std::exit(1);
  }
}

}  // namespace

int main(int argc, char** argv) {
  require(argc == 2, "operator catalog path argument");
  const auto catalog = addp::workflow::OperatorCatalog::load(argv[1]);
  std::ifstream catalog_file(argv[1]);
  const auto document = addp::workflow::Json::parse(catalog_file);
  require(document.at("count") == catalog.descriptors().size(), "declared catalog count matches operators");
  const std::set<std::string> expected = {
      "dataset.info",
      "dataset.project",
      "dataset.save",
      "dataset.select",
      "datasource.create",
      "datasource.enable_postgis",
      "datasource.enable_postgresql",
      "datasource.open",
      "datasource.open_postgis",
      "datasource.open_postgresql",
      "datasource.upgrade_udbx",
      "osgb_scene_to_s3m",
      "sgm_to_osgb",
      "overlay.clip",
      "overlay.erase",
      "overlay.intersect",
      "overlay.union",
      "table.delete",
      "table.read_batch",
      "table.read_close",
      "table.read_open",
      "table.write_abort",
      "table.write_batch",
      "table.write_close",
      "table.write_open",
      "table.write_prepare",
      "vector.buffer",
      "vector.dissolve",
      "vector.feature_envelope",
      "vector.filter",
      "vector.inner_point",
      "vector.merge",
      "vector.query",
      "vector.spatial_filter",
  };

  std::set<std::string> actual;
  for (const auto& descriptor : catalog.descriptors()) {
    actual.insert(descriptor.at("id").get<std::string>());
  }
  require(actual == expected, "catalog keeps all 34 C++ runtime operators");
  require(catalog.default_output_port("datasource.open") == "datasource", "default port");
  require(catalog.supports_mode("osgb_scene_to_s3m", "workflow"), "S3M workflow mode");
  require(catalog.supports_mode("osgb_scene_to_s3m", "direct"), "S3M direct mode");
  require(catalog.supports_mode("sgm_to_osgb", "workflow"), "SGM workflow mode");
  require(catalog.supports_mode("sgm_to_osgb", "direct"), "SGM direct mode");
  require(!catalog.supports_mode("table.read_open", "workflow"), "table sessions are direct-only");
  return 0;
}
