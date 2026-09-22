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
# KHÔNG truyền `/dev/stdin` làm tên tệp cho pg_restore.
#
# Đưa một TÊN TỆP vào thì pg_restore mở nó như tệp thường và cần nhảy đểc
# (seek) để đọc mục lục của định dạng custom — mà `/dev/stdin` ở đây là một
# ống, không nhảy đểc được. Nó báo "did not find magic string in file
# header", nghe y hệt như tệp hỏng.
#
# Không truyền tên tệp thì pg_restore đọc thẳng stdin theo luồng, không cần
# nhảy đểc, và chạy đúng.
#
# Đây không phải chuyện riêng của Windows: pg_restore chạy trong container
# Linux ở mọi nền tảng, và đã kiểm chứng bằng cách chạy cả hai cách ngay
# trong container.
docker compose exec -T postgres pg_restore --list \
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

# Chép bản dump VÀO container trước khi phục hồi.
#
# BẮT BUỘC, không phải cho gọn. `pg_restore -j` từ chuỗi chuẩn vào với
# thông báo "parallel restore from standard input is not supported": phục hồi
# song song cần nhảy đểc trong tệp để nhiều tiến trình đọc các phần khác
# nhau cùng lúc, mà một ống thì không nhảy đểc được.
#
# Bỏ -j đi thì đỡ phải chép, nhưng đánh đổi sai chỗ: phục hồi song song
# chính là một trong những lý do chọn định dạng custom khi sao lưu, và
# phút giây lúc đang khôi phục sự cố là thứ đắt nhất trong cả quy trình.
#
# MSYS_NO_PATHCONV=1: trên Git Bash (Windows), đường dẫn kiểu Unix trong
# tham số bị viết lại thành đường dẫn Windows trước khi tới container, nên
# `/var/tmp/...` thành `C:/Program Files/Git/var/tmp/...` và pg_restore báo
# không tìm thấy tệp.
REMOTE_DUMP=/var/tmp/manage-restore.dump
PG_CID="$(docker compose ps -q postgres)"
[ -n "$PG_CID" ] || die "không tìm thấy container postgres"

log "Chép bản dump vào container..."
MSYS_NO_PATHCONV=1 docker cp "$PATH_LOCAL" "$PG_CID:$REMOTE_DUMP" \
  || die "không chép được bản dump vào container"

# Dù script thoát giữa chừng cũng không để lại bản sao dữ liệu trong
# container. Backup chứa toàn bộ hồ sơ nhân sự và bảng lương.
cleanup_dump() {
  MSYS_NO_PATHCONV=1 docker exec "$PG_CID" rm -f "$REMOTE_DUMP" >/dev/null 2>&1 || true
}
trap cleanup_dump EXIT

log "Khôi phục..."
# --clean --if-exists: xoá đối tượng cũ trước khi tạo lại, không báo lỗi nếu
# chưa có. Thiếu nó thì mọi CREATE TABLE đều lỗi "đã tồn tại".
MSYS_NO_PATHCONV=1 docker compose exec -T postgres pg_restore \
  -U "${POSTGRES_USER:-manage}" \
  -d "${POSTGRES_DB:-manage}" \
  --clean --if-exists --no-owner --no-acl -j 4 \
  "$REMOTE_DUMP" || printf '\033[33m!\033[0m pg_restore báo một số cảnh báo (thường vô hại với --clean)\n'

log "Chạy migration (bản backup có thể cũ hơn schema hiện tại)..."
docker compose run --rm migrate || die "migration thất bại sau khôi phục"

log "Khởi động lại api và worker..."
docker compose up -d api worker >/dev/null

# Nạp lại nginx: api vừa được dựng lại nên có địa chỉ mới, mà nginx chỉ
# phân giải tên `api` một lần lúc nạp cấu hình. Cùng lý do với bước tương
# tự cuối scripts/deploy.sh — xem ghi chú upstream trong
# docker/nginx/conf.d/app.conf.
#
# Không chặn nếu hỏng: có triển khai không dùng nginx trong cùng compose,
# và dữ liệu đã khôi phục xong rồi — báo hỏng lúc này chỉ khiến người
# đang xử lý sự cố tưởng việc khôi phục thất bại.
docker compose exec -T nginx nginx -s reload >/dev/null 2>&1   && ok "đã nạp lại nginx"   || printf '[33m![0m không nạp lại được nginx — nếu dùng nginx, hãy tự chạy `nginx -s reload`
'

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
