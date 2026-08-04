ARG BASE_IMAGE=addp-supermap-workflow-base:local
ARG RUNTIME_IMAGE=ubuntu:24.04
FROM ${BASE_IMAGE} AS build

ENV LD_LIBRARY_PATH=
ENV LD_PRELOAD=

RUN apt-get update \
  && apt-get install -y --no-install-recommends libx11-dev \
  && rm -rf /var/lib/apt/lists/*

RUN rm -f \
  /opt/supermap/bin/bin/libfreetype.so \
  /opt/supermap/bin/bin/libfreetype.so.6 \
  /opt/supermap/bin/bin/libfreetype.so.6.17.4

# Supported operators do not load the optional IFC/BIM, weather, ocean, or JVM bundles.
RUN find /opt/supermap/bin/bin -maxdepth 1 \
      \( -type f -o -type l \) \
      \( \
        -name 'TB_*.tx' -o -name 'TG_*.tx' -o -name 'TD_*.tx' -o \
        -name 'libTB_*.so' -o -name 'libTG_*.so' -o -name 'libTD_*.so' -o \
        -name 'IFC*.txexp' -o -name 'libIFC*.so' -o \
        -name 'sdai.tx' -o -name 'libsdai.so' -o -name 'libjvm.so' \
      \) \
      -delete \
  && find \
      /opt/supermap/bin/bin/Resource/SilverLiningResource \
      /opt/supermap/bin/bin/Resource/OceanResource \
      -depth -delete

WORKDIR /build/source
COPY CMakeLists.txt ./
COPY config ./config
COPY src/main/cpp ./src/main/cpp
COPY src/test/cpp ./src/test/cpp

RUN cmake -S . -B /build/output -G Ninja \
      -DSUPERMAP_SDK_ROOT=/opt/supermap \
      -DBUILD_TESTING=ON \
  && env LD_PRELOAD=/lib/aarch64-linux-gnu/libfreetype.so.6 \
      cmake --build /build/output --parallel \
  && ctest --test-dir /build/output --output-on-failure

FROM ${RUNTIME_IMAGE} AS runtime

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
  && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    libcpp-httplib0.14t64 \
    libfreetype6 \
    libgomp1 \
    libqt5core5t64 \
    libqt5gui5t64 \
    libqt5widgets5t64 \
    libxinerama1 \
    libxrandr2 \
    netbase \
    nfs-common \
  && rm -rf /var/lib/apt/lists/*

LABEL addp.service="supermap-workflow-engine"
LABEL addp.supermap.code-image="true"
LABEL addp.supermap.runtime="iobjects-cpp"

ENV PORT=8103
ENV APP_ROOT=/app
ENV SUPERMAP_CPP_SDK_ROOT=/opt/supermap
ENV SUPERMAP_OPERATORS_CONFIG=/app/config/operators.json
ENV SUPERMAP_HTTP_THREADS=4

WORKDIR /app

COPY --from=build /opt/supermap/bin/bin /opt/supermap/bin/bin
COPY --from=build /build/output/supermap-workflow-server /app/bin/supermap-workflow-server
COPY config /app/config
COPY run.sh /app/run.sh

RUN chmod +x /app/run.sh /app/bin/supermap-workflow-server

ENV QT_QPA_PLATFORM=offscreen
ENV LD_LIBRARY_PATH=/opt/supermap/bin/bin
ENV LD_PRELOAD=/lib/aarch64-linux-gnu/libfreetype.so.6

EXPOSE 8103
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=6 CMD curl --max-time 2 -fsS "http://localhost:${PORT:-8103}/health" | grep -q '"status":"healthy"' || exit 1
CMD ["/app/run.sh"]
