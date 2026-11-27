# Build stage
FROM golang:1.27.4-alpine AS builder
RUN apk add --no-cache build-base sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p bin
ARG VERSION=dev
ENV CGO_ENABLED=1 GOOS=linux
RUN go build -ldflags="-s -w -X main.Version=${VERSION}" -o bin/kaf-mirror ./cmd/kaf-mirror
RUN go build -ldflags="-s -w" -o bin/admin-cli ./cmd/admin-cli
RUN go build -ldflags="-s -w -X main.Version=${VERSION}" -o bin/mirror-cli ./cmd/mirror-cli

# Final stage
FROM alpine:3.21
RUN apk add --no-cache sqlite-libs
WORKDIR /app
RUN addgroup -S kaf-mirror && adduser -S kaf-mirror -G kaf-mirror
COPY --from=builder /app/bin/kaf-mirror .
COPY --from=builder /app/bin/admin-cli .
COPY --from=builder /app/bin/mirror-cli .
COPY configs/default.yml /app/configs/default.yml
COPY web/ /app/web/
RUN mkdir -p /app/data /app/logs && chown kaf-mirror:kaf-mirror /app/data /app/logs
ENV KAF_MIRROR_SERVER_HOST=0.0.0.0
VOLUME ["/app/data", "/app/logs"]
USER kaf-mirror
EXPOSE 8080
CMD ["./kaf-mirror"]
