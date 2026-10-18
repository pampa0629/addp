ARG GAUSSDB_BASE_IMAGE
FROM ${GAUSSDB_BASE_IMAGE}
ARG GAUSSDB_VERSION
ENV GAUSSDB_VERSION=${GAUSSDB_VERSION}
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends libaio1 libnuma1 libreadline8 libncurses6 libncurses5 libtinfo5 zlib1g libatomic1 libltdl7 libtirpc3 locales && rm -rf /var/lib/apt/lists/*
COPY kernel.bin /tmp/kernel.bin
RUN chmod 700 /tmp/kernel.bin && /tmp/kernel.bin -y -o/opt/gauss && rm /tmp/kernel.bin && useradd -m -u 10001 omm && mkdir /data && chown -R omm:omm /opt/gauss /data
ENV GAUSSHOME=/opt/gauss GAUSSLOG=/data/log LD_LIBRARY_PATH=/opt/gauss/lib PATH=/opt/gauss/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
COPY entrypoint.sh init.sql /addp/gaussdb/
RUN chmod 755 /addp/gaussdb/entrypoint.sh
USER omm
RUN gsql --version && gaussdb --version
ENTRYPOINT ["bash", "/addp/gaussdb/entrypoint.sh"]
