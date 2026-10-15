add_executable(addp-assimp-io-smoke "${CMAKE_CURRENT_LIST_DIR}/assimp_io_smoke.cpp")
target_link_libraries(addp-assimp-io-smoke PRIVATE assimp)
target_link_options(addp-assimp-io-smoke PRIVATE "-Wl,--wrap=stat")
