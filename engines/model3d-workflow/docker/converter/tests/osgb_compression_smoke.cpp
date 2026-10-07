#include <osg/Geode>
#include <osg/Geometry>
#include <osgDB/Registry>
#include <osgDB/WriteFile>
#include <nlohmann/json.hpp>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>

USE_OSGPLUGIN(osg)
USE_OSGPLUGIN(osg2)
USE_SERIALIZER_WRAPPER_LIBRARY(osg)
USE_COMPRESSOR_WRAPPER(ZLibCompressor)

// Own deterministic geometry: no vendor sample, SDK, or license is required.
int main(int argc, char** argv) {
  try {
    if (argc != 3) throw std::runtime_error("expected generate|verify and file path");
    if (std::string(argv[1]) == "generate") {
      osg::ref_ptr<osg::Vec3Array> vertices = new osg::Vec3Array;
      vertices->push_back(osg::Vec3(0, 0, 0));
      vertices->push_back(osg::Vec3(1, 0, 0));
      vertices->push_back(osg::Vec3(0, 1, 0));
      osg::ref_ptr<osg::Geometry> geometry = new osg::Geometry;
      geometry->setVertexArray(vertices.get());
      osg::ref_ptr<osg::DrawElementsUInt> indices = new osg::DrawElementsUInt(GL_TRIANGLES);
      indices->push_back(0); indices->push_back(1); indices->push_back(2);
      geometry->addPrimitiveSet(indices.get());
      osg::ref_ptr<osg::Geode> node = new osg::Geode;
      node->addDrawable(geometry.get());
      osg::ref_ptr<osgDB::Options> options = new osgDB::Options("Compressor=zlib");
      if (!osgDB::writeNodeFile(*node, argv[2], options.get()))
        throw std::runtime_error("failed to generate compressed OSGB fixture");
      std::ifstream input(argv[2], std::ios::binary);
      std::string prefix(28, '\0'); input.read(prefix.data(), prefix.size());
      if (!input || prefix.substr(24, 4) != "zlib")
        throw std::runtime_error("OSGB fixture must declare zlib compression");
    } else if (std::string(argv[1]) == "verify") {
      std::ifstream input(argv[2], std::ios::binary);
      std::uint32_t header[5]{};
      input.read(reinterpret_cast<char*>(header), sizeof(header));
      if (!input || header[0] != 0x46546c67 || header[1] != 2 || header[4] != 0x4e4f534a)
        throw std::runtime_error("conversion did not produce GLB 2.0");
      std::string payload(header[3], '\0'); input.read(payload.data(), payload.size());
      if (!input) throw std::runtime_error("truncated GLB JSON chunk");
      const auto doc = nlohmann::json::parse(payload);
      if (doc.at("asset").at("version") != "2.0" || doc.at("meshes").size() != 1)
        throw std::runtime_error("invalid GLB scene");
      const auto& primitive = doc.at("meshes").at(0).at("primitives").at(0);
      const auto& accessors = doc.at("accessors");
      if (primitive.value("mode", 4) != 4 ||
          accessors.at(primitive.at("attributes").at("POSITION").get<std::size_t>()).at("count") != 3 ||
          accessors.at(primitive.at("indices").get<std::size_t>()).at("count") != 3)
        throw std::runtime_error("GLB must retain one triangle and three vertices");
      std::cout << "Compressed OSGB to GLB 2.0 smoke passed" << std::endl;
    } else {
      throw std::runtime_error("unknown smoke operation");
    }
    return 0;
  } catch (const std::exception& error) {
    std::cerr << error.what() << std::endl;
    return 1;
  }
}
