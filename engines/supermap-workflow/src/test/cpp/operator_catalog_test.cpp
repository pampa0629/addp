#include "operator_catalog.hpp"

#include <cstdlib>
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
  const std::set<std::string> expected = {
      "cad.inspect",
      "cad.render_preview",
      "dataset.info",
      "dataset.project",
      "dataset.save",
      "dataset.select",
      "datasource.create",
      "datasource.enable_postgis",
      "datasource.open",
      "datasource.open_postgis",
      "datasource.upgrade_udbx",
      "osgb_scene_to_s3m",
      "overlay.clip",
      "overlay.erase",
      "overlay.intersect",
      "overlay.union",
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
  require(actual == expected, "catalog keeps all 24 Java runtime operators");
  require(catalog.default_output_port("datasource.open") == "datasource", "default port");
  require(catalog.supports_mode("osgb_scene_to_s3m", "workflow"), "S3M workflow mode");
  require(catalog.supports_mode("osgb_scene_to_s3m", "direct"), "S3M direct mode");
  require(!catalog.supports_mode("cad.inspect", "workflow"), "CAD inspect is direct-only");
  return 0;
}
