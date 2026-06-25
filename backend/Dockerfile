# --- build stage ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/roompulse ./cmd/roompulse
# Writable token dir, owned by the non-root runtime user (distroless can't mkdir).
RUN mkdir -p /data && chown 65532:65532 /data

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/roompulse /roompulse
COPY --from=build --chown=nonroot:nonroot /data /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/roompulse"]
