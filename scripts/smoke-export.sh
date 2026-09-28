#!/usr/bin/env bash
# Kiểm chứng xuất báo cáo chấm công ra Excel.
#
# Chạy: ADMIN_PASS='...' bash scripts/smoke-export.sh
#
# Đi hết đường thật: api ghi nhận → RabbitMQ → worker đọc bảng công và dựng
# tệp .xlsx → thông báo → tải tệp về và mở ra kiểm. Dọn lượt xuất ở cuối.

set -uo pipefail
cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
set -a; [ -f .env ] && . ./.env; set +a

BASE="${PUBLIC_BASE_URL:-http://localhost}/api/v1"
# Đăng nhập thẳng vào api, bỏ qua giới hạn tốc độ đăng nhập của nginx.
DIRECT="http://localhost:${API_PORT:-8080}/api/v1"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@abc.vn}"
ADMIN_PASS="${ADMIN_PASS:?Đặt biến ADMIN_PASS trước khi chạy}"
MAILHOG="http://localhost:${MAILHOG_UI_PORT:-8025}"

PASS=0
FAIL=0
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

check() {
  if [ "$2" = "$3" ]; then
    printf '  \033[32m✓\033[0m %-54s %s\n' "$1" "$3"
    PASS=$((PASS + 1))
  else
    printf '  \033[31m✗\033[0m %-54s mong %s, được %s\n' "$1" "$2" "$3"
    FAIL=$((FAIL + 1))
  fi
}

JSON='Content-Type: application/json'
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
field() { grep -o "\"$1\":[^,}]*" | head -1 | cut -d: -f2- | tr -d '"'; }

# Bản sao của hàm trong smoke-hr.sh — các script cố ý chạy độc lập.
login_otp() { # login_otp <email> <password>
  local email="$1" pass="$2" res ch otp i body
  curl -s -X DELETE "$MAILHOG/api/v1/messages" >/dev/null
  res=$(curl -s -X POST "$DIRECT/auth/login" -H "$JSON" \
    -d "{\"email\":\"$email\",\"password\":\"$pass\"}")
  if echo "$res" | grep -q '"access_token"'; then
    echo "$res" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4; return 0
  fi
  ch=$(echo "$res" | grep -o '"challenge_id":"[^"]*"' | cut -d'"' -f4)
  [ -n "$ch" ] || return 1
  for i in $(seq 1 40); do
    body=$(curl -s "$MAILHOG/api/v2/messages?limit=1" | awk -F'"Body":"' '{print $2}')
    otp=$(echo "$body" | grep -o '[0-9]\{6\}' | head -1)
    [ -n "$otp" ] && break
    sleep 0.25
  done
  [ -n "$otp" ] || return 1
  curl -s -X POST "$DIRECT/auth/verify-otp" -H "$JSON" \
    -d "{\"challenge_id\":\"$ch\",\"code\":\"$otp\"}" |
    grep -o '"access_token":"[^"]*"' | cut -d'"' -f4
}

docker compose exec -T redis sh -c \
  "redis-cli -a '$REDIS_PASSWORD' --no-auth-warning --scan --pattern 'login_fail:*' \
   | xargs -r redis-cli -a '$REDIS_PASSWORD' --no-auth-warning DEL" >/dev/null 2>&1

TOKEN=$(login_otp "$ADMIN_EMAIL" "$ADMIN_PASS")
AUTH="Authorization: Bearer $TOKEN"

echo
echo "═══ KIỂM CHỨNG XUẤT CHẤM CÔNG RA EXCEL ═══"
echo

YEAR=$(date +%Y)
MONTH=$(date +%-m)
NEXT_YEAR=$((YEAR + 1))

echo "── Yêu cầu sai bị từ chối ngay ──"
check "Tháng chưa tới → 400" 400 \
  "$(code -X POST "$BASE/attendance/exports" -H "$AUTH" -H "$JSON" -d "{\"year\":$NEXT_YEAR,\"month\":1}")"
check "Tháng 13 → 400" 400 \
  "$(code -X POST "$BASE/attendance/exports" -H "$AUTH" -H "$JSON" -d "{\"year\":$YEAR,\"month\":13}")"

echo "── Xuất tháng này ──"
RES=$(curl -s -w '\n%{http_code}' -X POST "$BASE/attendance/exports" -H "$AUTH" -H "$JSON" \
  -d "{\"year\":$YEAR,\"month\":$MONTH}")
check "Yêu cầu → 202 (đã nhận, đang dựng tệp)" 202 "$(echo "$RES" | tail -1)"
EXP_ID=$(echo "$RES" | head -n -1 | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

STATUS=""
for i in $(seq 1 40); do
  ROW=$(curl -s "$BASE/attendance/exports" -H "$AUTH" | grep -o "{\"id\":\"$EXP_ID\"[^}]*}")
  STATUS=$(echo "$ROW" | field status)
  [ "$STATUS" = "done" ] || [ "$STATUS" = "failed" ] && break
  sleep 0.5
done
check "Worker dựng xong" "done" "$STATUS"
check "Có tệp để tải" "true" "$(echo "$ROW" | field has_file)"

echo "── Tải tệp và mở ra kiểm ──"
HDR="$TMP/hdr.txt"
check "Tải tệp → 200" 200 \
  "$(curl -s -D "$HDR" -o "$TMP/bc.xlsx" -w '%{http_code}' "$BASE/attendance/exports/$EXP_ID/file" -H "$AUTH")"
check "Content-Type là xlsx" "yes" \
  "$(grep -qi 'spreadsheetml.sheet' "$HDR" && echo yes || echo no)"
check "Không cho lưu đệm (no-store)" "yes" \
  "$(grep -qi 'cache-control: no-store' "$HDR" && echo yes || echo no)"
# Mở bằng thư viện zip — .xlsx là một tệp zip. Có đủ hai trang tính mới là
# tệp Excel dùng được thật, không chỉ là vài byte trả về với mã 200.
SHEETS=$(python -c "
import zipfile,sys
z=zipfile.ZipFile(sys.argv[1])
wb=z.read('xl/workbook.xml').decode()
print('yes' if 'Tổng hợp' in wb and 'Chi tiết' in wb else 'no')
" "$TMP/bc.xlsx" 2>/dev/null)
check "Tệp mở được, có trang Tổng hợp và Chi tiết" "yes" "${SHEETS:-no}"
check "Có người trong báo cáo" "yes" \
  "$([ "$(echo "$ROW" | field employee_count)" -gt 0 ] 2>/dev/null && echo yes || echo no)"

NOTI=$(curl -s "$BASE/notifications?page_size=5" -H "$AUTH")
check "Người yêu cầu nhận thông báo trỏ về trang xuất" "yes" \
  "$(echo "$NOTI" | grep -q '"link":"/attendance/exports"' && echo yes || echo no)"

check "Tệp không tồn tại → 404" 404 \
  "$(code "$BASE/attendance/exports/00000000-0000-0000-0000-000000000000/file" -H "$AUTH")"

# --------------------------------------------------------------------- dọn dẹp
echo
echo "── Dọn dữ liệu kiểm thử ──"
docker compose exec -T postgres psql -U "${POSTGRES_USER:-manage}" -d "${POSTGRES_DB:-manage}" -q \
  -c "DELETE FROM attendance_exports WHERE id = '$EXP_ID'" >/dev/null
echo "  đã xoá lượt xuất vừa tạo"

echo
echo "═══════════════════════════════════════"
printf '  Đạt: \033[32m%d\033[0m   Hỏng: \033[31m%d\033[0m\n' "$PASS" "$FAIL"
echo "═══════════════════════════════════════"
echo

[ "$FAIL" -eq 0 ]
