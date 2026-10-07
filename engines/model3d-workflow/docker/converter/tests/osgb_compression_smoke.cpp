#include <osg/Geode>
#include <osg/Geometry>
#include <osg/Texture2D>
#include <osgDB/Registry>
#include <osgDB/WriteFile>
#include <nlohmann/json.hpp>
#include <cstdint>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>
#include <memory>
#include <algorithm>
#define STB_IMAGE_IMPLEMENTATION
#include <stb_image.h>

USE_OSGPLUGIN(osg)
USE_OSGPLUGIN(osg2)
USE_SERIALIZER_WRAPPER_LIBRARY(osg)
USE_COMPRESSOR_WRAPPER(ZLibCompressor)

// Own deterministic geometry: no vendor sample, SDK, or license is required.
int main(int argc, char** argv) {
  try {
    if (argc != 3 && argc != 4) throw std::runtime_error("expected generate|verify, file path and optional texture format");
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
      if (argc == 4) {
        const std::string format = argv[3];
        const GLenum pixel_format = format == "dxt1" ? GL_COMPRESSED_RGB_S3TC_DXT1_EXT :
          format == "dxt1a" ? GL_COMPRESSED_RGBA_S3TC_DXT1_EXT :
          format == "dxt3" ? GL_COMPRESSED_RGBA_S3TC_DXT3_EXT :
          format == "dxt5" ? GL_COMPRESSED_RGBA_S3TC_DXT5_EXT : 0;
        if (!pixel_format) throw std::runtime_error("unknown fixture texture format");
        const int block_bytes = format == "dxt1" || format == "dxt1a" ? 8 : 16;
        auto* pixels = new unsigned char[4 * block_bytes]{};
        for (int block = 0; block < 4; ++block) {
          auto* data = pixels + block * block_bytes;
          if (format == "dxt3") std::fill(data, data + 8, 255);
          if (format == "dxt5") data[0] = 255;
          auto* color = data + block_bytes - 8;
          color[0] = 0; color[1] = 0xf8; // RGB565 red endpoint, all indices zero.
          color[2] = 0xe0; color[3] = 0x07;
        }
        osg::ref_ptr<osg::Image> image = new osg::Image;
        image->setImage(8, 8, 1, pixel_format, pixel_format, GL_UNSIGNED_BYTE,
                        pixels, osg::Image::USE_NEW_DELETE, 1);
        osg::ref_ptr<osg::Texture2D> texture = new osg::Texture2D(image.get());
        geometry->getOrCreateStateSet()->setTextureAttributeAndModes(0, texture.get(), osg::StateAttribute::ON);
        osg::ref_ptr<osg::Vec2Array> uv = new osg::Vec2Array;
        uv->push_back(osg::Vec2(0, 0)); uv->push_back(osg::Vec2(1, 0)); uv->push_back(osg::Vec2(0, 1));
        geometry->setTexCoordArray(0, uv.get());
      }
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
      if (argc == 4) {
        if (doc.at("images").size() != 1) throw std::runtime_error("GLB must retain one embedded texture");
        const auto& material = doc.at("materials").at(primitive.at("material").get<std::size_t>());
        const auto texture = material.at("pbrMetallicRoughness").at("baseColorTexture").at("index").get<std::size_t>();
        if (doc.at("textures").at(texture).at("source") != 0)
          throw std::runtime_error("GLB material must reference the compressed texture");
        const auto& image = doc.at("images").at(0);
        const auto& view = doc.at("bufferViews").at(image.at("bufferView").get<std::size_t>());
        std::uint32_t binary_header[2]{};
        input.read(reinterpret_cast<char*>(binary_header), sizeof(binary_header));
        if (!input || binary_header[1] != 0x004e4942) throw std::runtime_error("missing GLB binary chunk");
        std::string binary(binary_header[0], '\0'); input.read(binary.data(), binary.size());
        const auto offset = view.value("byteOffset", std::size_t(0));
        const auto length = view.at("byteLength").get<std::size_t>();
        if (!input || offset > binary.size() || length > binary.size() - offset)
          throw std::runtime_error("invalid GLB image buffer range");
        int width = 0, height = 0, channels = 0;
        std::unique_ptr<unsigned char, decltype(&stbi_image_free)> pixels(
          stbi_load_from_memory(reinterpret_cast<const unsigned char*>(binary.data() + offset),
                                static_cast<int>(length), &width, &height, &channels, 3), stbi_image_free);
        if (!pixels || width != 8 || height != 8) throw std::runtime_error("compressed texture dimensions changed");
        for (int i = 0; i < width * height; ++i)
          if (pixels.get()[i * 3] < 200 || pixels.get()[i * 3 + 1] > 60 || pixels.get()[i * 3 + 2] > 60)
            throw std::runtime_error("compressed red texture became a placeholder or lost its color");
        std::cout << argv[3] << " texture pixel regression passed\n";
      }
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
