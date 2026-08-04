#include "operator_runtime.hpp"

#include "Toolkit/UGErrorObj.h"
#include "Toolkit/UGLicense.h"

#include <fstream>
#include <iostream>
#include <stdexcept>
#include <string>

namespace {

addp::workflow::Json read_json(const std::string& path) {
  std::ifstream input(path);
  if (!input) {
    throw std::runtime_error("failed to open JSON file: " + path);
  }
  addp::workflow::Json result;
  input >> result;
  return result;
}

}  // namespace

int main(int argc, char** argv) {
  if (argc != 3 && argc != 5) {
    std::cerr << "usage: supermap-runtime-test-driver <operators.json> <workflow.json>\n"
              << "   or: supermap-runtime-test-driver <operators.json> --direct "
                 "<operator> <params.json>\n";
    return 64;
  }
  try {
    UGC::UGErrorObj::GetInstance().Startup();
    if (!UGC::UGLicense::VerifyLicense(UGLicense_iObjectsCppCore)) {
      throw std::runtime_error("SuperMap C++ Core license is unavailable");
    }
    addp::supermap::OperatorRuntime runtime(
        addp::workflow::OperatorCatalog::load(argv[1]));
    if (argc == 5) {
      if (std::string(argv[2]) != "--direct") {
        throw std::invalid_argument("expected --direct");
      }
      std::cout << runtime.invoke_direct(argv[3], read_json(argv[4])).dump() << '\n';
    } else {
      std::cout << runtime.execute_workflow("cpp-runtime-test", read_json(argv[2])).dump()
                << '\n';
    }
    return 0;
  } catch (const std::exception& error) {
    std::cerr << error.what() << '\n';
    return 1;
  } catch (...) {
    std::cerr << "unknown SuperMap runtime error\n";
    return 2;
  }
}
