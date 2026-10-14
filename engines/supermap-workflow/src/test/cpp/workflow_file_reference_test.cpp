#include "workflow_file_reference.hpp"

#include <iostream>
#include <vector>

int main() {
  using addp::supermap::workflow_path_within;
  using addp::supermap::workflow_relative_file;
  try {
    if (workflow_relative_file("Textures\\图像.png") != "Textures/图像.png" ||
        !workflow_path_within("/private/model", "/private/model/Textures/image.png") ||
        workflow_path_within("/private/model", "/private/model-other/image.png") ||
        workflow_path_within("/private/model", "/private/image.png"))
      throw std::runtime_error("relative file boundary differs");
    const auto candidates = addp::supermap::sgm_texture_candidates("/private/model", "/private/model/Textures/image.png");
    if (candidates != std::vector<std::filesystem::path>{"image.png", "Textures/image.png"})
      throw std::runtime_error("SGM candidate order differs");
    for (const std::string reference : {"/private/image.png", "/private/model-other/image.png",
                                        "/private/model/nested/image.png", "/private/model/Textures/../image.png"}) {
      bool rejected = false;
      try { addp::supermap::sgm_texture_candidates("/private/model", reference); }
      catch (const std::invalid_argument&) { rejected = true; }
      if (!rejected) throw std::runtime_error("unsupported SGM reference was accepted");
    }
    const std::vector<std::string> invalid{
        "", "/etc/passwd", "../image.png", "Textures/../image.png", "./image.png",
        "C:\\image.png", "https://example.com/image.png", "Textures/", std::string("a\0.png", 6)};
    for (const auto& reference : invalid) {
      bool rejected = false;
      try { workflow_relative_file(reference); }
      catch (const std::invalid_argument&) { rejected = true; }
      if (!rejected) throw std::runtime_error("unsafe file reference was accepted");
    }
    return 0;
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  }
}
