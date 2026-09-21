#!/usr/bin/env bash
# Kiểm chứng hạ tầng WebSocket, chấm công và nghỉ phép — Phase 5 (một phần)
# và Phase 3.
#
# Chạy: ADMIN_PASS='...' bash scripts/smoke-attendance.sh
#
# Script tạo dữ liệu thật rồi dọn sạch ở cuối, nên chạy lại bao nhiêu lần
# cũng được.

set -uo pipefail
cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
set -a; [ -f .env ] && . ./.env; set +a

BASE="${PUBLIC_BASE_URL:-http://localhost}/api/v1"

# Gọi đăng nhập THẲNG vào api, bỏ qua nginx — xem ghi chú trong smoke-hr.sh.
DIRECT="http://localhost:${API_PORT:-8080}/api/v1"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@abc.vn}"
ADMIN_PASS="${ADMIN_PASS:?Đặt biến ADMIN_PASS trước khi chạy}"

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
idof() { grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4; }
field() { grep -o "\"$1\":\"[^\"]*\"" | head -1 | cut -d'"' -f4; }
num() { grep -o "\"$1\":-\?[0-9.]*" | head -1 | cut -d':' -f2; }

MAILHOG="http://localhost:${MAILHOG_UI_PORT:-8025}"

# Cộng ngày, tương thích cả GNU date lẫn BSD date.
add_days() { # add_days <YYYY-MM-DD|now> <n>
  if [ "$1" = "now" ]; then
    date -d "+$2 days" +%F 2>/dev/null || date -v+"$2"d +%F
  else
    date -d "$1 +$2 days" +%F 2>/dev/null || date -j -v+"$2"d -f %F "$1" +%F
  fi
}

dow() { date -d "$1" +%u 2>/dev/null || date -j -f %F "$1" +%u; }

# Đẩy tới ngày làm việc gần nhất (thứ hai tới thứ sáu).
#
# Cần thiết vì hệ thống TỪ CHỐI đơn nghỉ phép rơi trọn vào cuối tuần — đó là
# hành vi đúng (không ai tiêu ngày phép vào ngày vốn đã nghỉ), nhưng nó khiến
# phép thử chọn ngày cứng bị hỏng tuỳ theo hôm nay là thứ mấy.
next_weekday() {
  local d="$1"
  while [ "$(dow "$d")" -gt 5 ]; do d=$(add_days "$d" 1); done
  echo "$d"
}

login_otp() { # login_otp <email> <password>
  local email="$1" pass="$2" res ch otp i body

  curl -s -X DELETE "$MAILHOG/api/v1/messages" >/dev/null

  res=$(curl -s -X POST "$DIRECT/auth/login" -H "$JSON" \
    -d "{\"email\":\"$email\",\"password\":\"$pass\"}")

  if echo "$res" | grep -q '"access_token"'; then
    echo "$res" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4
    return 0
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
    -d "{\"challenge_id\":\"$ch\",\"code\":\"$otp\"}" \
    | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4
}

# Xoá bộ đếm chặn dò mật khẩu — xem ghi chú trong smoke-hr.sh.
docker compose exec -T redis sh -c \
  "redis-cli -a '$REDIS_PASSWORD' --no-auth-warning --scan --pattern 'login_fail:*' \
   | xargs -r redis-cli -a '$REDIS_PASSWORD' --no-auth-warning DEL" >/dev/null 2>&1

TOKEN=$(login_otp "$ADMIN_EMAIL" "$ADMIN_PASS")
if [ -z "$TOKEN" ]; then
  echo "Không đăng nhập được bằng $ADMIN_EMAIL — dừng." >&2
  exit 1
fi
AUTH="Authorization: Bearer $TOKEN"

ME_ID=$(curl -s "$BASE/auth/me" -H "$AUTH" | grep -o '"employee_id":"[^"]*"' | cut -d'"' -f4)

TODAY=$(date +%F)
YEAR=$(date +%Y)

echo
echo "═══ KIỂM CHỨNG CHẤM CÔNG & NGHỈ PHÉP ═══"
echo

# ================================================= hạ tầng WebSocket
echo "── Hạ tầng WebSocket ──"

# Bắt tay WebSocket dùng token trên QUERY STRING: trình duyệt không cho đặt
# header Authorization khi mở WebSocket.
check "Không token → 401" 401 \
  "$(code --http1.1 -H 'Connection: Upgrade' -H 'Upgrade: websocket' \
     -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: AAAAAAAAAAAAAAAAAAAAAA==' \
     "http://localhost:${API_PORT:-8080}/ws")"

check "Token rác → 401" 401 \
  "$(code --http1.1 -H 'Connection: Upgrade' -H 'Upgrade: websocket' \
     -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: AAAAAAAAAAAAAAAAAAAAAA==' \
     "http://localhost:${API_PORT:-8080}/ws?token=rac")"

# Token hợp lệ: server trả 101 Switching Protocols.
check "Token hợp lệ → 101 nâng cấp giao thức" 101 \
  "$(code --http1.1 -H 'Connection: Upgrade' -H 'Upgrade: websocket' \
     -H 'Sec-WebSocket-Version: 13' -H 'Sec-WebSocket-Key: AAAAAAAAAAAAAAAAAAAAAA==' \
     "http://localhost:${API_PORT:-8080}/ws?token=$TOKEN")"

# ============================================== khung giờ làm việc
echo
echo "── Khung giờ làm việc ──"

SCHEDULES=$(curl -s "$BASE/work-schedules" -H "$AUTH")
check "Có khung giờ mặc định của công ty" "company" "$(echo "$SCHEDULES" | field scope)"

DEFAULT_ID=$(echo "$SCHEDULES" | idof)
check "Không xoá được khung giờ công ty → 409" 409 \
  "$(code -X DELETE "$BASE/work-schedules/$DEFAULT_ID" -H "$AUTH")"

check "Giờ kết thúc trước giờ bắt đầu → 400" 400 \
  "$(code -X POST "$BASE/work-schedules" -H "$AUTH" -H "$JSON" \
     -d '{"name":"Sai gio","work_start":"18:00","work_end":"09:00"}')"

check "Ngày làm việc ngoài 1..7 → 400" 400 \
  "$(code -X POST "$BASE/work-schedules" -H "$AUTH" -H "$JSON" \
     -d '{"name":"Sai ngay","work_start":"08:00","work_end":"17:00","workdays":[1,9]}')"

SCHED_ID=$(curl -s -X POST "$BASE/work-schedules" -H "$AUTH" -H "$JSON" \
  -d "{\"name\":\"Ca kiem thu\",\"work_start\":\"09:00\",\"work_end\":\"18:00\",\"employee_id\":\"$ME_ID\",\"grace_minutes\":15}" | idof)
check "Tạo khung giờ cho cá nhân" "yes" "$([ -n "$SCHED_ID" ] && echo yes || echo no)"

# ==================================================== ngày lễ
echo
echo "── Ngày lễ ──"

HOL_ID=$(curl -s -X POST "$BASE/holidays" -H "$AUTH" -H "$JSON" \
  -d "{\"date\":\"$YEAR-12-25\",\"name\":\"Ngay kiem thu\"}" | idof)
check "Tạo ngày lễ" "yes" "$([ -n "$HOL_ID" ] && echo yes || echo no)"

check "Trùng ngày lễ → cập nhật tên (201)" 201 \
  "$(code -X POST "$BASE/holidays" -H "$AUTH" -H "$JSON" \
     -d "{\"date\":\"$YEAR-12-25\",\"name\":\"Ngay kiem thu sua\"}")"

check "Ngày lễ sai định dạng → 400" 400 \
  "$(code -X POST "$BASE/holidays" -H "$AUTH" -H "$JSON" \
     -d '{"date":"25-12","name":"X"}')"

# ================================================= chấm công
echo
echo "── Chấm công ──"

check "Bảng công hôm nay → 200" 200 "$(code "$BASE/attendance/today" -H "$AUTH")"
check "Danh sách ngày công → 200" 200 "$(code "$BASE/attendance/days" -H "$AUTH")"
check "Chi tiết một ngày → 200" 200 "$(code "$BASE/attendance/day?date=$TODAY" -H "$AUTH")"
check "Tổng hợp tháng → 200" 200 "$(code "$BASE/attendance/summary" -H "$AUTH")"
check "Ai đang online → 200" 200 "$(code "$BASE/attendance/team" -H "$AUTH")"

check "Khoảng ngày quá dài → 400" 400 \
  "$(code "$BASE/attendance/days?from=2000-01-01&to=$TODAY" -H "$AUTH")"

check "Tháng không hợp lệ → 400" 400 \
  "$(code "$BASE/attendance/summary?year=$YEAR&month=13" -H "$AUTH")"

# Check-in thủ công cho trường hợp mất mạng / họp ngoài.
NOW_ISO=$(date -u +%FT%TZ)
AGO_ISO=$(date -u -d '2 hours ago' +%FT%TZ 2>/dev/null || date -u -v-2H +%FT%TZ)

check "Check-in thủ công → 201" 201 \
  "$(code -X POST "$BASE/attendance/check-in" -H "$AUTH" -H "$JSON" \
     -d "{\"started_at\":\"$AGO_ISO\",\"ended_at\":\"$NOW_ISO\",\"note\":\"hop ngoai\"}")"

check "Check-in giờ kết thúc trước giờ bắt đầu → 400" 400 \
  "$(code -X POST "$BASE/attendance/check-in" -H "$AUTH" -H "$JSON" \
     -d "{\"started_at\":\"$NOW_ISO\",\"ended_at\":\"$AGO_ISO\"}")"

FUTURE_ISO=$(date -u -d '2 days' +%FT%TZ 2>/dev/null || date -u -v+2d +%FT%TZ)
FUTURE_END=$(date -u -d '2 days 1 hour' +%FT%TZ 2>/dev/null || date -u -v+2d -v+1H +%FT%TZ)
check "Check-in cho tương lai → 400" 400 \
  "$(code -X POST "$BASE/attendance/check-in" -H "$AUTH" -H "$JSON" \
     -d "{\"started_at\":\"$FUTURE_ISO\",\"ended_at\":\"$FUTURE_END\"}")"

# Check-in đã kích hoạt tổng hợp lại, nên bảng công hôm nay phải có dữ liệu.
DAY=$(curl -s "$BASE/attendance/day?date=$TODAY" -H "$AUTH")
check "Check-in làm tăng số phút online" "yes" \
  "$([ "$(echo "$DAY" | num online_minutes)" -gt 0 ] && echo yes || echo no)"
check "Phiên thủ công được đánh dấu source=manual" "yes" \
  "$(echo "$DAY" | grep -q '"source":"manual"' && echo yes || echo no)"

# ======================================= yêu cầu điều chỉnh công
echo
echo "── Điều chỉnh công ──"

check "Điều chỉnh thiếu lý do → 400" 400 \
  "$(code -X POST "$BASE/attendance/adjustments" -H "$AUTH" -H "$JSON" \
     -d "{\"started_at\":\"$AGO_ISO\",\"ended_at\":\"$NOW_ISO\",\"reason\":\"  \"}")"

ADJ_ID=$(curl -s -X POST "$BASE/attendance/adjustments" -H "$AUTH" -H "$JSON" \
  -d "{\"started_at\":\"$AGO_ISO\",\"ended_at\":\"$NOW_ISO\",\"reason\":\"mat mang\"}" | idof)
check "Tạo yêu cầu điều chỉnh" "yes" "$([ -n "$ADJ_ID" ] && echo yes || echo no)"

# Không ai tự duyệt yêu cầu của chính mình — nguyên tắc kiểm soát nội bộ.
check "Tự duyệt yêu cầu của mình → 403" 403 \
  "$(code -X PUT "$BASE/attendance/adjustments/$ADJ_ID/decision" -H "$AUTH" -H "$JSON" \
     -d '{"approve":true}')"

check "Danh sách điều chỉnh → 200" 200 \
  "$(code "$BASE/attendance/adjustments?status=pending" -H "$AUTH")"

# ==================================================== nghỉ phép
echo
echo "── Nghỉ phép ──"

check "Đặt quỹ phép → 200" 200 \
  "$(code -X PUT "$BASE/leaves/balances" -H "$AUTH" -H "$JSON" \
     -d "{\"employee_id\":\"$ME_ID\",\"year\":$YEAR,\"entitled_days\":12,\"carried_over_days\":3}")"

BAL=$(curl -s "$BASE/leaves/balance?year=$YEAR" -H "$AUTH")
check "Quỹ phép còn lại = 12 + 3" 15 "$(echo "$BAL" | num remaining_days)"

check "Quỹ phép âm → 400" 400 \
  "$(code -X PUT "$BASE/leaves/balances" -H "$AUTH" -H "$JSON" \
     -d "{\"employee_id\":\"$ME_ID\",\"year\":$YEAR,\"entitled_days\":-5}")"

# Chọn một khoảng ngày trong tương lai để không đụng dữ liệu hôm nay.
L_START=$(next_weekday "$(add_days now 40)")
L_END=$(next_weekday "$(add_days "$L_START" 1)")

LEAVE=$(curl -s -X POST "$BASE/leaves" -H "$AUTH" -H "$JSON" \
  -d "{\"leave_type\":\"annual\",\"start_date\":\"$L_START\",\"end_date\":\"$L_END\",\"reason\":\"kiem thu\"}")
LEAVE_ID=$(echo "$LEAVE" | idof)
check "Tạo đơn nghỉ phép" "yes" "$([ -n "$LEAVE_ID" ] && echo yes || echo no)"

check "Đơn trùng khoảng ngày → 409" 409 \
  "$(code -X POST "$BASE/leaves" -H "$AUTH" -H "$JSON" \
     -d "{\"leave_type\":\"annual\",\"start_date\":\"$L_START\",\"end_date\":\"$L_END\"}")"

check "Ngày kết thúc trước ngày bắt đầu → 400" 400 \
  "$(code -X POST "$BASE/leaves" -H "$AUTH" -H "$JSON" \
     -d "{\"leave_type\":\"annual\",\"start_date\":\"$L_END\",\"end_date\":\"$L_START\"}")"

# Nghỉ trọn cuối tuần bị từ chối: không ai tiêu ngày phép vào ngày vốn đã
# nghỉ. Tìm thứ bảy gần nhất để thử.
SAT=$(add_days now 30)
while [ "$(dow "$SAT")" -ne 6 ]; do SAT=$(add_days "$SAT" 1); done
SUN=$(add_days "$SAT" 1)
check "Đơn nghỉ rơi trọn vào cuối tuần → 400" 400 \
  "$(code -X POST "$BASE/leaves" -H "$AUTH" -H "$JSON" \
     -d "{\"leave_type\":\"annual\",\"start_date\":\"$SAT\",\"end_date\":\"$SUN\"}")"

check "Loại nghỉ phép không hợp lệ → 400" 400 \
  "$(code -X POST "$BASE/leaves" -H "$AUTH" -H "$JSON" \
     -d "{\"leave_type\":\"khong_ton_tai\",\"start_date\":\"$L_START\",\"end_date\":\"$L_START\"}")"

# Nửa ngày chỉ áp dụng cho đơn gói gọn trong một ngày.
HALF_START=$(next_weekday "$(add_days now 60)")
HALF_END=$(add_days "$HALF_START" 1)
check "Nghỉ nửa ngày kéo dài 2 ngày → 400" 400 \
  "$(code -X POST "$BASE/leaves" -H "$AUTH" -H "$JSON" \
     -d "{\"leave_type\":\"annual\",\"start_date\":\"$HALF_START\",\"end_date\":\"$HALF_END\",\"day_part\":\"morning\"}")"

# Không ai tự duyệt đơn của chính mình, kể cả admin.
check "Tự duyệt đơn của mình → 403" 403 \
  "$(code -X PUT "$BASE/leaves/$LEAVE_ID/decision" -H "$AUTH" -H "$JSON" \
     -d '{"approve":true}')"

check "Danh sách đơn nghỉ → 200" 200 "$(code "$BASE/leaves" -H "$AUTH")"

check "Huỷ đơn của mình → 200" 200 \
  "$(code -X DELETE "$BASE/leaves/$LEAVE_ID" -H "$AUTH")"

check "Huỷ xong thì quỹ phép không bị trừ" 15 \
  "$(curl -s "$BASE/leaves/balance?year=$YEAR" -H "$AUTH" | num remaining_days)"

# Đơn vượt quá quỹ phép bị chặn NGAY LÚC TẠO, không đợi tới lúc duyệt.
BIG_START=$(next_weekday "$(add_days now 90)")
BIG_END=$(add_days now 150)
check "Đơn vượt quỹ phép → 409" 409 \
  "$(code -X POST "$BASE/leaves" -H "$AUTH" -H "$JSON" \
     -d "{\"leave_type\":\"annual\",\"start_date\":\"$BIG_START\",\"end_date\":\"$BIG_END\"}")"

# Nghỉ không lương KHÔNG trừ quỹ phép nên không bị chặn.
UNPAID=$(curl -s -X POST "$BASE/leaves" -H "$AUTH" -H "$JSON" \
  -d "{\"leave_type\":\"unpaid\",\"start_date\":\"$BIG_START\",\"end_date\":\"$BIG_END\"}" | idof)
check "Nghỉ không lương dài ngày (không trừ quỹ)" "yes" "$([ -n "$UNPAID" ] && echo yes || echo no)"

# ================================================== khoá kỳ công
echo
echo "── Khoá kỳ công ──"

check "Khoá kỳ → 200" 200 \
  "$(code -X POST "$BASE/attendance/lock" -H "$AUTH" -H "$JSON" \
     -d "{\"from\":\"$TODAY\",\"to\":\"$TODAY\",\"locked\":true}")"

check "Ngày đã khoá hiện is_locked" "true" \
  "$(curl -s "$BASE/attendance/day?date=$TODAY" -H "$AUTH" | grep -o '"is_locked":[a-z]*' | head -1 | cut -d':' -f2)"

check "Check-in vào ngày đã khoá → 409" 409 \
  "$(code -X POST "$BASE/attendance/check-in" -H "$AUTH" -H "$JSON" \
     -d "{\"started_at\":\"$AGO_ISO\",\"ended_at\":\"$NOW_ISO\"}")"

check "Mở khoá kỳ → 200" 200 \
  "$(code -X POST "$BASE/attendance/lock" -H "$AUTH" -H "$JSON" \
     -d "{\"from\":\"$TODAY\",\"to\":\"$TODAY\",\"locked\":false}")"

# ==================================================== phân quyền
echo
echo "── Phân quyền ──"

check "Không token → 401" 401 "$(code "$BASE/attendance/today")"
check "Token rác → 401" 401 "$(code "$BASE/attendance/days" -H 'Authorization: Bearer rac')"

# --------------------------------------------------------------- dọn dẹp
echo
echo "── Dọn dữ liệu kiểm thử ──"

[ -n "$SCHED_ID" ] && curl -s -o /dev/null -X DELETE "$BASE/work-schedules/$SCHED_ID" -H "$AUTH"
[ -n "$HOL_ID" ]   && curl -s -o /dev/null -X DELETE "$BASE/holidays/$HOL_ID" -H "$AUTH"
# Huỷ đơn nghỉ đã tạo. Bắt buộc để chạy lại được: đơn đang chờ chiếm chỗ
# khoảng ngày đó, và lần chạy sau sẽ bị chặn trùng — đúng hành vi của hệ
# thống, nhưng làm hỏng phép thử.
[ -n "${UNPAID:-}" ] && curl -s -o /dev/null -X DELETE "$BASE/leaves/$UNPAID" -H "$AUTH"

echo "  đã xoá khung giờ, ngày lễ và huỷ đơn nghỉ vừa tạo"
echo "  (phiên làm việc giữ lại để kiểm tra bằng mắt nếu cần)"

echo
echo "═══════════════════════════════════════"
printf '  Đạt: \033[32m%d\033[0m   Hỏng: \033[31m%d\033[0m\n' "$PASS" "$FAIL"
echo "═══════════════════════════════════════"
echo

[ "$FAIL" -eq 0 ]
