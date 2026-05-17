FROM golang:1.25 AS build

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=unknown
ARG BUILD_TIME=unknown

WORKDIR /src/guardian

# Guardian's go.mod replaces github.com/radryc/monofs with ../monofs, and
# monofs/go.mod replaces github.com/rydzu/ainfra/kvs with ../kvs, so builds
# must provide both sibling repos as named build contexts.
COPY go.mod go.sum ./
COPY --from=monofs go.mod /src/monofs/go.mod
COPY --from=monofs go.sum /src/monofs/go.sum
COPY --from=kvs go.mod /src/kvs/go.mod
COPY --from=kvs go.sum /src/kvs/go.sum
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY --from=monofs . /src/monofs
COPY --from=kvs . /src/kvs

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
	-trimpath \
	-ldflags "-s -w -X github.com/rydzu/ainfra/guardian/internal/buildinfo.Version=${BUILD_VERSION} -X github.com/rydzu/ainfra/guardian/internal/buildinfo.Commit=${BUILD_COMMIT} -X github.com/rydzu/ainfra/guardian/internal/buildinfo.BuildTime=${BUILD_TIME}" \
	-o /out/guardiand ./cmd/guardiand

FROM alpine:3.21

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT=unknown
ARG BUILD_TIME=unknown

RUN apk add --no-cache ca-certificates

LABEL org.opencontainers.image.version=${BUILD_VERSION}
LABEL org.opencontainers.image.revision=${BUILD_COMMIT}
LABEL org.opencontainers.image.created=${BUILD_TIME}

COPY --from=build /out/guardiand /usr/local/bin/guardiand
COPY docker/entrypoint.sh /usr/local/bin/guardian-entrypoint

RUN chmod +x /usr/local/bin/guardian-entrypoint

ENTRYPOINT ["/usr/local/bin/guardian-entrypoint"]
