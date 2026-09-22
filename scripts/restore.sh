#!/usr/bin/env bash
# Khôi phục database từ một bản backup.
#
#   bash scripts/restore.sh --list              # xem bản có sẵn
#   bash scripts/restore.sh manage-....dump     # khôi phục
#
# Script này GHI ĐÈ database hiện tại. Nó hỏi xác nhận và bắt gõ đúng chữ,
# không phải chỉ "y": khôi phục nhầm lên database production là thao tác
# không hoàn tác được, và một lần bấm y theo phản xạ là đủ để mất dữ liệu.

set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
set -a; [ -f .env ] && . ./.env; set +a

LOCAL_DIR="${BACKUP_DIR:-./backups}"

log() { printf '\033[36m▶\033[0m %s\n' "$*"; }
ok()  { printf '\033[32m✓\033[0m %s\n' "$*"; }
die() { printf '\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

r2() {
  docker run --rm \
    -e AWS_ACCESS_KEY_ID="${R2_ACCESS_KEY_ID:-}" \
    -e AWS_SECRET_ACCESS_KEY="${R2_SECRET_ACCESS_KEY:-}" \
    -e AWS_DEFAULT_REGION=auto \
    -v "$(pwd)/$LOCAL_DIR:/backup" \
    amazon/aws-cli:2.22.35 \
    "$@" --endpoint-url "https://${R2_ACCOUNT_ID:-}.r2.cloudflarestorage.com"
}

# ----------------------------------------------------------------- --list
if [ "${1:-}" = "--list" ]; then
  echo "Bản backup ở máy này ($LOCAL_DIR):"
  ls -lh "$LOCAL_DIR"/manage-*.dump 2>/dev/null | awk '{print "  " $9 "  " $5}' \
    || echo "  (không có)"

  if [ -n "${R2_BUCKET:-}" ] && [ -n "${R2_ACCESS_KEY_ID:-}" ]; then
    echo
    echo "Bản backup trên R2:"
    r2 s3 ls "s3://$R2_BUCKET/backups/" 2>/dev/null \
      | awk '{print "  " $4 "  " $3 " bytes  (" $1 ")"}' || echo "  (không đọc được)"
  fi
  exit 0
fi

FILE="${1:?Dùng: bash scripts/restore.sh <tên-file> | --list}"
PATH_LOCAL="$LOCAL_DIR/$FILE"

# Không có ở máy thì thử tải từ R2.
if [ ! -f "$PATH_LOCAL" ]; then
  [ -n "${R2_BUCKET:-}" ] || die "không có $PATH_LOCAL và chưa cấu hình R2"

  log "Không có ở máy, tải từ R2..."
  r2 s3 cp "s3://$R2_BUCKET/backups/$FILE" "/backup/$FILE" \
    || die "không tải được $FILE từ R2"
  ok "đã tải về"
fi

# Kiểm tra file đọc được TRƯỚC khi xoá gì cả.
#
# Thứ tự này quan trọng: phát hiện file hỏng sau khi đã xoá database hiện tại
# nghĩa là mất cả hai.
log "Kiểm tra bản backup..."
docker compose exec -T postgres pg_restore --list /dev/stdin \
  < "$PATH_LOCAL" > /dev/null 2>&1 \
  || die "bản backup không đọc được — KHÔNG khôi phục"
ok "bản backup đọc được"

# ----------------------------------------------------------- xác nhận
SIZE="$(du -h "$PATH_LOCAL" | cut -f1)"
echo
printf '\033[31m╔════════════════════════════════════════════════════════════╗\033[0m\n'
printf '\033[31m║  GHI ĐÈ TOÀN BỘ DATABASE HIỆN TẠI                          ║\033[0m\n'
printf '\033[31m╚════════════════════════════════════════════════════════════╝\033[0m\n'
echo "  Database : ${POSTGRES_DB:-manage}"
echo "  Từ bản   : $FILE ($SIZE)"
echo "  Môi trường: ${ENV:-development}"
echo
echo "Mọi dữ liệu hiện tại sẽ MẤT. Thao tác này không hoàn tác được."
printf 'Gõ đúng chữ KHOI-PHUC để tiếp tục: '
read -r answer
[ "$answer" = "KHOI-PHUC" ] || die "đã huỷ"

# ------------------------------------------------------------ khôi phục
# Dừng api và worker TRƯỚC khi khôi phục.
#
# Chúng đang giữ kết nối tới database, và pg_restore --clean không xoá được
# bảng khi còn kết nối khác dùng tới. Quan trọng hơn: để chúng chạy nghĩa là
# có thể ghi dữ liệu mới vào giữa lúc khôi phục.
log "Dừng api và worker..."
docker compose stop api worker >/dev/null

log "Khôi phục..."
# --clean --if-exists: xoá đối tượng cũ trước khi tạo lại, không báo lỗi nếu
# chưa có. Thiếu nó thì mọi CREATE TABLE đều lỗi "đã tồn tại".
#
# -j 4: phục hồi song song. Chỉ làm được với định dạng custom (-Fc) — đây là
# một trong những lý do chọn định dạng đó khi sao lưu.
docker compose exec -T postgres pg_restore \
  -U "${POSTGRES_USER:-manage}" \
  -d "${POSTGRES_DB:-manage}" \
  --clean --if-exists --no-owner --no-acl -j 4 \
  < "$PATH_LOCAL" || printf '\033[33m!\033[0m pg_restore báo một số cảnh báo (thường vô hại với --clean)\n'

log "Chạy migration (bản backup có thể cũ hơn schema hiện tại)..."
docker compose run --rm migrate || die "migration thất bại sau khôi phục"

log "Khởi động lại api và worker..."
docker compose up -d api worker >/dev/null

log "Kiểm tra..."
for i in $(seq 1 30); do
  if curl -fsS "${PUBLIC_BASE_URL:-http://localhost}/ready" 2>/dev/null \
     | grep -q '"postgres":"ok"'; then
    ok "/ready xanh"

    COUNT="$(docker compose exec -T postgres psql -U "${POSTGRES_USER:-manage}" \
      -d "${POSTGRES_DB:-manage}" -t -c \
      'SELECT COUNT(*) FROM employees WHERE deleted_at IS NULL;' | tr -d ' ')"
    ok "khôi phục xong — $COUNT nhân viên trong database"
    exit 0
  fi
  sleep 2
done

die "hệ thống không sẵn sàng sau 60 giây — xem docker compose logs"
