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
RUN go build -trimpath -ldflags "-s -w" -o /out/wardline ./cmd/wardline

# Just the program, for installing without Docker: docker build --target binary --output dist .
FROM scratch AS binary
COPY --from=build /out/wardline /wardline

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates iproute2 \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/wardline /usr/local/bin/wardline
VOLUME /var/lib/wardline
EXPOSE 8080/tcp 51820/udp
ENTRYPOINT ["wardline"]
