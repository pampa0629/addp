#include <assimp/DefaultIOSystem.h>
#include <cerrno>
#include <cstring>
#include <fstream>
#include <iostream>
#include <sys/stat.h>

extern "C" int __real_stat(const char *, struct stat *);
static bool failed_stat_called = false;

extern "C" int __wrap_stat(const char *path, struct stat *result) {
    if (std::strcmp(path, "addp-missing-texture.png") == 0) {
        failed_stat_called = true;
        // stat() leaves the output unspecified on failure. Make the old bug
        // deterministic rather than relying on whatever was left on the stack.
        std::memset(result, 0, sizeof(*result));
        result->st_mode = S_IFREG | 0644;
        errno = ENOENT;
        return -1;
    }
    return __real_stat(path, result);
}

int main() {
    Assimp::DefaultIOSystem io;
    const char *existing = "addp-existing-texture.png";
    std::ofstream(existing) << "fixture";
    const bool valid = io.Exists(existing) && !io.Exists(".")
        && !io.Exists("addp-missing-texture.png")
        && !io.Exists("addp-no-such-directory/texture.png")
        && !io.Exists(nullptr) && failed_stat_called;
    std::remove(existing);
    if (!valid) {
        std::cerr << "Assimp must reject failed stat without reading its output\n";
        return 1;
    }
    std::cout << "Assimp file existence regression passed\n";
}
