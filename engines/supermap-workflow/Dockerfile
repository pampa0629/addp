ARG BASE_IMAGE=addp-supermap-workflow-base:local
FROM ${BASE_IMAGE}

LABEL addp.service="supermap-workflow-engine"
LABEL addp.supermap.code-image="true"

ENV PORT=8103
ENV APP_ROOT=/app
ENV SUPERMAP_OBJECTSJAVA_BIN=/opt/supermap/objectsjava/bin_linux_arm64
ENV SUPERMAP_GPA_LIB_DIR=/opt/supermap/gpa/libs

WORKDIR /app

COPY src /app/src
COPY run.sh /app/run.sh
RUN chmod +x /app/run.sh \
  && mkdir -p /app/target/classes \
  && javac \
    -encoding UTF-8 \
    -cp "/opt/supermap/gpa/libs/*:/opt/supermap/objectsjava/bin_linux_arm64/*" \
    -d /app/target/classes \
    /app/src/main/java/com/addp/supermap/workflow/SuperMapWorkflowRuntime.java

EXPOSE 8103
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=6 CMD curl -fsS "http://localhost:${PORT:-8103}/health" | grep -q '"status":"healthy"' || exit 1
CMD ["/app/run.sh"]
