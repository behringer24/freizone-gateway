# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/freizone-gateway ./cmd/gateway

# distroless/static provides CA certificates (needed for outbound calls to
# FCM/APNs and for autocert's ACME requests) and nothing else -- the binary
# is fully static (CGO_ENABLED=0), so no libc is required.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/freizone-gateway /freizone-gateway

ENV GATEWAY_DATA_DIR=/data
VOLUME ["/data"]
EXPOSE 8080 8443

ENTRYPOINT ["/freizone-gateway"]
