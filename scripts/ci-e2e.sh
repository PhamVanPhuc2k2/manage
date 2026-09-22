#!/usr/bin/env bash
# Dựng stack sạch, seed dữ liệu, rồi chạy cả 6 bộ smoke.
#
#   bash scripts/ci-e2e.sh
#
# Dùng ở hai chỗ: CI gọi thẳng script này, và người phát triển chạy nó trước
# khi mở pull request. Một script cho cả hai chỗ để không bao giờ có chuyện
# "chạy ở máy tôi thì được" — cùng một đường đi, cùng một kết quả.
#
# VÌ SAO LÀ SCRIPT CHỨ KHÔNG PHẢI CÁC BƯỚC TRONG YAML
#
# Các bước viết trong YAML chỉ chạy được trên runner. Chúng không kiểm thử
# được ở máy, nên mỗi lần sửa là một lần đẩy commit lên rồi chờ xem CI đỏ hay
# xanh. Gói vào script thì kiểm tại chỗ được, và job CI thu lại còn một dòng.
#
# Script CÓ XOÁ DỮ LIỆU: nó gọi `docker compose down -v`, tức là xoá cả
# volume. Đừng chạy trên máy đang có dữ liệu cần giữ.

set -uo pipefail
cd "$(dirname "$0")/.."

log()  { printf '\033[36m▶\033[0m %s\n' "$*"; }
ok()   { printf '\033[32m✓\033[0m %s\n' "$*"; }
die()  { printf '\033[31m✗\033[0m %s\n' "$*" >&2; exit 1; }

KEEP="${KEEP_STACK:-0}"

cleanup() {
  if [ "$KEEP" = "1" ]; then
    printf '\033[33m!\033[0m Giữ lại stack theo KEEP_STACK=1\n'
    return
  fi
  log "Dọn stack..."
  docker compose down -v >/dev/null 2>&1
}
trap cleanup EXIT

# --------------------------------------------------------------------- .env
#
# .env không nằm trong kho mã (nó chứa mật khẩu). Dựng một bản dùng một lần.
#
# Mật khẩu ở đây là giá trị cho một stack sẽ bị xoá sau vài phút, không phải
# bí mật — đặt chúng thành secret của kho mã chỉ tạo cảm giác an toàn chứ
# không bảo vệ thêm điều gì.
if [ ! -f .env ] || [ "${FORCE_ENV:-0}" = "1" ]; then
  log "Dựng .env cho lần chạy..."
  cp .env.example .env
  {
    echo "POSTGRES_PASSWORD=e2e-pg-$RANDOM$RANDOM"
    echo "RABBITMQ_PASSWORD=e2e-mq-$RANDOM$RANDOM"
    echo "REDIS_PASSWORD=e2e-redis-$RANDOM$RANDOM"
    echo "GRAFANA_PASSWORD=e2e-grafana-$RANDOM$RANDOM"
    echo "JWT_SECRET=$(openssl rand -hex 32)"
    echo "NGINX_PORT=8088"
    echo "PUBLIC_BASE_URL=http://localhost:8088"
  } >> .env
  ok ".env đã dựng"
else
  ok "dùng .env sẵn có"
fi

# shellcheck disable=SC1091
set -a; . ./.env; set +a

API="http://localhost:${API_PORT:-8080}/api/v1"
MAILHOG="http://localhost:${MAILHOG_UI_PORT:-8025}"

# -------------------------------------------------------------- dựng stack
#
# Xoá sạch TRƯỚC khi dựng, kể cả volume.
#
# BẮT BUỘC, không phải cho gọn. PostgreSQL chỉ đọc POSTGRES_PASSWORD khi
# khởi tạo cụm dữ liệu lần đầu. Còn volume cũ thì mật khẩu cũ ở nguyên,
# còn .env vừa dựng lại mang mật khẩu mới — và migrate chết với "password
# authentication failed" trong khi postgres vẫn báo healthy.
#
# Trên runner CI thì máy luôn sạch nên không ai gặp lỗi này. Ở máy người
# phát triển thì gặp ngay lần đầu, và thông báo không gợi ý gì về volume.
log "Xoá stack cũ và volume..."
docker compose down -v --remove-orphans >/dev/null 2>&1

log "Dựng stack (có thể mất vài phút lần đầu)..."
docker compose up -d --build --wait || {
  docker compose ps
  docker compose logs --tail 100 api worker
  die "stack không lên"
}
ok "stack đã lên"

# ------------------------------------------------------------------- seed
#
# Seed sinh mật khẩu NGẪU NHIÊN và in ra đúng một lần, nên phải bắt lấy nó
# từ stdout. Không thêm cờ --password vào lệnh seed chỉ để tiện cho CI: một
# cờ như vậy rồi sẽ có người dùng ở production.
log "Seed công ty và tài khoản quản trị..."
SEED_OUT="$(docker compose run --rm -T api go run ./cmd/seed \
  --company "Công ty kiểm thử" --email admin@abc.vn --name "Quản trị viên" 2>&1)" \
  || { echo "$SEED_OUT"; die "seed thất bại"; }

# Dòng mật khẩu có dạng: ║  Mật khẩu:  <giá trị>   ║
TEMP_PASS="$(echo "$SEED_OUT" | grep -a 'Mật khẩu:' | head -1 \
  | sed 's/.*Mật khẩu:[[:space:]]*//' | sed 's/[[:space:]]*║.*//')"
[ -n "$TEMP_PASS" ] || { echo "$SEED_OUT"; die "không đọc được mật khẩu tạm từ seed"; }
ok "đã seed, có mật khẩu tạm"

# ------------------------------------------- đồng bộ nhóm chat tự động
#
# Worker đồng bộ nhóm chat theo phòng ban và dự án lúc KHỎI ĐỘNG, rồi mỗi 15
# phút. Ở đây worker lên trước khi seed tạo phòng ban, nên lần chạy đầu
# không thấy gì và lần sau thì quá muộn so với bộ smoke.
#
# Khởi động lại worker để job đó chạy lại với dữ liệu đã có. Đây không
# phải mẹo của riêng bộ kiểm thử: một hệ thống thật vừa cài xong cũng ở đúng
# tình trạng này, và người vận hành hoặc chờ 15 phút hoặc làm đúng việc này.
log "Khởi động lại worker để đồng bộ nhóm chat..."
docker compose restart worker >/dev/null 2>&1 || die "không khởi động lại được worker"

for i in $(seq 1 30); do
  if [ "$(docker compose ps worker --format '{{.Health}}' 2>/dev/null)" = "healthy" ]; then
    break
  fi
  sleep 2
done
ok "worker đã chạy lại"

# ------------------------------------------------ đổi mật khẩu lần đầu
#
# Seed đặt must_change_password, nên tài khoản chưa dùng được cho các bộ
# smoke. Đi đúng luồng thật để đổi: đăng nhập (kèm OTP nếu đang bật), rồi
# gọi endpoint đổi mật khẩu.
#
# Làm bằng API chứ không sửa thẳng database: nếu luồng đổi mật khẩu bắt buộc
# này hỏng, một người dùng mới cũng sẽ không vào được hệ thống — và đó là
# thứ đáng để CI phát hiện.
E2E_PASS="E2eSmoke#$(date +%s)"

otp_code() {
  local i body
  for i in $(seq 1 40); do
    body="$(curl -s "$MAILHOG/api/v2/messages?limit=1" | awk -F'"Body":"' '{print $2}')"
    case "$body" in
      *[0-9][0-9][0-9][0-9][0-9][0-9]*)
        echo "$body" | grep -o '[0-9]\{6\}' | head -1
        return 0 ;;
    esac
    sleep 0.25
  done
  return 1
}

log "Đăng nhập lần đầu và đổi mật khẩu..."
curl -s -X DELETE "$MAILHOG/api/v1/messages" >/dev/null

STEP1="$(curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"admin@abc.vn\",\"password\":\"$TEMP_PASS\"}")"

TOKEN="$(echo "$STEP1" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)"
if [ -z "$TOKEN" ]; then
  CH="$(echo "$STEP1" | grep -o '"challenge_id":"[^"]*"' | cut -d'"' -f4)"
  [ -n "$CH" ] || { echo "$STEP1"; die "đăng nhập lần đầu thất bại"; }

  OTP="$(otp_code)" || die "không nhận được mã OTP qua MailHog"
  TOKEN="$(curl -s -X POST "$API/auth/verify-otp" -H 'Content-Type: application/json' \
    -d "{\"challenge_id\":\"$CH\",\"code\":\"$OTP\"}" \
    | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)"
fi
[ -n "$TOKEN" ] || die "không lấy được access token"

CODE="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/auth/change-password" \
  -H 'Content-Type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d "{\"old_password\":\"$TEMP_PASS\",\"new_password\":\"$E2E_PASS\"}")"
[ "$CODE" = "200" ] || [ "$CODE" = "204" ] || die "đổi mật khẩu trả $CODE"
ok "mật khẩu quản trị đã sẵn sàng"

# --------------------------------------------- nhân viên thứ hai
#
# `cmd/seed` chỉ tạo đúng một người: tài khoản quản trị. Với một công ty
# một người, smoke-project BỎQUA sáu phép thử — đúng những phép đáng giá
# nhất: thêm và đổi vai trò thành viên, và đường phát sự kiện khi giao việc
# cho người khác.
#
# Những phép thử bị bỏ qua không báo hỏng, nên một lần CI xanh vẫn trông y
# hệt như đã kiểm đủ. Tạo sẵn một đồng nghiệp để chúng chạy thật.
log "Tạo nhân viên thứ hai..."
DEPT_ID="$(curl -s "$API/departments" -H "Authorization: Bearer $TOKEN"   | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)"

cat > /tmp/e2e-colleague.json <<JSON
{
  "employee_code": "NV0002",
  "full_name": "Trần Thị Bình",
  "email": "binh@abc.vn",
  "department_id": ${DEPT_ID:+\"$DEPT_ID\"},
  "work_mode": "onsite",
  "status": "official",
  "joined_at": "2024-01-01"
}
JSON
# Bỏ trường department_id nếu không tra được phòng ban.
[ -n "$DEPT_ID" ] || sed -i '/department_id/d' /tmp/e2e-colleague.json

CODE="$(curl -s -o /tmp/e2e-colleague.out -w '%{http_code}'   -X POST "$API/employees" -H 'Content-Type: application/json'   -H "Authorization: Bearer $TOKEN" --data-binary @/tmp/e2e-colleague.json)"
if [ "$CODE" = "201" ] || [ "$CODE" = "200" ]; then
  ok "đã tạo nhân viên thứ hai"
else
  cat /tmp/e2e-colleague.out
  die "tạo nhân viên thứ hai trả $CODE"
fi

# ------------------------------------------------------------ chạy smoke
export ADMIN_PASS="$E2E_PASS"

FAILED=""
for suite in auth hr project attendance payroll chat; do
  echo
  log "smoke-$suite"
  if bash "scripts/smoke-$suite.sh"; then
    ok "smoke-$suite đạt"
  else
    FAILED="$FAILED $suite"
    printf '\033[31m✗\033[0m smoke-%s HỎNG\n' "$suite"
  fi
done

echo
if [ -n "$FAILED" ]; then
  # In log TRƯỚC khi thoát. Không có nó thì một lần CI đỏ chỉ nói "smoke
  # hỏng", và người sửa phải dựng lại toàn bộ ở máy mình để đoán.
  printf '\033[31m✗\033[0m Bộ hỏng:%s\n' "$FAILED"
  echo
  log "200 dòng log cuối của api, worker và nginx:"
  docker compose logs --tail 200 api worker nginx
  exit 1
fi

ok "Cả 6 bộ smoke đều đạt"
