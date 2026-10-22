# Build stage
FROM golang:1.26.10-alpine AS builder
RUN apk add --no-cache build-base sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN mkdir -p bin
ENV CGO_ENABLED=1 GOOS=linux
RUN go build -ldflags="-s -w" -o bin/kaf-mirror cmd/kaf-mirror/main.go
RUN go build -ldflags="-s -w" -o bin/admin-cli cmd/admin-cli/main.go
RUN go build -ldflags="-s -w" -o bin/mirror-cli cmd/mirror-cli/main.go

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
USER kaf-mirror
EXPOSE 8080
CMD ["./kaf-mirror"]
