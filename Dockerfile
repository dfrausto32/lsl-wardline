# Production image: one static binary. Build with: docker build -t lsl-wardline:latest .
FROM node:22-bookworm-slim AS web
WORKDIR /src/web
RUN npm install -g pnpm@9
COPY web/ ./
RUN if [ -f package.json ]; then pnpm install --frozen-lockfile && pnpm run build; else mkdir -p /src/internal/webui/dist; fi

FROM golang:1.26-bookworm AS build
ENV CGO_ENABLED=0 GOFLAGS=-buildvcs=false
WORKDIR /build
COPY . .
COPY --from=web /src/internal/webui/dist/ ./internal/webui/dist/
# Cross-compiles for any system without emulation: docker build --build-arg GOOS=windows --build-arg GOARCH=amd64 ...
ARG GOOS=linux
ARG GOARCH=amd64
ARG VERSION=dev
RUN GOOS=$GOOS GOARCH=$GOARCH go build -trimpath -ldflags "-s -w -X main.version=$VERSION" -o /out/wardline ./cmd/wardline

# Just the program, for installing without Docker: docker build --target binary --output dist .
FROM scratch AS binary
COPY --from=build /out/wardline /wardline

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates iproute2 iputils-ping \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/wardline /usr/local/bin/wardline
VOLUME /var/lib/wardline
EXPOSE 8080/tcp 51820/udp
ENTRYPOINT ["wardline"]
