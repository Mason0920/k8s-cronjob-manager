# syntax=docker/dockerfile:1

# 1. Build the web UI.
FROM node:24-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2. Build the Go binary with the UI embedded.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist/ui ./web/dist/ui
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/cronjob-manager ./cmd/cronjob-manager

# 3. Minimal runtime: no shell, runs as uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cronjob-manager /cronjob-manager
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/cronjob-manager"]
