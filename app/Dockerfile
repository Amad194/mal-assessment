# --- build stage ------------------------------------------------------------
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache module downloads separately from source for faster rebuilds.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
# Static, stripped, reproducible binary. CGO off so it runs on distroless static.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/accounts-api .

# --- runtime stage ----------------------------------------------------------
# distroless/static: no shell, no package manager, minimal CVE surface.
# :nonroot runs as uid 65532 — matches the securityContext in the Helm chart.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/accounts-api /accounts-api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/accounts-api"]
