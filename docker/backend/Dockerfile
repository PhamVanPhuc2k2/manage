# syntax=docker/dockerfile:1.7

# GHIM BASE IMAGE THEO DIGEST
#
# Thẻ `:1.27-alpine` là con trỏ di động: cùng một Dockerfile dựng hôm nay và
# tháng sau cho ra hai image khác nhau. Khi một bản dựng hỏng, không có cách
# nào dựng lại đúng thứ đã chạy để so sánh — và đó cũng là đường để một
# image bị thay đổi ở thượng nguồn đi thẳng vào production.
#
# Giữ cả thẻ lẫn digest: Docker dùng digest, còn thẻ để người đọc biết
# digest đó ứng với phiên bản nào mà không phải tra.
#
# CẬP NHẬT ĐỊNH KỲ LÀ BẮT BUỘC. Ghim rồi bỏ quên thì image đóng băng
# luôn cả những lỗ hổng đã được vá ở thượng nguồn — tệ hơn hẳn thẻ trôi nổi.
# Chạy `bash scripts/update-base-digests.sh` để lấy digest mới nhất; quyết
# định có cập nhật hay không dựa vào kết quả quét Trivy hàng tuần
# (.github/workflows/security.yml).

# =============================================================================
# base — phần dùng chung: toolchain Go + dependency đã tải.
# Tách riêng để tầng cache dependency không bị xoá mỗi khi sửa code.
#
# KHÔNG dùng `apk add` ở bất kỳ stage nào. Lý do:
#   - Mirror của Alpine thỉnh thoảng lỗi hoặc bị chặn, làm build treo hàng
#     chục phút mà không báo gì rõ ràng.
#   - Thứ ta cần đều có cách khác: chứng chỉ CA copy từ image golang, dữ liệu
#     múi giờ nhúng thẳng vào binary bằng `import _ "time/tzdata"`, còn wget
#     và pgrep thì busybox của alpine đã có sẵn.
#   - Ít phụ thuộc mạng lúc build nghĩa là build tái lập được và nhanh hơn.
#
# Go không cần binary `git`: nó tải module qua GOPROXY (proxy.golang.org),
# chỉ những module trỏ thẳng vào VCS mới cần git — dự án này không có.
# =============================================================================
FROM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS base

WORKDIR /app

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
# prod — image cuối cùng. Chỉ chứa binary và chứng chỉ CA.
# Kích thước thực tế đo được: api 32.5MB, worker 27.1MB.
# =============================================================================
FROM alpine:3.21@sha256:ce64758a109eb420d874a118f87920e625e12d3634e03b4a5573fd9f6e5d3507 AS prod

# Chứng chỉ CA copy từ image golang thay vì cài bằng apk — cần cho mọi
# kết nối TLS đi ra (SMTP, S3, API bên ngoài).
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Chạy bằng user thường. Nếu container bị chiếm quyền, kẻ tấn công không có root.
# addgroup/adduser là applet của busybox, có sẵn trong alpine.
RUN addgroup -g 1001 -S app && \
    adduser -u 1001 -S app -G app -H -D

COPY --from=builder /out/server /usr/local/bin/server

USER app

ENV TZ=Asia/Ho_Chi_Minh

# ENTRYPOINT dạng exec form → binary chạy ở PID 1 → nhận trực tiếp SIGTERM
# từ `docker stop`. Dùng shell form ("/bin/sh -c ...") sẽ khiến shell nuốt
# mất tín hiệu và graceful shutdown không bao giờ chạy.
ENTRYPOINT ["/usr/local/bin/server"]
