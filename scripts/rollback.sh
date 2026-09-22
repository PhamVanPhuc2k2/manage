#!/usr/bin/env bash
# Quay lui về một phiên bản image trước đó.
#
#   IMAGE_TAG=<git-sha-cũ> bash scripts/rollback.sh
#
# Quay lui KHÔNG hoàn tác migration, và đó là có chủ ý.
#
# Migration của dự án này chỉ thêm — không xoá cột, không đổi tên cột, không
# thu hẹp kiểu. Nhờ vậy code phiên bản cũ vẫn chạy được trên schema mới, và
# quay lui là một thao tác an toàn làm được trong một phút.
#
# Điều kiện đó phải được GIỮ khi viết migration mới. Một migration xoá cột sẽ
# làm mọi lần quay lui về trước nó thành không thể — và ta chỉ phát hiện điều
# đó vào đúng lúc đang cần quay lui.

set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
set -a; [ -f .env ] && . ./.env; set +a

IMAGE_TAG="${IMAGE_TAG:?Đặt IMAGE_TAG về phiên bản muốn quay lui}"
export IMAGE_TAG

COMPOSE="docker compose -f docker-compose.yml -f docker-compose.prod.yml"

log() { printf '\033[36m▶\033[0m %s\n' "$*"; }
ok()  { printf '\033[32m✓\033[0m %s\n' "$*"; }
die() { printf '\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

log "Quay lui về $IMAGE_TAG"
printf 'Xác nhận? Thao tác này thay image của api, worker và frontend. [y/N] '
read -r answer
[ "$answer" = "y" ] || die "đã huỷ"

log "Kéo image cũ..."
$COMPOSE pull api worker frontend || die "không kéo được image $IMAGE_TAG"

# Quay lui ưu tiên TỐC ĐỘ hơn tính liên tục: khi đang quay lui thì hệ thống
# vốn đã có vấn đề, và thay tất cả một lượt nhanh hơn thay lần lượt.
log "Thay toàn bộ..."
$COMPOSE up -d --no-deps api worker frontend

log "Kiểm tra..."
for i in $(seq 1 30); do
  if curl -fsS "${PUBLIC_BASE_URL:-http://localhost}/ready" 2>/dev/null \
     | grep -q '"postgres":"ok"'; then
    ok "/ready xanh"
    ok "Đã quay lui về $IMAGE_TAG"
    exit 0
  fi
  sleep 2
done

die "hệ thống không sẵn sàng sau 60 giây — xem docker compose logs"
