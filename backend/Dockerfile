# Requires the Vue SPA to be pre-built before this runs: `cd frontend && npm
# run build`, which writes straight into internal/api/web/dist (see
# frontend/vite.config.ts) so go:embed picks it up here. Not built as a Docker
# stage — the deploy dir on the VPS ships only backend/, matching how the
# vendored Swagger UI assets already work (pre-built, not generated in-image).

# --- build stage ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/quickroom ./cmd/quickroom
# Writable token dir, owned by the non-root runtime user (distroless can't mkdir).
RUN mkdir -p /data && chown 65532:65532 /data

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/quickroom /quickroom
COPY --from=build --chown=nonroot:nonroot /data /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/quickroom"]
