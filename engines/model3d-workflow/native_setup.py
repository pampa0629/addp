"""Build and verify the engine-owned, immutable macOS conversion tools."""
from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import platform
import shutil
import subprocess
import sys
import tempfile
import urllib.request
import zipfile
from pathlib import Path

ENGINE = Path(__file__).resolve().parent
CONVERTER = ENGINE / "docker/converter"
MANIFEST = ENGINE / "native-assets.json"


def digest(path, algorithm="sha256"):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, algorithm).hexdigest()


def native_paths(root):
    manifest = json.loads(MANIFEST.read_text())
    key = platform.system() + "-" + platform.machine()
    if key != manifest["platform"]:
        raise RuntimeError("Model3D native installation is not verified for " + key)
    files = [MANIFEST, Path(__file__), CONVERTER / "Cargo.lock",
             CONVERTER / "patches/linux-build-rs.patch",
             CONVERTER / "tests/osgb_compression_smoke.cpp"]
    files += sorted((CONVERTER / "vcpkg-overlays").rglob("*"))
    identity = hashlib.sha256(key.encode())
    for path in files:
        if path.is_file():
            identity.update(str(path.relative_to(ENGINE)).encode())
            identity.update(path.read_bytes())
    identity = identity.hexdigest()
    return manifest, identity, Path(root).resolve() / identity[:16]


def tool_environment(prefix):
    env = dict(os.environ)
    for key in ("PYTHONHOME", "PYTHONPATH", "CONDA_PREFIX", "CONDA_DEFAULT_ENV", "CONDA_EXE",
                "GDAL_DRIVER_PATH", "GDAL_DATA", "PROJ_DATA", "PROJ_LIB", "OSG_LIBRARY_PATH",
                "LD_LIBRARY_PATH", "DYLD_LIBRARY_PATH", "DYLD_FALLBACK_LIBRARY_PATH",
                "CMAKE_PREFIX_PATH", "PKG_CONFIG_PATH", "CC", "CXX", "CFLAGS", "CXXFLAGS",
                "CPPFLAGS", "LDFLAGS", "CPATH", "LIBRARY_PATH"):
        env.pop(key, None)
    env.update(GDAL_DATA=str(prefix / "bin/gdal"), PROJ_DATA=str(prefix / "bin/proj"),
               OSG_LIBRARY_PATH=str(prefix / "bin/osgPlugins-3.6.5"), PYTHONNOUSERSITE="1")
    return env


def bundle_files(prefix):
    return sorted(path for folder in ("bin", "deps/lib") for path in (prefix / folder).rglob("*")
                  if path.is_file())


def current(root):
    try:
        _, identity, prefix = native_paths(root)
        state = json.loads((prefix / "ready.json").read_text())
        files = {str(p.relative_to(prefix)): digest(p) for p in bundle_files(prefix)}
        required = {"bin/_3dtile", "bin/assimp", "bin/IfcConvert", "bin/osgb-smoke", "bin/proj/proj.db"}
        return (state["identity"] == identity and required <= files.keys()
                and state["files"] == files
                and all(os.access(prefix / p, os.X_OK) for p in required if p != "bin/proj/proj.db"))
    except (OSError, ValueError, KeyError):
        return False


def download(asset, cache):
    cache.mkdir(parents=True, exist_ok=True)
    algorithm = "sha512" if "sha512" in asset else "sha256"
    expected = asset[algorithm]
    target = cache / (expected[:16] + "-" + asset["url"].rsplit("/", 1)[1])
    if target.is_file() and digest(target, algorithm) == expected:
        return target
    handle, filename = tempfile.mkstemp(dir=cache, prefix="download-")
    os.close(handle)
    pending = Path(filename)
    try:
        with urllib.request.urlopen(asset["url"], timeout=120) as response, pending.open("wb") as stream:
            shutil.copyfileobj(response, stream)
        if digest(pending, algorithm) != expected:
            raise RuntimeError("download checksum mismatch: " + asset["url"])
        pending.replace(target)
        return target
    finally:
        pending.unlink(missing_ok=True)


def install_ifc(platform_key, destination, cache):
    manifest = json.loads(MANIFEST.read_text())
    archive = download(manifest["ifc_packages"][platform_key], cache)
    destination.mkdir(parents=True, exist_ok=True)
    with zipfile.ZipFile(archive) as source:
        # Extract only the declared executable, never archive-controlled paths.
        (destination / "IfcConvert").write_bytes(source.read("IfcConvert"))
    (destination / "IfcConvert").chmod(0o755)


def git_cache(specification, directory, run):
    cache = directory / (specification["commit"] + ".git")
    if not cache.exists():
        run(["git", "init", "--bare", cache])
    present = subprocess.run(["git", "--git-dir", str(cache), "cat-file", "-e", specification["commit"] + "^{commit}"],
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
    if not present:
        run(["git", "--git-dir", cache, "fetch", "--depth", "1", specification["url"], specification["commit"]])
    return cache


def clone(specification, destination, run):
    if not destination.exists():
        run(["git", "init", str(destination)])
        run(["git", "-C", str(destination), "remote", "add", "origin", specification["url"]])
    commit = subprocess.check_output(["git", "-C", str(destination), "rev-parse", "HEAD"],
                                     stderr=subprocess.DEVNULL, text=True).strip() if (destination / ".addp-source").exists() else ""
    if commit != specification["commit"]:
        # Source objects survive tool-bundle fingerprint changes; Git verifies
        # the same pinned commit whether it was fetched now or already cached.
        cache = git_cache(specification, destination.parents[2] / "cache/git", run)
        run(["git", "-C", str(destination), "fetch", "--depth", "1", cache, specification["commit"]])
        run(["git", "-C", str(destination), "checkout", "--detach", "FETCH_HEAD"])
        (destination / ".addp-source").write_text(specification["commit"])
    actual = subprocess.check_output(["git", "-C", str(destination), "rev-parse", "HEAD"], text=True).strip()
    if actual != specification["commit"]:
        raise RuntimeError("source identity mismatch: " + str(destination))


def build(root, manifest, prefix, log):
    cache = root / "cache"
    build_dir = root / "build" / prefix.name
    build_dir.mkdir(parents=True, exist_ok=True)
    env = tool_environment(prefix)
    commands = {name: shutil.which(name) for name in ("cargo", "rustc", "cmake", "conda")}
    for name, path in commands.items():
        if not path:
            raise RuntimeError("Model3D native build requires " + name)
    for name, expected in (("rustc", manifest["rust"]), ("cmake", manifest["cmake"])):
        actual = subprocess.check_output([commands[name], "--version"], text=True).splitlines()[0].split()[1 if name == "rustc" else 2]
        if actual != expected:
            raise RuntimeError(f"Model3D native build requires {name} {expected}, found {actual}")
    # A pip-installed launcher needs user site-packages; bind its native program
    # before entering the isolated build environment instead of enabling them.
    cmake_probe = build_dir / "cmake-program.cmake"
    cmake_probe.write_text('message("${CMAKE_COMMAND}")\n')
    cmake_program = subprocess.run([commands["cmake"], "-P", str(cmake_probe)], check=True,
                                   capture_output=True, text=True).stderr.strip()
    if not Path(cmake_program).is_absolute() or not os.access(cmake_program, os.X_OK):
        raise RuntimeError("CMake did not report an executable native program")
    commands["cmake"] = cmake_program
    env.update(CMAKE=cmake_program,
               PATH=str(Path(cmake_program).parent) + os.pathsep + env["PATH"])
    subprocess.run(["xcode-select", "-p"], check=True, stdout=log, stderr=log)

    def run(command, cwd=None):
        log.write(("\n" + " ".join(map(str, command)) + "\n").encode()); log.flush()
        subprocess.run(list(map(str, command)), cwd=cwd, env=env, check=True, stdout=log, stderr=log)

    deps = prefix / "deps"
    if not (deps / ".addp-packages").exists():
        if deps.exists():
            shutil.rmtree(deps)
        packages = [download(asset, cache / "downloads") for asset in manifest["conda_packages"]]
        # Conda requires the original archive filename to determine package identity.
        package_dir = cache / "conda-packages"
        package_dir.mkdir(parents=True, exist_ok=True)
        archives = []
        for asset, package in zip(manifest["conda_packages"], packages):
            path = package_dir / asset["url"].rsplit("/", 1)[1]
            shutil.copyfile(package, path)
            archives.append(str(path))
        specification = build_dir / "conda-explicit.txt"
        specification.write_text("@EXPLICIT\n" + "\n".join(archives) + "\n")
        run([commands["conda"], "create", "--yes", "--no-default-packages", "--prefix", deps, "--file", specification])
        (deps / ".addp-packages").write_text(json.dumps(manifest["conda_packages"]))
    env.update(PATH=str(deps / "bin") + os.pathsep + env["PATH"],
               ACLOCAL_PATH=str(deps / "share/aclocal"))
    source = build_dir / "3dtiles"
    clone(manifest["3dtiles"], source, run)
    submodules = subprocess.check_output(["git", "config", "-f", str(source / ".gitmodules"),
                                         "--get-regexp", r"submodule\..*\.path"], text=True)
    for entry in submodules.splitlines():
        key, path = entry.split(" ", 1)
        url_key = key.removesuffix(".path") + ".url"
        url = subprocess.check_output(["git", "config", "-f", str(source / ".gitmodules"), "--get", url_key], text=True).strip()
        commit = subprocess.check_output(["git", "-C", str(source), "rev-parse", "HEAD:" + path], text=True).strip()
        submodule_cache = git_cache({"url": url, "commit": commit}, cache / "git", run)
        run(["git", "-C", source, "config", url_key, submodule_cache])
    run(["git", "-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive", "--depth", "1"], source)
    if not (source / ".addp-patch").exists():
        run(["patch", "-p1", "-i", CONVERTER / "patches/linux-build-rs.patch"], source)
        (source / ".addp-patch").write_text(digest(CONVERTER / "patches/linux-build-rs.patch"))
    shutil.copyfile(CONVERTER / "Cargo.lock", source / "Cargo.lock")
    vcpkg = build_dir / "vcpkg"
    clone(manifest["vcpkg"], vcpkg, run)
    shutil.copyfile(download(manifest["vcpkg_tool"], cache / "downloads"), vcpkg / "vcpkg")
    (vcpkg / "vcpkg").chmod(0o755)
    env.update(VCPKG_ROOT=str(vcpkg), VCPKG_OVERLAY_PORTS=str(CONVERTER / "vcpkg-overlays"),
               VCPKG_DOWNLOADS=str(cache / "vcpkg-downloads"),
               VCPKG_BINARY_SOURCES="clear;files," + str(cache / "vcpkg-binary") + ",readwrite",
               X_VCPKG_ASSET_SOURCES='clear;x-script,"' + sys.executable + '" "' + str(Path(__file__)) + '" fetch-asset "{url}" {sha512} "{dst}";x-block-origin',
               VCPKG_MAX_CONCURRENCY="4", CARGO_BUILD_JOBS="4", CARGO_HOME=str(cache / "cargo"),
               CARGO_TARGET_DIR=str(build_dir / "target"), VCPKG_HAS_BEEN_INSTALLED="1", CMAKE_GENERATOR="Unix Makefiles")
    for name in ("vcpkg-downloads", "vcpkg-binary", "cargo"):
        (cache / name).mkdir(parents=True, exist_ok=True)
    run([commands["cargo"], "build", "--locked", "--release", "-vv"], source)
    binary_dir = prefix / "bin"
    binary_dir.mkdir(parents=True, exist_ok=True)
    output = build_dir / "target/release"
    shutil.copyfile(output / "_3dtile", binary_dir / "_3dtile")
    (binary_dir / "_3dtile").chmod(0o755)
    for name in ("gdal", "proj", "osgPlugins-3.6.5"):
        shutil.copytree(output / name, binary_dir / name, dirs_exist_ok=True)
    library = next((output / "build").glob("_3dtile-*/out/build/vcpkg_installed/arm64-osx"))
    run(["/usr/bin/clang++", "-std=c++17", CONVERTER / "tests/osgb_compression_smoke.cpp", "-o", binary_dir / "osgb-smoke",
         "-I" + str(library / "include"), "-I" + str(library / "include/stb"),
         "-L" + str(library / "lib"), "-L" + str(library / "lib/osgPlugins-3.6.5"), "-Wl,-all_load",
         "-losgdb_osg", "-losgdb_serializers_osg", "-losgUtil", "-losgDB", "-losg", "-lOpenThreads", "-lz",
         "-framework", "OpenGL", "-framework", "CoreFoundation", "-framework", "Foundation"])
    assimp = build_dir / "assimp"
    clone(manifest["assimp"], assimp, run)
    cli_sources = list((assimp / "tools/assimp_cmd").glob("*.cpp"))
    run(["/usr/bin/clang++", "-std=c++17", "-I" + str(deps / "include"), "-I" + str(assimp / "code"),
         *cli_sources, "-L" + str(deps / "lib"), "-lassimp", "-lz", "-Wl,-rpath," + str(deps / "lib"),
         "-o", binary_dir / "assimp"])
    install_ifc("macosm164", binary_dir, cache / "downloads")


def verify(prefix):
    from glb_validation import validate_glb
    from operators import invoke_operator

    env = tool_environment(prefix)
    os.environ.clear(); os.environ.update(env)
    bound = {"MODEL3D_CONVERTER_BIN": str(prefix / "bin/_3dtile"),
             "MODEL3D_MESH_CONVERTER_BIN": str(prefix / "bin/assimp"),
             "MODEL3D_IFC_CONVERTER_BIN": str(prefix / "bin/IfcConvert")}
    with tempfile.TemporaryDirectory(prefix="addp-model3d-native-check-") as temporary:
        work = Path(temporary)
        def convert(operator, source, format_name):
            target = work / (source.stem + ".glb")
            plan = {"schema_version": "addp.workflow.access-plan/v1",
                    "source": {"kind": "file", "format": format_name, "access": {"method": "mounted_path", "path": str(source)}},
                    "target": {"kind": "file", "format": "glb", "name": target.name, "write_mode": "create",
                               "access": {"method": "mounted_path", "path": str(target)}}}
            invoke_operator(operator, {"access_plan": plan}, env=bound, timeout_seconds=60)
            validate_glb(target)
            return target
        for texture in ("dxt1", "dxt1a", "dxt3", "dxt5"):
            source = work / (texture + ".osgb")
            subprocess.run([str(prefix / "bin/osgb-smoke"), "generate", str(source), texture], check=True, capture_output=True)
            target = convert("osgb_to_glb", source, "osgb")
            subprocess.run([str(prefix / "bin/osgb-smoke"), "verify", str(target), texture], check=True, capture_output=True)
        source = work / "triangle.obj"
        source.write_text("v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n")
        convert("obj_to_glb", source, "obj")
        source = work / "box.ifc"
        source.write_text(IFC_BOX)
        convert("ifc_to_glb", source, "ifc")
    version = subprocess.check_output([str(prefix / "bin/IfcConvert"), "--version"], text=True).strip()
    if json.loads(MANIFEST.read_text())["ifc_version"] not in version:
        raise RuntimeError("IfcConvert build identity mismatch: " + version)


def prepare(root):
    root = Path(root).resolve()
    root.mkdir(parents=True, exist_ok=True)
    with (root / "install.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        manifest, identity, prefix = native_paths(root)
        if current(root):
            verify(prefix)
            return
        if (prefix / "ready.json").exists():
            raise RuntimeError("Model3D native cache is damaged; stop the Runtime and inspect " + str(prefix))
        prefix.mkdir(parents=True, exist_ok=True)
        print("Model3D native build log: " + str(prefix / "build.log"), flush=True)
        with (prefix / "build.log").open("ab") as log:
            build(root, manifest, prefix, log)
        verify(prefix)
        if native_paths(root)[1] != identity:
            raise RuntimeError("Model3D build inputs changed during installation")
        state = {"identity": identity, "files": {str(p.relative_to(prefix)): digest(p) for p in bundle_files(prefix)}}
        (prefix / "ready.json").write_text(json.dumps(state, indent=2))
        if not current(root):
            raise RuntimeError("Model3D native installation failed integrity verification")


IFC_BOX = """ISO-10303-21;
HEADER;
FILE_DESCRIPTION(('ADDP native preflight'),'2;1');
FILE_NAME('box.ifc','2026-10-07T00:00:00',('ADDP'),('ADDP'),'ADDP','ADDP','');
FILE_SCHEMA(('IFC4'));
ENDSEC;
DATA;
#1=IFCCARTESIANPOINT((0.,0.,0.));
#2=IFCDIRECTION((0.,0.,1.));
#3=IFCDIRECTION((1.,0.,0.));
#4=IFCAXIS2PLACEMENT3D(#1,#2,#3);
#5=IFCGEOMETRICREPRESENTATIONCONTEXT($,'Model',3,0.00001,#4,$);
#6=IFCSIUNIT(*,.LENGTHUNIT.,$,.METRE.);
#7=IFCUNITASSIGNMENT((#6));
#8=IFCPROJECT('0000000000000000000001',$,'ADDP',$,$,$,$,(#5),#7);
#9=IFCCARTESIANPOINT((0.,0.));
#10=IFCAXIS2PLACEMENT2D(#9,$);
#11=IFCRECTANGLEPROFILEDEF(.AREA.,$,#10,1.,1.);
#12=IFCEXTRUDEDAREASOLID(#11,#4,#2,1.);
#13=IFCSHAPEREPRESENTATION(#5,'Body','SweptSolid',(#12));
#14=IFCPRODUCTDEFINITIONSHAPE($,$,(#13));
#15=IFCLOCALPLACEMENT($,#4);
#16=IFCBUILDINGELEMENTPROXY('0000000000000000000002',$,'ADDP box',$,$,#15,#14,$,$);
ENDSEC;
END-ISO-10303-21;
"""


def main():
    if len(sys.argv) > 1 and sys.argv[1] == "fetch-asset":
        url, checksum, target = sys.argv[2:]
        downloaded = download({"url": url, "sha512": checksum}, Path(target).parent)
        shutil.copyfile(downloaded, target)
        return
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=("prepare", "current", "environment", "verify", "install-ifc"))
    parser.add_argument("root", type=Path)
    parser.add_argument("--platform", choices=("macosm164", "linux64", "linuxarm64"))
    args = parser.parse_args()
    if args.command == "install-ifc":
        if not args.platform:
            parser.error("--platform is required")
        install_ifc(args.platform, args.root, args.root / "downloads")
    elif args.command == "prepare":
        prepare(args.root)
    elif args.command == "current":
        sys.exit(0 if current(args.root) else 1)
    elif args.command == "environment":
        print(native_paths(args.root)[2])
    else:
        if not current(args.root):
            raise RuntimeError("Model3D native installation is not current")
        verify(native_paths(args.root)[2])


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, OSError, subprocess.SubprocessError, KeyError, ValueError) as error:
        print("Model3D native setup failed: " + str(error), file=sys.stderr)
        sys.exit(1)
