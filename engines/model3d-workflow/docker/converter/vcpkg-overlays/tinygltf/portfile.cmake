# Pin the 2.9.7 release commit; the baseline's tag archive hash is stale.
vcpkg_from_github(
    OUT_SOURCE_PATH SOURCE_PATH
    REPO syoyo/tinygltf
    REF 488a70a3df62a4df1a736e9e56fb8836580c4888
    SHA512 ad2f68089f9ef5610055aaabcd2c95543b125a15c6fd9c6fdec680ff1465370b1480e2427e3e4465b74a9b4ef772c2aa6ab3c294ea684aef513371d21eba7038
)

vcpkg_replace_string("${SOURCE_PATH}/tiny_gltf.h" "#include \"json.hpp\"" "#include <nlohmann/json.hpp>")
file(INSTALL "${SOURCE_PATH}/tiny_gltf.h" DESTINATION "${CURRENT_PACKAGES_DIR}/include")
vcpkg_install_copyright(FILE_LIST "${SOURCE_PATH}/LICENSE")
