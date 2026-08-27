# syntax=docker/dockerfile:1

# Pinned to an exact patch, not the floating 1.26-alpine tag -- the same pin
# freizone-server carries, for the same reasons. A floating tag makes the build
# non-reproducible while *also* not keeping itself current: Docker reuses
# whatever base image sits in the local cache unless the caller passes --pull,
# so a toolchain security fix simply never arrives. And the official Go image
# sets GOTOOLCHAIN=local, so it never fetches a newer toolchain on its own -- a
# cached image older than go.mod's `go` line fails the build outright rather
# than adapting, which is how this was found on freizone-server.
#
# This must stay >= the `go` line in go.mod; move the two together. Bump it on
# Go patch releases to pick up standard-library security fixes: this is what
# determines the toolchain the shipped binary is actually built with, so it --
# not the go.mod line -- is what closes those. `govulncheck ./...` reports when.
FROM golang:1.26.7-alpine AS build
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
