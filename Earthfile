VERSION 0.8
FROM golang:1.27
WORKDIR /src

# Cache dependency downloads independently of source changes.
deps:
    COPY go.mod go.sum ./
    RUN go mod download

source:
    FROM +deps
    COPY cmd ./cmd
    COPY internal ./internal
    COPY testdata ./testdata

# Export a standalone binary as well as an artifact for the runtime image.
build:
    FROM +source
    RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/earth-profile ./cmd/earth-profile
    SAVE ARTIFACT /out/earth-profile /earth-profile AS LOCAL bin/earth-profile

# Cross-compile the same CLI/TUI for downloadable release assets.
release-binary:
    FROM +source
    ARG GOOS=linux
    ARG GOARCH=amd64
    ARG EXT=""
    RUN CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -trimpath -ldflags="-s -w" -o /out/earth-profile$EXT ./cmd/earth-profile
    SAVE ARTIFACT /out/earth-profile$EXT /earth-profile$EXT AS LOCAL dist/$GOOS-$GOARCH/earth-profile$EXT

test:
    FROM +source
    RUN go test -race ./...

lint:
    FROM +source
    RUN test -z "$(gofmt -l cmd internal)"
    RUN go vet ./...

# Load the non-root server image into the local container runtime.
image:
    FROM alpine:3.23
    RUN addgroup -g 10001 profiler && adduser -D -u 10001 -G profiler profiler && mkdir /data && chown profiler:profiler /data
    COPY +build/earth-profile /usr/local/bin/earth-profile
    USER 10001:10001
    VOLUME /data
    EXPOSE 8080
    ENTRYPOINT ["earth-profile", "--db", "/data/runs.db", "serve"]
    CMD ["--listen", "0.0.0.0:8080"]
    ARG SOURCE=https://github.com/rdunne2020/earthbuild-profiler
    ARG REVISION=""
    ARG VERSION=dev
    LABEL org.opencontainers.image.source=$SOURCE
    LABEL org.opencontainers.image.revision=$REVISION
    LABEL org.opencontainers.image.version=$VERSION
    ARG IMAGE=ghcr.io/rdunne2020/earthbuild-profiler:latest
    ARG IMAGE_ALIAS=""
    IF [ -n "$IMAGE_ALIAS" ]
        SAVE IMAGE --push $IMAGE $IMAGE_ALIAS
    ELSE
        SAVE IMAGE --push $IMAGE
    END

all:
    BUILD +test
    BUILD +lint
    BUILD +build
    BUILD +image
