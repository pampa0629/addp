cmake_minimum_required(VERSION 3.20)
project(addp_supermap_workflow LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

find_package(nlohmann_json REQUIRED)
find_package(Threads REQUIRED)

set(SUPERMAP_SDK_ROOT "" CACHE PATH "Path to the complete SuperMap iObjects C++ SDK")

add_library(addp_workflow_core
  src/main/cpp/operator_catalog.cpp
  src/main/cpp/workflow.cpp
)
target_include_directories(addp_workflow_core PUBLIC src/main/cpp)
target_link_libraries(addp_workflow_core PUBLIC nlohmann_json::nlohmann_json)
target_compile_options(addp_workflow_core PRIVATE -Wall -Wextra -Wpedantic -Werror)

if(SUPERMAP_SDK_ROOT)
  find_package(CURL REQUIRED)
  find_path(CPP_HTTPLIB_INCLUDE_DIR httplib.h REQUIRED)
  find_library(CPP_HTTPLIB_LIBRARY cpp-httplib REQUIRED)
  if(NOT EXISTS "${SUPERMAP_SDK_ROOT}/include/Engine/UGDataSource.h")
    message(FATAL_ERROR "SUPERMAP_SDK_ROOT does not contain the iObjects C++ headers")
  endif()
  if(NOT EXISTS "${SUPERMAP_SDK_ROOT}/bin/bin/libSuEngine.so")
    message(FATAL_ERROR "SUPERMAP_SDK_ROOT does not contain the iObjects C++ runtime libraries")
  endif()

  target_compile_definitions(addp_workflow_core PUBLIC _GLIBCXX_USE_CXX11_ABI=0)

  add_library(addp_supermap_runtime
    src/main/cpp/cad_runtime.cpp
    src/main/cpp/operator_runtime.cpp
    src/main/cpp/runtime_access.cpp
    src/main/cpp/s3m_runtime.cpp
    src/main/cpp/supermap_runtime.cpp
    src/main/cpp/table_session_runtime.cpp
    src/main/cpp/udbx_runtime.cpp
  )
  target_include_directories(addp_supermap_runtime PUBLIC src/main/cpp)
  target_include_directories(addp_supermap_runtime SYSTEM PUBLIC
    "${SUPERMAP_SDK_ROOT}/include"
    "${SUPERMAP_SDK_ROOT}/include/private"
    ${CURL_INCLUDE_DIRS}
  )
  target_compile_definitions(
    addp_supermap_runtime PUBLIC _GLIBCXX_USE_CXX11_ABI=0 _UGUNICODE
  )
  target_compile_options(addp_supermap_runtime PUBLIC -fshort-wchar)
  target_compile_options(addp_supermap_runtime PRIVATE
    -Wall
    -Wextra
    -Wpedantic
    -Werror
  )
  set_property(TARGET addp_supermap_runtime PROPERTY CXX_STANDARD 17)
  target_link_directories(addp_supermap_runtime PUBLIC "${SUPERMAP_SDK_ROOT}/bin/bin")
  target_link_libraries(addp_supermap_runtime
    PUBLIC addp_workflow_core
    PRIVATE
      SuEngine
      SuCacheBuilder
      SuElement
      SuGeneralization
      SuGeometry
      SuGeometryConverter
      SuGraphicsQT
      SuGraphics
      SuDrawing
      SuMap
      SuOverlay
      SuProximity
      SuProjection
      SuStream
      SuToolkit
      SuTopoBase
      SuTileStorage
      SuWorkspace
      SuBase
      SuBase3D
      SuOGDC
      "${SUPERMAP_SDK_ROOT}/bin/bin/libsqlite328.so.0"
      "${SUPERMAP_SDK_ROOT}/bin/bin/libcurl.so.4"
  )

  add_library(addp_supermap_runtime_bridge src/main/cpp/runtime_bridge.cpp)
  target_include_directories(addp_supermap_runtime_bridge PUBLIC src/main/cpp)
  target_compile_definitions(
    addp_supermap_runtime_bridge PRIVATE _GLIBCXX_USE_CXX11_ABI=0 _UGUNICODE
  )
  target_compile_options(addp_supermap_runtime_bridge PRIVATE
    -fshort-wchar
    -Wall
    -Wextra
    -Wpedantic
    -Werror
  )
  target_link_libraries(addp_supermap_runtime_bridge PRIVATE addp_supermap_runtime)
  set_property(TARGET addp_supermap_runtime_bridge PROPERTY CXX_STANDARD 17)

  add_executable(supermap-workflow-server
    src/main/cpp/http_server.cpp
    src/main/cpp/main.cpp
  )
  target_link_libraries(
    supermap-workflow-server
    PRIVATE addp_supermap_runtime_bridge "${CPP_HTTPLIB_LIBRARY}" nlohmann_json::nlohmann_json
  )
  target_include_directories(
    supermap-workflow-server SYSTEM PRIVATE "${CPP_HTTPLIB_INCLUDE_DIR}"
  )
  target_compile_options(
    supermap-workflow-server PRIVATE -Wall -Wextra -Wpedantic -Werror
  )
  target_compile_definitions(supermap-workflow-server PRIVATE _GLIBCXX_USE_CXX11_ABI=1)
  set_property(TARGET supermap-workflow-server PROPERTY CXX_STANDARD 20)
endif()

include(CTest)
if(BUILD_TESTING)
  add_executable(workflow-core-test src/test/cpp/workflow_test.cpp)
  target_link_libraries(workflow-core-test PRIVATE addp_workflow_core)
  target_compile_options(workflow-core-test PRIVATE -Wall -Wextra -Wpedantic -Werror)
  add_test(NAME workflow-core-test COMMAND workflow-core-test)

  add_executable(operator-catalog-test src/test/cpp/operator_catalog_test.cpp)
  target_link_libraries(operator-catalog-test PRIVATE addp_workflow_core)
  target_compile_options(operator-catalog-test PRIVATE -Wall -Wextra -Wpedantic -Werror)
  add_test(
    NAME operator-catalog-test
    COMMAND operator-catalog-test "${CMAKE_CURRENT_SOURCE_DIR}/config/operators.json"
  )

  if(SUPERMAP_SDK_ROOT)
    add_executable(supermap-runtime-test-driver src/test/cpp/runtime_test_driver.cpp)
    target_link_libraries(supermap-runtime-test-driver PRIVATE addp_supermap_runtime)
    target_compile_options(
      supermap-runtime-test-driver PRIVATE -Wall -Wextra -Wpedantic -Werror
    )
    set_property(TARGET supermap-runtime-test-driver PROPERTY CXX_STANDARD 17)
  endif()
endif()
