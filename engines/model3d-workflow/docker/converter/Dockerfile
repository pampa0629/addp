ARG RUST_IMAGE=rust:1.92.0-bookworm
FROM ${RUST_IMAGE} AS builder

ARG THREE_DTILES_REPO=https://github.com/fanvanzh/3dtiles.git
ARG THREE_DTILES_REF=acbcf603f33fdfe3c34b704a8b019c4fd32a8376
ARG VCPKG_COMMIT=84bab45d415d22042bd0b9081aea57f362da3f35
ARG TARGETARCH

ENV DEBIAN_FRONTEND=noninteractive
ENV VCPKG_ROOT=/opt/vcpkg
ENV PATH=/opt/vcpkg:${PATH}

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git patch python3 \
    build-essential cmake make zip unzip tar pkg-config \
    autoconf autoconf-archive automake libtool linux-libc-dev \
    libgl1-mesa-dev libglu1-mesa-dev \
    libx11-dev libxrandr-dev libxi-dev libxxf86vm-dev \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /src
RUN git clone --depth 1 "${THREE_DTILES_REPO}" 3dtiles \
  && cd 3dtiles \
  && git fetch --depth 1 origin "${THREE_DTILES_REF}" \
  && git checkout FETCH_HEAD
RUN cd /src/3dtiles \
  && git submodule sync --recursive \
  && git submodule update --init --recursive --depth 1

COPY engines/model3d-workflow/docker/converter/patches/linux-build-rs.patch /tmp/linux-build-rs.patch
RUN cd /src/3dtiles && patch -p1 < /tmp/linux-build-rs.patch

RUN git clone https://github.com/microsoft/vcpkg.git /opt/vcpkg \
  && cd /opt/vcpkg \
  && git checkout "${VCPKG_COMMIT}" \
  && ./bootstrap-vcpkg.sh

COPY engines/model3d-workflow/docker/converter/vcpkg-overlays/ /opt/addp/vcpkg-overlays/
ENV VCPKG_OVERLAY_PORTS=/opt/addp/vcpkg-overlays

COPY engines/model3d-workflow/docker/converter/Cargo.lock /src/3dtiles/Cargo.lock
WORKDIR /src/3dtiles
ENV CARGO_TERM_COLOR=always
ENV VCPKG_HAS_BEEN_INSTALLED=1
RUN --mount=type=cache,target=/root/.cache/vcpkg \
    --mount=type=cache,target=/usr/local/cargo/registry \
    cargo build --locked --release -vv

COPY engines/model3d-workflow/docker/converter/tests/osgb_compression_smoke.cpp /tmp/osgb_compression_smoke.cpp
RUN set -eu; \
    case "$TARGETARCH" in amd64) triplet=x64-linux ;; arm64) triplet=arm64-linux ;; *) exit 1 ;; esac; \
    library_root=$(find -L target/release/build -type d -path "*/vcpkg_installed/${triplet}" -print -quit); \
    test -n "$library_root"; \
    c++ -std=c++17 /tmp/osgb_compression_smoke.cpp -o /tmp/osgb-compression-smoke \
      -I"$library_root/include" -L"$library_root/lib" -L"$library_root/lib/osgPlugins-3.6.5" \
      -Wl,--start-group -losgdb_osg -losgdb_serializers_osg -losgUtil -losgDB -losg -lOpenThreads \
      -Wl,--end-group -lz -lGL -lpthread -ldl; \
    /tmp/osgb-compression-smoke generate /tmp/compressed-triangle.osgb; \
    target/release/_3dtile -f gltf -i /tmp/compressed-triangle.osgb -o /tmp/compressed-triangle.glb; \
    /tmp/osgb-compression-smoke verify /tmp/compressed-triangle.glb; \
    for format in dxt1 dxt1a dxt3 dxt5 dxt1a-alpha dxt3-alpha dxt5-alpha rgb-padded rgba-padded bgra-padded; do \
      /tmp/osgb-compression-smoke generate "/tmp/texture-${format}.osgb" "$format"; \
      target/release/_3dtile -f gltf -i "/tmp/texture-${format}.osgb" -o "/tmp/texture-${format}.glb"; \
      /tmp/osgb-compression-smoke verify "/tmp/texture-${format}.glb" "$format"; \
      rm "/tmp/texture-${format}.osgb" "/tmp/texture-${format}.glb"; \
    done; \
    rm /tmp/osgb-compression-smoke /tmp/compressed-triangle.osgb /tmp/compressed-triangle.glb

RUN mkdir -p /out \
  && cp target/release/_3dtile /out/_3dtile \
  && cp -r target/release/gdal /out/gdal \
  && cp -r target/release/proj /out/proj \
  && cp -r target/release/osgPlugins-3.6.5 /out/osgPlugins-3.6.5
COPY engines/model3d-workflow/native-assets.json engines/model3d-workflow/native_setup.py /tmp/addp-model3d/
RUN case "$TARGETARCH" in \
      amd64) ifc_platform=linux64 ;; \
      arm64) ifc_platform=linuxarm64 ;; \
      *) echo "unsupported IfcConvert target: $TARGETARCH" >&2; exit 1 ;; \
    esac \
  && python3 /tmp/addp-model3d/native_setup.py install-ifc /out --platform "$ifc_platform" \
  && rm -rf /out/downloads

COPY engines/model3d-workflow/docker/converter/patches/assimp-stat-result.patch /tmp/assimp-stat-result.patch
COPY engines/model3d-workflow/docker/converter/tests/assimp_io_smoke.* /tmp/assimp-io/
RUN curl --fail --location --retry 3 https://github.com/assimp/assimp/archive/refs/tags/v5.4.3.tar.gz -o /tmp/assimp.tar.gz \
  && echo '66dfbaee288f2bc43172440a55d0235dfc7bf885dda6435c038e8000e79582cb  /tmp/assimp.tar.gz' | sha256sum --check \
  && tar -xzf /tmp/assimp.tar.gz -C /tmp \
  && cd /tmp/assimp-5.4.3 && patch -p1 < /tmp/assimp-stat-result.patch \
  && cmake -S . -B build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF \
     -DASSIMP_BUILD_TESTS=OFF -DASSIMP_BUILD_ASSIMP_TOOLS=ON -DASSIMP_BUILD_ZLIB=ON \
     -DASSIMP_WARNINGS_AS_ERRORS=OFF -DCMAKE_PROJECT_Assimp_INCLUDE=/tmp/assimp-io/assimp_io_smoke.cmake \
  && cmake --build build --target assimp_cmd addp-assimp-io-smoke --parallel 2 \
  && cd build && ./addp-assimp-io-smoke \
  && cp bin/assimp /out/assimp

FROM debian:bookworm-slim AS runtime

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libgl1 libglu1-mesa libx11-6 libxrandr2 libxi6 libxxf86vm1 \
  && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /opt/addp/model3d-workflow/bin
COPY --from=builder /out/ /opt/addp/model3d-workflow/bin/

ENV MODEL3D_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/_3dtile
ENV MODEL3D_MESH_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/assimp
ENV MODEL3D_IFC_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/IfcConvert
ENV GDAL_DATA=/opt/addp/model3d-workflow/bin/gdal
ENV PROJ_DATA=/opt/addp/model3d-workflow/bin/proj
ENV OSG_LIBRARY_PATH=/opt/addp/model3d-workflow/bin/osgPlugins-3.6.5

WORKDIR /data
ENTRYPOINT ["/opt/addp/model3d-workflow/bin/_3dtile"]
