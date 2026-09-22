#!/usr/bin/env bash
# Triển khai một phiên bản lên production.
#
#   IMAGE_TAG=<git-sha> bash scripts/deploy.sh
#
# Thứ tự các bước KHÔNG được đổi:
#
#   1. Kéo image mới về TRƯỚC khi dừng gì cả. Kéo image mất vài chục giây, và
#      làm việc đó sau khi đã dừng container nghĩa là hệ thống chết trong suốt
#      thời gian đó — kể cả khi image bị lỗi và ta phải quay lui.
#   2. Chạy migration TRƯỚC khi khởi động code mới. Code mới có thể cần cột
#      mới; code cũ vẫn chạy được trên schema mới vì migration của dự án này
#      chỉ thêm, không xoá cột. Đó là điều kiện để quay lui an toàn.
#   3. Khởi động lại api TỪNG CÁI, chờ container mới lành mới sang cái tiếp
#      theo. Dừng cả hai một lúc là một khoảng trắng nhìn thấy được.
#
# Script này KHÔNG tự chạy backup: backup theo lịch đã có container riêng, và
# một bản backup sát giờ triển khai không cứu được gì mà chỉ làm việc triển
# khai chậm thêm vài phút. Muốn chắc thì chạy scripts/backup.sh trước.

set -euo pipefail
cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
set -a; [ -f .env ] && . ./.env; set +a

IMAGE_TAG="${IMAGE_TAG:?Đặt IMAGE_TAG (thường là git SHA) trước khi chạy}"
export IMAGE_TAG

COMPOSE="docker compose -f docker-compose.yml -f docker-compose.prod.yml"

# Số replica api phải khớp docker-compose.prod.yml. Đọc từ chính compose để
# hai chỗ không lệch nhau khi ai đó đổi số replica.
API_REPLICAS="$($COMPOSE config --format json 2>/dev/null \
  | grep -o '"replicas":[0-9]*' | head -1 | cut -d: -f2)"
API_REPLICAS="${API_REPLICAS:-2}"

log() { printf '\033[36m▶\033[0m %s\n' "$*"; }
ok()  { printf '\033[32m✓\033[0m %s\n' "$*"; }
die() { printf '\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

log "Triển khai phiên bản $IMAGE_TAG"

# ------------------------------------------------------------------ 1. kéo
log "Kéo image..."
$COMPOSE pull api worker frontend || die "không kéo được image $IMAGE_TAG"
ok "đã có image"

# ------------------------------------------------------- 2. migration
log "Chạy migration..."
$COMPOSE run --rm migrate || die "migration thất bại — DỪNG, không khởi động code mới"
ok "migration xong"

# ------------------------------------------- 3. khởi động lại lần lượt
# waitHealthy chờ một container về trạng thái healthy.
#
# Chờ theo HEALTHCHECK của Docker, không phải `sleep`: thời gian khởi động phụ
# thuộc vào tải của máy, và một con số sleep cố định thì hoặc quá ngắn (khởi
# động lại cái tiếp theo khi cái này chưa nhận traffic) hoặc quá dài.
wait_healthy() { # wait_healthy <container-id> <nhãn>
  local id="$1" label="$2" i status
  for i in $(seq 1 60); do
    status="$(docker inspect --format '{{.State.Health.Status}}' "$id" 2>/dev/null || echo missing)"
    case "$status" in
      healthy) ok "$label đã lành"; return 0 ;;
      unhealthy) die "$label báo unhealthy — dừng triển khai" ;;
    esac
    sleep 2
  done
  die "$label không lành sau 120 giây"
}

log "Khởi động lại worker..."
$COMPOSE up -d --no-deps worker
ok "worker đã khởi động lại"

log "Khởi động lại api từng cái ($API_REPLICAS replica)..."
# --scale bằng số hiện tại + up -d sẽ thay lần lượt. Cách chắc chắn hơn là
# dừng và dựng lại từng container theo id.
mapfile -t API_IDS < <($COMPOSE ps -q api)

if [ "${#API_IDS[@]}" -eq 0 ]; then
  # Lần triển khai đầu: chưa có container nào.
  $COMPOSE up -d --no-deps api
else
  for id in "${API_IDS[@]}"; do
    short="${id:0:12}"
    log "  thay $short"

    # stop trước rồi rm: stop tôn trọng stop_grace_period 60 giây, đủ để api
    # đóng sạch kết nối WebSocket. `rm -f` bỏ qua khoảng chờ đó và cắt ngang
    # mọi kết nối đang mở.
    docker stop "$id" >/dev/null
    docker rm "$id" >/dev/null

    $COMPOSE up -d --no-deps --no-recreate api
    new_id="$($COMPOSE ps -q api | head -1)"
    wait_healthy "$new_id" "api $short"
  done
fi

log "Khởi động lại frontend..."
$COMPOSE up -d --no-deps frontend
ok "frontend đã khởi động lại"

# Nạp lại nginx SAU khi đã thay xong toàn bộ api và frontend.
#
# BẮT BUỘC, không phải cho chắc. nginx bản mã nguồn mở phân giải tên trong
# khối `upstream` ĐÚNG MỘT LẦN lúc nạp cấu hình, rồi giữ nguyên danh sách IP
# đó mãi. Vòng thay container ở trên cấp IP mới cho mỗi api, nên sau khi
# chạy xong nginx vẫn đang trỏ tới những địa chỉ không còn ai ở đó — toàn bộ
# lưu lượng thành 502 cho tới lần nạp lại định kỳ sáu tiếng sau.
#
# Đây không phải suy đoán: đo trên stack thật, sau khi scale lên ba bản thì
# cả ba mươi request liên tiếp vẫn vào đúng một container, và chỉ chia đều
# 10/10/10 sau khi nạp lại nginx.
#
# `nginx -s reload` giữ các worker cũ sống cho tới khi kết nối hiện có đóng
# hết, nên WebSocket đang mở được rút dần chứ không bị cắt ngang.
log "Nạp lại nginx để nhận địa chỉ mới của api..."
$COMPOSE exec -T nginx nginx -s reload || die "không nạp lại được nginx"
ok "nginx đã nạp lại"

# ------------------------------------------------------------ nghiệm thu
log "Kiểm tra sau triển khai..."

READY="$(curl -fsS "${PUBLIC_BASE_URL:-http://localhost}/ready" || echo FAIL)"
echo "$READY" | grep -q '"postgres":"ok"' || die "hệ thống không sẵn sàng: $READY"
ok "/ready xanh"

VERSION="$(curl -fsS "${PUBLIC_BASE_URL:-http://localhost}/health" || echo '')"
ok "phiên bản đang chạy: $VERSION"

echo
ok "Triển khai $IMAGE_TAG hoàn tất"
echo "  Quay lui: IMAGE_TAG=<sha-cũ> bash scripts/rollback.sh"
