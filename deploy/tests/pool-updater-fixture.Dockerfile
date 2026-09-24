FROM busybox:1.37.0
ARG VERSION=0.0.0-pool.1
ARG REVISION=1111111111111111111111111111111111111111
LABEL org.opencontainers.image.source="https://github.com/dongyaoa/sub2api-pool"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.revision="${REVISION}"
RUN mkdir -p /app/data && printf '#!/bin/sh\nprintf "Sub2API %s (commit: %s, built at: fixture)\\n"\n' "$VERSION" "$REVISION" > /app/sub2api && chmod 755 /app/sub2api
ENTRYPOINT ["/bin/sh", "-c", "exec sleep 3600"]
