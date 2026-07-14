# Multi-stage; identical hardening posture to the API image.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/accounts-consumer .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/accounts-consumer /accounts-consumer
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/accounts-consumer"]
