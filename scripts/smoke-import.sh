#!/usr/bin/env bash
# Kiểm chứng nhập nhân viên hàng loạt từ CSV/Excel.
#
# Chạy: ADMIN_PASS='...' bash scripts/smoke-import.sh
#
# Đi hết đường thật: tải tệp lên api → RabbitMQ → worker tạo nhân viên và
# tài khoản → mail chào mừng qua MailHog → thông báo cho người nhập. Script
# tạo dữ liệu thật rồi dọn ở cuối, nên chạy lại bao nhiêu lần cũng được.

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

upload() { # upload <file> [create_accounts] → in ra body, mã HTTP ở dòng cuối
  curl -s -w '\n%{http_code}' -X POST "$BASE/employees/imports" -H "$AUTH" \
    -F "file=@$1" -F "create_accounts=${2:-false}"
}

echo
echo "═══ KIỂM CHỨNG NHẬP NHÂN VIÊN HÀNG LOẠT ═══"
echo

# --------------------------------------------------------------- tệp mẫu
echo "── Mô tả tệp mẫu ──"
LIST=$(curl -s "$BASE/employees/imports" -H "$AUTH")
check "Danh sách lượt nhập có meta.columns" "yes" \
  "$(echo "$LIST" | grep -q '"columns":\["ma_nhan_vien"' && echo yes || echo no)"

# ------------------------------------------------------- lỗi của cả tệp
echo "── Tệp hỏng bị từ chối ngay, không vào hàng đợi ──"
printf 'x' > "$TMP/ds.xls"
check "Đuôi .xls (định dạng cũ) → 400" 400 "$(upload "$TMP/ds.xls" | tail -1)"

printf 'ma_nhan_vien,ho_ten\nNV1,A\n' > "$TMP/thieu.csv"
RES=$(upload "$TMP/thieu.csv")
check "Thiếu cột bắt buộc → 400" 400 "$(echo "$RES" | tail -1)"
check "…và nêu tên cột thiếu" "yes" \
  "$(echo "$RES" | grep -q 'ngay_vao_lam' && echo yes || echo no)"

# "Nguyễn" trong bảng mã Windows — thứ Excel lưu khi chọn "CSV" thường.
printf 'ma_nhan_vien,ho_ten,email,ngay_vao_lam\nNV1,Nguy\xd2n,a@x.vn,2024-01-01\n' > "$TMP/ansi.csv"
RES=$(upload "$TMP/ansi.csv")
check "CSV không phải UTF-8 → 400" 400 "$(echo "$RES" | tail -1)"
check "…và chỉ cách lưu lại" "yes" \
  "$(echo "$RES" | grep -q 'CSV UTF-8' && echo yes || echo no)"

# ------------------------------------------------------------ nhập thật
echo "── Nhập thật: BOM, chấm phẩy, tiêu đề có dấu, kèm tạo tài khoản ──"
TAG=$(date +%s | tail -c 6)
LEAD="IMP${TAG}A"
STAFF="IMP${TAG}B"
BAD="IMP${TAG}C"
# Đúng thứ Excel vùng Việt Nam lưu khi chọn "CSV UTF-8". Ghi bằng printf để
# giữ nguyên UTF-8 — gõ tiếng Việt thẳng trên dòng lệnh Git Bash dễ hỏng mã.
{
  printf '\xef\xbb\xbf'
  printf 'STT;Mã nhân viên;Họ và tên;Email;Ngày vào làm;Mã cấp trên;Hình thức làm việc\n'
  printf '1;%s;Trưởng Nhóm Nhập;%s@smoke.vn;15/01/2024;;Tại văn phòng\n' "$LEAD" "$(echo "$LEAD" | tr 'A-Z' 'a-z')"
  printf '2;%s;Nhân Viên Nhập;%s@smoke.vn;2024-02-01;%s;remote\n' "$STAFF" "$(echo "$STAFF" | tr 'A-Z' 'a-z')" "$LEAD"
  printf '3;%s;Ngày Sai;%s@smoke.vn;31/02/2024;;\n' "$BAD" "$(echo "$BAD" | tr 'A-Z' 'a-z')"
} > "$TMP/ds.csv"

curl -s -X DELETE "$MAILHOG/api/v1/messages" >/dev/null
RES=$(upload "$TMP/ds.csv" true)
check "Tải lên → 202 (đã nhận, đang xử lý)" 202 "$(echo "$RES" | tail -1)"
IMP_ID=$(echo "$RES" | head -n -1 | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

STATUS=""
for i in $(seq 1 40); do
  DETAIL=$(curl -s "$BASE/employees/imports/$IMP_ID" -H "$AUTH")
  STATUS=$(echo "$DETAIL" | field status)
  [ "$STATUS" = "done" ] && break
  sleep 0.5
done
check "Worker chạy xong" "done" "$STATUS"
check "2 dòng thành công" 2 "$(echo "$DETAIL" | field succeeded)"
check "1 dòng lỗi" 1 "$(echo "$DETAIL" | field failed)"
check "Dòng lỗi là dòng 4 của bảng tính, lỗi ngày vào làm" "yes" \
  "$(echo "$DETAIL" | grep -q '"line":4,[^}]*"error":"ngày vào làm' && echo yes || echo no)"
check "Tạo luôn tài khoản" "yes" \
  "$(echo "$DETAIL" | grep -q '"account_created":true' && echo yes || echo no)"

# Nhân viên thật sự có trong hệ thống, và cấp trên được gắn đúng.
EMP=$(curl -s "$BASE/employees?search=$STAFF" -H "$AUTH")
check "Nhân viên dòng 3 tìm thấy qua API danh sách" "yes" \
  "$(echo "$EMP" | grep -q "\"employee_code\":\"$STAFF\"" && echo yes || echo no)"
check "…có cấp trên là người ở dòng phía trên" "yes" \
  "$(echo "$EMP" | grep -q '"manager_name":"Trưởng Nhóm Nhập"' && echo yes || echo no)"

# Mail chào mừng đi qua worker → SMTP.
MAILS=0
for i in $(seq 1 20); do
  MAILS=$(curl -s "$MAILHOG/api/v2/search?kind=containing&query=smoke.vn" | field total)
  [ "${MAILS:-0}" -ge 2 ] && break
  sleep 0.5
done
check "Hai mail chào mừng kèm mật khẩu tạm" 2 "${MAILS:-0}"

# Thông báo cho người nhập, trỏ về trang kết quả.
NOTI=$(curl -s "$BASE/notifications?page_size=5" -H "$AUTH")
check "Người nhập nhận thông báo có link tới kết quả" "yes" \
  "$(echo "$NOTI" | grep -q "\"link\":\"/employees/imports/$IMP_ID\"" && echo yes || echo no)"

check "Lượt nhập không tồn tại → 404" 404 \
  "$(code "$BASE/employees/imports/00000000-0000-0000-0000-000000000000" -H "$AUTH")"

# --------------------------------------------------------------------- dọn dẹp
echo
echo "── Dọn dữ liệu kiểm thử ──"
for id in $(echo "$DETAIL" | grep -o '"employee_id":"[^"]*"' | cut -d'"' -f4); do
  curl -s -o /dev/null -X DELETE "$BASE/employees/$id" -H "$AUTH"
done
docker compose exec -T postgres psql -U "${POSTGRES_USER:-manage}" -d "${POSTGRES_DB:-manage}" -q \
  -c "DELETE FROM employee_imports WHERE id = '$IMP_ID'" >/dev/null
echo "  đã xoá nhân viên và lượt nhập vừa tạo"

echo
echo "═══════════════════════════════════════"
printf '  Đạt: \033[32m%d\033[0m   Hỏng: \033[31m%d\033[0m\n' "$PASS" "$FAIL"
echo "═══════════════════════════════════════"
echo

[ "$FAIL" -eq 0 ]
