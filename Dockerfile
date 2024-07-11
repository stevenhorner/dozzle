# Build assets
FROM --platform=$BUILDPLATFORM node:22.4.1-alpine AS node

RUN corepack enable

WORKDIR /build

# Install dependencies from lock file
COPY pnpm-*.yaml ./
RUN pnpm fetch --ignore-scripts --no-optional

# Copy package.json and install dependencies
COPY package.json ./
RUN pnpm install --offline --ignore-scripts --no-optional

# Copy assets and translations to build
COPY .* *.config.ts *.config.js *.config.cjs ./
COPY assets ./assets
COPY locales ./locales
COPY public ./public

# Build assets
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.22.5-alpine AS builder

# install gRPC dependencies
RUN apk add --no-cache ca-certificates protoc protobuf-dev\
    && mkdir /dozzle \
    && go install google.golang.org/protobuf/cmd/protoc-gen-go@latest \
    && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

WORKDIR /dozzle

# Copy go mod files
COPY go.* ./
RUN go mod download

# Copy all other files
COPY internal ./internal
COPY main.go ./
COPY protos ./protos

# Copy assets built with node
COPY --from=node /build/dist ./dist

# Args
ARG TAG=dev
ARG TARGETOS TARGETARCH

# Generate protos
RUN go generate

RUN --mount=type=secret,id=shared_cert.pem cp /run/secrets/shared_cert.pem shared_cert.pem
RUN --mount=type=secret,id=shared_key.pem cp /run/secrets/shared_key.pem shared_key.pem

# Build binary
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -ldflags "-s -w -X github.com/amir20/dozzle/internal/support/cli.Version=$TAG" -o dozzle

RUN mkdir /data

FROM scratch

COPY --from=builder /data /data
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /dozzle/dozzle /dozzle

EXPOSE 8080

ENTRYPOINT ["/dozzle"]
