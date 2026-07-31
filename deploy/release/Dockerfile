ARG POSTGRES_IMAGE=postgres:18-alpine3.21@sha256:44d837eb4c2ed263474a95f0cc24745413c50924df60dd73ed6c4c3e36b84259
ARG ALPINE_IMAGE=alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

FROM ${POSTGRES_IMAGE} AS pg-client
FROM ${ALPINE_IMAGE}

ARG TARGETARCH
ARG RELEASE_TAG
ARG SOURCE_COMMIT

LABEL maintainer="o87110"
LABEL description="Sub2API Custom - AI API Gateway Platform"
LABEL org.opencontainers.image.source="https://github.com/o87110/sub2api-custom-public"
LABEL org.opencontainers.image.version="${RELEASE_TAG}"
LABEL org.opencontainers.image.revision="${SOURCE_COMMIT}"

RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    curl \
    su-exec \
    libpq \
    zstd-libs \
    lz4-libs \
    krb5-libs \
    libldap \
    libedit \
    && rm -rf /var/cache/apk/*

COPY --from=pg-client /usr/local/bin/pg_dump /usr/local/bin/pg_dump
COPY --from=pg-client /usr/local/bin/psql /usr/local/bin/psql
COPY --from=pg-client /usr/local/lib/libpq.so.5* /usr/local/lib/

RUN addgroup -g 1000 sub2api && \
    adduser -u 1000 -G sub2api -s /bin/sh -D sub2api

WORKDIR /app
COPY binaries/${TARGETARCH}/sub2api /app/image/sub2api
COPY --chown=sub2api:sub2api backend/resources /app/resources
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN mkdir -p /app/data /app/runtime && \
    chmod 0755 /app/docker-entrypoint.sh /app/image/sub2api && \
    chown -R sub2api:sub2api /app

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:${SERVER_PORT:-8080}/health || exit 1

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/runtime/sub2api"]
