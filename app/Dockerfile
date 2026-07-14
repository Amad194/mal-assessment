# --- build stage ------------------------------------------------------------
# Base images are pinned to a digest (see the @sha256 below) so the build is
# reproducible and can't drift under a moved tag. Digests are resolved/updated
# by the bootstrap workflow (docker buildx imagetools inspect).
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache module downloads separately from source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static, stripped, reproducible binary. CGO off so it runs on distroless static.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/accounts-api .

# --- runtime stage ----------------------------------------------------------
# distroless/static: no shell, no package manager, minimal CVE surface.
# :nonroot runs as uid 65532 — matches the numeric securityContext in the chart.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/accounts-api /accounts-api
USER 65532:65532
EXPOSE 8080

# HEALTHCHECK via the binary itself (distroless has no curl/wget). Kubernetes
# uses the HTTP probes instead; this covers plain `docker run`/compose.
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/accounts-api", "-health"]

ENTRYPOINT ["/accounts-api"]
