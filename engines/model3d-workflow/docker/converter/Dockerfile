ARG RUST_IMAGE=rust:1.90.0-bookworm
FROM ${RUST_IMAGE} AS builder

ARG THREE_DTILES_REPO=https://github.com/fanvanzh/3dtiles.git
ARG THREE_DTILES_REF=acbcf603f33fdfe3c34b704a8b019c4fd32a8376
ARG VCPKG_COMMIT=84bab45d415d22042bd0b9081aea57f362da3f35

ENV DEBIAN_FRONTEND=noninteractive
ENV VCPKG_ROOT=/opt/vcpkg
ENV PATH=/opt/vcpkg:${PATH}

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git patch \
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

COPY patches/linux-arm64-build-rs.patch /tmp/linux-arm64-build-rs.patch
RUN cd /src/3dtiles && patch -p1 < /tmp/linux-arm64-build-rs.patch

RUN git clone https://github.com/microsoft/vcpkg.git /opt/vcpkg \
  && cd /opt/vcpkg \
  && git checkout "${VCPKG_COMMIT}" \
  && ./bootstrap-vcpkg.sh

WORKDIR /src/3dtiles
ENV CARGO_TERM_COLOR=always
ENV VCPKG_HAS_BEEN_INSTALLED=1
RUN --mount=type=cache,target=/root/.cache/vcpkg \
    --mount=type=cache,target=/usr/local/cargo/registry \
    cargo build --release -vv

RUN mkdir -p /out \
  && cp target/release/_3dtile /out/_3dtile \
  && cp -r target/release/gdal /out/gdal \
  && cp -r target/release/proj /out/proj \
  && cp -r target/release/osgPlugins-3.6.5 /out/osgPlugins-3.6.5

FROM debian:bookworm-slim AS runtime

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libgl1 libglu1-mesa libx11-6 libxrandr2 libxi6 libxxf86vm1 \
  && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /opt/addp/model3d-workflow/bin
COPY --from=builder /out/ /opt/addp/model3d-workflow/bin/

ENV MODEL3D_CONVERTER_BIN=/opt/addp/model3d-workflow/bin/_3dtile
ENV GDAL_DATA=/opt/addp/model3d-workflow/bin/gdal
ENV PROJ_DATA=/opt/addp/model3d-workflow/bin/proj
ENV OSG_LIBRARY_PATH=/opt/addp/model3d-workflow/bin/osgPlugins-3.6.5

WORKDIR /data
ENTRYPOINT ["/opt/addp/model3d-workflow/bin/_3dtile"]
