# syntax=docker/dockerfile:1.7
FROM golang:1.26.0-bookworm AS build

WORKDIR /src
RUN mkdir -p /out
COPY go.mod go.sum* ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -mod=readonly \
      -trimpath -ldflags="-s -w" \
      -o /out/factcheck ./cmd/factcheck

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl tzdata && \
    rm -rf /var/lib/apt/lists/* && \
    useradd --system --uid 10001 --create-home factcheck && \
    install -d -m 0700 /var/lib/factcheck/telegram && \
    chown factcheck: /var/lib/factcheck/telegram
WORKDIR /opt/factcheck
COPY --from=build /out/* /usr/local/bin/
USER factcheck
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/factcheck"]
CMD ["serve"]
