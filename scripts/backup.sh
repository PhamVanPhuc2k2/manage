#!/usr/bin/env bash
# Sao lưu database, nén lại và đẩy lên Cloudflare R2.
#
#   bash scripts/backup.sh
#
# Container backup trong docker-compose.prod.yml gọi chính script này theo
# lịch. Chạy tay được, và nên chạy tay trước những thay đổi lớn.
#
# Dùng pg_dump định dạng custom (-Fc) chứ không phải SQL thuần:
#   - nén sẵn, nhỏ hơn khoảng 5 lần
#   - pg_restore phục hồi SONG SONG được (-j), nhanh hơn nhiều khi khôi phục
#   - chọn được bảng cụ thể lúc khôi phục, không phải nạp cả file

set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
set -a; [ -f .env ] && . ./.env; set +a

STAMP="$(date +%Y%m%d-%H%M%S)"
NAME="manage-${STAMP}.dump"
LOCAL_DIR="${BACKUP_DIR:-./backups}"
KEEP_DAYS="${BACKUP_KEEP_DAYS:-30}"

mkdir -p "$LOCAL_DIR"

log() { printf '\033[36m▶\033[0m %s\n' "$*"; }
ok()  { printf '\033[32m✓\033[0m %s\n' "$*"; }
die() { printf '\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

# ----------------------------------------------------------------- dump
log "Sao lưu database..."

# Chạy pg_dump TRONG container postgres: phiên bản pg_dump phải khớp phiên bản
# server, và cách này bảo đảm điều đó mà không cần cài client trên host.
docker compose exec -T postgres pg_dump \
  -U "${POSTGRES_USER:-manage}" \
  -d "${POSTGRES_DB:-manage}" \
  -Fc --no-owner --no-acl \
  > "$LOCAL_DIR/$NAME" || die "pg_dump thất bại"

SIZE="$(du -h "$LOCAL_DIR/$NAME" | cut -f1)"

# Kiểm tra file KHÔNG rỗng và đọc được.
#
# pg_dump có thể thoát với mã 0 mà vẫn cho file hỏng nếu kết nối đứt giữa
# chừng. Một bản backup hỏng còn tệ hơn không có backup: nó tạo cảm giác an
# toàn sai, và ta chỉ biết vào đúng lúc cần khôi phục.
docker compose exec -T postgres pg_restore --list /dev/stdin \
  < "$LOCAL_DIR/$NAME" > /dev/null 2>&1 \
  || die "bản backup không đọc được — KHÔNG dùng được để khôi phục"

ok "đã sao lưu: $NAME ($SIZE), đã kiểm tra đọc được"

# ------------------------------------------------------------------ R2
if [ -n "${R2_BUCKET:-}" ] && [ -n "${R2_ACCESS_KEY_ID:-}" ]; then
  log "Đẩy lên Cloudflare R2..."

  docker run --rm \
    -e AWS_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID" \
    -e AWS_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY" \
    -e AWS_DEFAULT_REGION=auto \
    -v "$(pwd)/$LOCAL_DIR:/backup:ro" \
    amazon/aws-cli:2.22.35 \
    s3 cp "/backup/$NAME" "s3://$R2_BUCKET/backups/$NAME" \
      --endpoint-url "https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com" \
    || die "đẩy lên R2 thất bại — bản backup vẫn còn ở $LOCAL_DIR/$NAME"

  ok "đã đẩy lên R2: backups/$NAME"
else
  # Không có R2 thì vẫn giữ bản local, nhưng phải nói rõ.
  #
  # Backup nằm cùng máy với database KHÔNG bảo vệ được trước hỏng ổ đĩa hay
  # mất máy chủ — đúng hai tình huống người ta cần backup nhất.
  printf '\033[33m!\033[0m Chưa cấu hình R2 — backup chỉ nằm ở %s trên CÙNG máy với database.\n' "$LOCAL_DIR"
  printf '  Điều đó không bảo vệ được trước hỏng ổ đĩa hay mất máy chủ.\n'
fi

# ----------------------------------------------------------- dọn bản cũ
log "Dọn bản local cũ hơn $KEEP_DAYS ngày..."
find "$LOCAL_DIR" -name 'manage-*.dump' -type f -mtime "+$KEEP_DAYS" -print -delete

# Dọn trên R2 theo cùng chính sách. Làm bằng lifecycle rule của R2 thì tốt
# hơn (không phụ thuộc script chạy đúng), nhưng ở đây làm cả hai cho chắc.
if [ -n "${R2_BUCKET:-}" ] && [ -n "${R2_ACCESS_KEY_ID:-}" ]; then
  CUTOFF="$(date -d "-${KEEP_DAYS} days" +%Y-%m-%d 2>/dev/null \
            || date -v-"${KEEP_DAYS}"d +%Y-%m-%d)"
  log "Dọn bản trên R2 cũ hơn $CUTOFF..."

  docker run --rm \
    -e AWS_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID" \
    -e AWS_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY" \
    -e AWS_DEFAULT_REGION=auto \
    amazon/aws-cli:2.22.35 \
    s3 ls "s3://$R2_BUCKET/backups/" \
      --endpoint-url "https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com" \
    2>/dev/null | awk -v cutoff="$CUTOFF" '$1 < cutoff {print $4}' \
    | while read -r old; do
        [ -n "$old" ] || continue
        docker run --rm \
          -e AWS_ACCESS_KEY_ID="$R2_ACCESS_KEY_ID" \
          -e AWS_SECRET_ACCESS_KEY="$R2_SECRET_ACCESS_KEY" \
          -e AWS_DEFAULT_REGION=auto \
          amazon/aws-cli:2.22.35 \
          s3 rm "s3://$R2_BUCKET/backups/$old" \
            --endpoint-url "https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com" \
          && echo "  đã xoá $old"
      done
fi

ok "Sao lưu hoàn tất"
