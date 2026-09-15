# syntax=docker/dockerfile:1.7

# =============================================================================
# base — phần dùng chung: toolchain Go + dependency đã tải.
# Tách riêng để tầng cache dependency không bị xoá mỗi khi sửa code.
# =============================================================================
FROM golang:1.25-alpine AS base

WORKDIR /app

RUN apk add --no-cache git ca-certificates tzdata

# Copy go.mod/go.sum TRƯỚC, tải dependency, RỒI mới copy code.
# Nhờ thứ tự này, sửa code không làm mất cache dependency —
# khác biệt giữa build 5 giây và build 3 phút.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# =============================================================================
# dev — dùng cho docker-compose.override.yml. Có Air, có toolchain đầy đủ.
# Source code KHÔNG copy vào image mà bind mount lúc chạy.
# =============================================================================
FROM base AS dev

ARG BINARY
ENV BINARY=${BINARY}

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go install github.com/air-verse/air@v1.61.7

EXPOSE 8080

# Mỗi binary có file cấu hình Air riêng.
CMD ["sh", "-c", "air -c .air.${BINARY}.toml"]

# =============================================================================
# builder — biên dịch binary tĩnh cho production.
# =============================================================================
FROM base AS builder

ARG BINARY
ARG VERSION=dev
ARG GIT_SHA=unknown
ARG BUILD_TIME=unknown

COPY . .

# CGO_ENABLED=0  → binary tĩnh, chạy được trên image không có libc
# -trimpath       → bỏ đường dẫn máy build khỏi binary (build tái lập được)
# -s -w           → bỏ ký hiệu debug, giảm khoảng 30% dung lượng
# -X              → nhúng thông tin build vào biến trong package main
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w \
        -X main.version=${VERSION} \
        -X main.gitSHA=${GIT_SHA} \
        -X main.buildTime=${BUILD_TIME}" \
      -o /out/server \
      ./cmd/${BINARY}

# =============================================================================
# prod — image cuối cùng. Chỉ chứa binary, chứng chỉ CA và dữ liệu múi giờ.
# Mục tiêu: dưới 30MB.
# =============================================================================
FROM alpine:3.21 AS prod

# wget và pgrep đã có sẵn trong busybox của alpine nên healthcheck dùng được
# ngay, không cần cài thêm gói.
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 1001 -S app && \
    adduser -u 1001 -S app -G app -H -D

COPY --from=builder /out/server /usr/local/bin/server

# Chạy bằng user thường. Nếu container bị chiếm quyền,
# kẻ tấn công không có root.
USER app

ENV TZ=Asia/Ho_Chi_Minh

# ENTRYPOINT dạng exec form → binary chạy ở PID 1 → nhận trực tiếp SIGTERM
# từ `docker stop`. Dùng shell form ("/bin/sh -c ...") sẽ khiến shell nuốt
# mất tín hiệu và graceful shutdown không bao giờ chạy.
ENTRYPOINT ["/usr/local/bin/server"]
