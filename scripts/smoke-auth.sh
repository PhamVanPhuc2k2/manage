#!/usr/bin/env bash
# Kiểm chứng bảo mật Phase 1.
#
# Chạy: bash scripts/smoke-auth.sh
#
# Script này chạy TẤT CẢ các mục trong bảng kiểm chứng của PHASE-1-SETUP.md.
# Mỗi mục in ra kết quả mong đợi và kết quả thực tế, không tự đánh giá thay
# người đọc — để bạn tự nhìn thấy con số.

set -uo pipefail
cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
set -a; [ -f .env ] && . ./.env; set +a

BASE="${PUBLIC_BASE_URL:-http://localhost}/api/v1"

# Một số phép thử phải gọi THẲNG vào api, bỏ qua nginx.
#
# Lý do: nginx cũng có giới hạn tốc độ riêng cho /auth/login. Nếu đi qua nó,
# ta sẽ đo nhầm giới hạn của nginx thay vì giới hạn theo tài khoản của ứng
# dụng — hai cơ chế khác nhau, phục vụ hai mục đích khác nhau.
DIRECT="http://localhost:${API_PORT:-8080}/api/v1"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@abc.vn}"
ADMIN_PASS="${ADMIN_PASS:?Đặt biến ADMIN_PASS trước khi chạy}"

PASS=0
FAIL=0

check() { # check "mô tả" "mong đợi" "thực tế"
  if [ "$2" = "$3" ]; then
    printf '  \033[32m✓\033[0m %-56s %s\n' "$1" "$3"
    PASS=$((PASS + 1))
  else
    printf '  \033[31m✗\033[0m %-56s mong %s, được %s\n' "$1" "$2" "$3"
    FAIL=$((FAIL + 1))
  fi
}

code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
json() { curl -s "$@"; }

MAILHOG="http://localhost:${MAILHOG_UI_PORT:-8025}"

# Đọc mã OTP mới nhất trong MailHog.
#
# Cố ý đi đường vòng qua hộp thư thay vì đọc thẳng Redis: mã phải đi hết
# api → RabbitMQ → worker → SMTP mới tới được đây, và đó đúng là đoạn hay
# hỏng nhất. Đọc Redis thì kiểm chứng được mỗi phép so chuỗi.
otp_code() {
  local i body
  for i in $(seq 1 40); do
    body=$(json "$MAILHOG/api/v2/messages?limit=1" | awk -F'"Body":"' '{print $2}')
    # Trong thân thư chỉ có đúng một dãy 6 chữ số: chính là mã.
    # ("5 phút" một chữ số, địa chỉ IP nhiều nhất ba chữ số một nhóm.)
    case "$body" in
      *[0-9][0-9][0-9][0-9][0-9][0-9]*)
        echo "$body" | grep -o '[0-9]\{6\}' | head -1
        return 0
        ;;
    esac
    sleep 0.25
  done
  return 1
}

# Đăng nhập trọn vẹn, in ra access token.
#
# Chạy được với cả hai cấu hình: AUTH_OTP_ENABLED=true thì đi hai bước,
# false thì bước một đã trả token. Script không cần biết máy chủ đang bật
# hay tắt — nó hỏi phản hồi.
login_otp() { # login_otp <email> <password> [cookie_jar]
  local email="$1" pass="$2" jar="${3:-/dev/null}" res tok ch

  # Dọn hộp thư để không nhặt nhầm mã của lần đăng nhập trước.
  curl -s -X DELETE "$MAILHOG/api/v1/messages" >/dev/null

  res=$(json -c "$jar" -X POST "$DIRECT/auth/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$email\",\"password\":\"$pass\"}")

  tok=$(echo "$res" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
  if [ -n "$tok" ]; then echo "$tok"; return 0; fi

  ch=$(echo "$res" | grep -o '"challenge_id":"[^"]*"' | cut -d'"' -f4)
  [ -n "$ch" ] || return 1

  local otp
  otp=$(otp_code) || return 1

  json -c "$jar" -X POST "$DIRECT/auth/verify-otp" -H 'Content-Type: application/json' \
    -d "{\"challenge_id\":\"$ch\",\"code\":\"$otp\"}" \
    | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4
}


# Xoá bộ đếm chặn đăng nhập để chạy lại script không bị khoá.
reset_throttle() {
  docker compose exec -T redis sh -c \
    "redis-cli -a '$REDIS_PASSWORD' --no-auth-warning --scan --pattern 'login_fail:*' | xargs -r redis-cli -a '$REDIS_PASSWORD' --no-auth-warning DEL" \
    >/dev/null 2>&1
}

echo
echo "═══ KIỂM CHỨNG BẢO MẬT PHASE 1 ═══"
echo

reset_throttle

# ---------------------------------------------------------------- xác thực
echo "── Xác thực ──"
check "Không token → 401" 401 "$(code "$BASE/employees")"

TOKEN=$(login_otp "$ADMIN_EMAIL" "$ADMIN_PASS" /tmp/admin.cookie)
AUTH="Authorization: Bearer $TOKEN"
check "Đăng nhập admin → có token" "yes" "$([ -n "$TOKEN" ] && echo yes || echo no)"
check "Cookie refresh httpOnly được đặt" "yes" \
  "$(grep -q manage_refresh /tmp/admin.cookie && echo yes || echo no)"
check "/auth/me với token hợp lệ → 200" 200 "$(code "$BASE/auth/me" -H "$AUTH")"

# ------------------------------------------------------------ OTP đăng nhập
echo
echo "── Mã xác minh đăng nhập ──"

OTP_JAR=/tmp/otp-step1.cookie
STEP1=$(curl -s -X DELETE "$MAILHOG/api/v1/messages" >/dev/null; \
  json -c "$OTP_JAR" -X POST "$DIRECT/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASS\"}")

check "Mật khẩu đúng → đòi mã, chưa cấp token" "yes" \
  "$(echo "$STEP1" | grep -q '"otp_required":true' \
     && ! echo "$STEP1" | grep -q 'access_token' && echo yes || echo no)"

# Bước một KHÔNG được đặt cookie refresh. Đặt ở đây là cấp nửa phiên cho
# người mới qua mật khẩu — đúng thứ OTP sinh ra để chặn.
check "Bước một KHÔNG đặt cookie refresh" "yes" \
  "$(grep -q manage_refresh "$OTP_JAR" && echo no || echo yes)"

check "Email hiển thị bị che bớt" "yes" \
  "$(echo "$STEP1" | grep -q '"masked_email":"[^"]*\*' && echo yes || echo no)"

OTP_CH=$(echo "$STEP1" | grep -o '"challenge_id":"[^"]*"' | cut -d'"' -f4)
check "Sai mã → 401" 401 \
  "$(code -X POST "$DIRECT/auth/verify-otp" -H 'Content-Type: application/json' \
     -d "{\"challenge_id\":\"$OTP_CH\",\"code\":\"000000\"}")"
check "Sai mã → nói rõ còn mấy lần" "yes" \
  "$(json -X POST "$DIRECT/auth/verify-otp" -H 'Content-Type: application/json' \
     -d "{\"challenge_id\":\"$OTP_CH\",\"code\":\"000000\"}" \
     | grep -q 'lần thử' && echo yes || echo no)"

# Gửi lại ngay lập tức phải bị chặn, nếu không kẻ tấn công cứ bấm gửi lại là
# bộ đếm số lần thử không bao giờ chạm trần.
check "Gửi lại mã quá sớm → 429" 429 \
  "$(code -X POST "$DIRECT/auth/resend-otp" -H 'Content-Type: application/json' \
     -d "{\"challenge_id\":\"$OTP_CH\"}")"

# Đã sai 2 lần ở trên; thêm 3 lần nữa là chạm trần 5.
for _ in 1 2 3; do
  code -X POST "$DIRECT/auth/verify-otp" -H 'Content-Type: application/json' \
    -d "{\"challenge_id\":\"$OTP_CH\",\"code\":\"000000\"}" >/dev/null
done
check "Sai 5 lần → thử thách bị huỷ, mã đúng cũng vô dụng" 401 \
  "$(code -X POST "$DIRECT/auth/verify-otp" -H 'Content-Type: application/json' \
     -d "{\"challenge_id\":\"$OTP_CH\",\"code\":\"$(otp_code)\"}")"

check "Thử thách bịa → 401" 401 \
  "$(code -X POST "$DIRECT/auth/verify-otp" -H 'Content-Type: application/json' \
     -d '{"challenge_id":"khong-ton-tai","code":"123456"}')"

# ------------------------------------------------------------ giả mạo JWT
echo
echo "── Giả mạo token ──"
NONE_HDR=$(printf '{"alg":"none","typ":"JWT"}' | base64 -w0 | tr '+/' '-_' | tr -d '=')
NONE_PAY=$(printf '{"uid":"00000000-0000-0000-0000-000000000000","scope":"all"}' | base64 -w0 | tr '+/' '-_' | tr -d '=')
check "JWT alg=none → 401" 401 "$(code "$BASE/employees" -H "Authorization: Bearer ${NONE_HDR}.${NONE_PAY}.")"
check "JWT sai chữ ký → 401" 401 "$(code "$BASE/employees" -H "Authorization: Bearer ${TOKEN%.*}.AAAAfake")"

# ------------------------------------------------------------ vòng đời phiên
echo
echo "── Vòng đời phiên ──"
NEW=$(json -b /tmp/admin.cookie -c /tmp/admin2.cookie -X POST "$BASE/auth/refresh" \
  | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
check "Refresh → cấp token mới" "yes" "$([ -n "$NEW" ] && [ "$NEW" != "$TOKEN" ] && echo yes || echo no)"
check "Token mới dùng được → 200" 200 "$(code "$BASE/auth/me" -H "Authorization: Bearer $NEW")"

# --- Hai tab cùng F5 ---
#
# Đây là lỗi thật đã từng xảy ra: hai tab cùng gọi refresh với cùng một
# cookie, một tab thắng, tab kia bị coi là đánh cắp token và hệ thống huỷ
# sạch phiên — CẢ HAI tab đều bị đăng xuất. Thời gian ân hạn sửa việc này.
cp /tmp/admin.cookie /tmp/tabA.cookie
cp /tmp/admin.cookie /tmp/tabB.cookie
json -b /tmp/tabA.cookie -X POST "$BASE/auth/refresh" > /tmp/tabA.json &
json -b /tmp/tabB.cookie -X POST "$BASE/auth/refresh" > /tmp/tabB.json &
wait
TOK_A=$(grep -o '"access_token":"[^"]*"' /tmp/tabA.json | cut -d'"' -f4)
TOK_B=$(grep -o '"access_token":"[^"]*"' /tmp/tabB.json | cut -d'"' -f4)
check "Hai tab cùng F5: tab 1 vẫn dùng được" 200 \
  "$([ -n "$TOK_A" ] && code "$BASE/auth/me" -H "Authorization: Bearer $TOK_A" || echo "no-token")"
check "Hai tab cùng F5: tab 2 vẫn dùng được" 200 \
  "$([ -n "$TOK_B" ] && code "$BASE/auth/me" -H "Authorization: Bearer $TOK_B" || echo "no-token")"

# --- Hai tab F5 KHÔNG được đẻ thêm phiên ---
#
# Thời gian ân hạn ban đầu xử lý lần dùng lại bằng cách cấp hẳn một phiên
# mới. Người dùng không bị đá ra, nhưng mỗi chu kỳ refresh với N tab lại để
# lại N-1 phiên rác sống tới 7 ngày, và trang "thiết bị đang đăng nhập" đầy
# dòng trùng nhau. Nay lần dùng lại bám vào đúng phiên mà lần đầu đã tạo.
# Đếm theo "last_seen_at": mỗi phiên có đúng một trường này, còn "id" thì
# xuất hiện ở nhiều nơi khác trong cùng response.
count_sessions() {
  json "$BASE/auth/sessions" -H "Authorization: Bearer $1" \
    | grep -o '"last_seen_at"' | wc -l | tr -d ' '
}

GRACE_JAR=/tmp/grace.cookie
G_TOKEN=$(login_otp "$ADMIN_EMAIL" "$ADMIN_PASS" "$GRACE_JAR")
N_BEFORE=$(count_sessions "$G_TOKEN")

cp "$GRACE_JAR" /tmp/g1.cookie
cp "$GRACE_JAR" /tmp/g2.cookie
json -b /tmp/g1.cookie -X POST "$BASE/auth/refresh" > /tmp/g1.json &
json -b /tmp/g2.cookie -X POST "$BASE/auth/refresh" > /tmp/g2.json &
wait
G1=$(grep -o '"access_token":"[^"]*"' /tmp/g1.json | cut -d'"' -f4)
G2=$(grep -o '"access_token":"[^"]*"' /tmp/g2.json | cut -d'"' -f4)
N_AFTER=$(count_sessions "$G1")

check "Hai tab F5 vẫn chỉ một phiên (không đẻ thêm)" "$N_BEFORE" "$N_AFTER"
check "Hai tab F5: cả hai token cùng trỏ một phiên" 200 \
  "$([ -n "$G2" ] && code "$BASE/auth/me" -H "Authorization: Bearer $G2" || echo "no-token")"

# --- Đăng xuất phải thắng thời gian ân hạn ---
#
# Lỗ hổng cũ: đăng xuất xong, tab khác gửi lại refresh token cũ trong vòng
# 10 giây là được cấp một phiên mới hợp lệ. Phiên vừa cắt sống lại dưới id
# khác, và người dùng tưởng mình đã thoát.
LO_JAR=/tmp/logout-grace.cookie
login_otp "$ADMIN_EMAIL" "$ADMIN_PASS" "$LO_JAR" >/dev/null
cp "$LO_JAR" /tmp/logout-old.cookie
LO_TOKEN=$(json -b "$LO_JAR" -c "$LO_JAR" -X POST "$BASE/auth/refresh" \
  | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
curl -s -b "$LO_JAR" -X POST "$BASE/auth/logout" \
  -H "Authorization: Bearer $LO_TOKEN" >/dev/null
check "Đăng xuất rồi dùng lại token cũ trong ân hạn → 401" 401 \
  "$(code -b /tmp/logout-old.cookie -X POST "$BASE/auth/refresh")"

# --- Đánh cắp thật: dùng lại SAU thời gian ân hạn ---
#
# Phải chờ qua 10 giây ân hạn, nếu không sẽ đo nhầm sang nhánh "nhiều tab".
STOLEN=/tmp/stolen.cookie
TOKEN=$(login_otp "$ADMIN_EMAIL" "$ADMIN_PASS" "$STOLEN")
cp "$STOLEN" /tmp/thief.cookie
OWNER_TOKEN=$(json -b "$STOLEN" -c "$STOLEN" -X POST "$BASE/auth/refresh" \
  | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

echo "  · chờ qua 10 giây ân hạn..."
DEADLINE=$(( $(date +%s) + 13 ))
until [ "$(date +%s)" -ge "$DEADLINE" ]; do sleep 2; done

check "Dùng lại refresh token cũ sau ân hạn → 401" 401 \
  "$(code -b /tmp/thief.cookie -X POST "$BASE/auth/refresh")"
check "Chống đánh cắp: huỷ luôn token của chủ nhân → 401" 401 \
  "$(code "$BASE/auth/me" -H "Authorization: Bearer $OWNER_TOKEN")"

# Đăng nhập lại để có phiên sạch.
TOKEN=$(login_otp "$ADMIN_EMAIL" "$ADMIN_PASS" /tmp/admin.cookie)
AUTH="Authorization: Bearer $TOKEN"

BEFORE=$(code "$BASE/auth/me" -H "$AUTH")
curl -s -b /tmp/admin.cookie -X POST "$BASE/auth/logout" -H "$AUTH" >/dev/null
check "Trước đăng xuất → 200" 200 "$BEFORE"
check "Sau đăng xuất token chết NGAY → 401" 401 "$(code "$BASE/auth/me" -H "$AUTH")"

# ------------------------------------------------------- dò mật khẩu & email
echo
echo "── Chống dò ──"
reset_throttle
MSG_NOUSER=$(json -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d '{"email":"khongtontai@example.com","password":"x"}' | grep -o '"message":"[^"]*"')
MSG_BADPASS=$(json -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"saibet\"}" | grep -o '"message":"[^"]*"')
check "Email lạ và sai mật khẩu cùng thông báo" "same" \
  "$([ "$MSG_NOUSER" = "$MSG_BADPASS" ] && echo same || echo different)"

reset_throttle
LAST=""
for _ in 1 2 3 4 5 6 7; do
  LAST=$(code -X POST "$DIRECT/auth/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"sai\"}")
done
check "Sai mật khẩu nhiều lần → 429 (chặn ở tầng ứng dụng)" 429 "$LAST"
reset_throttle

# Đăng nhập lại sau khi xoá bộ đếm. Gọi thẳng api để không bị giới hạn của
# nginx tính gộp với loạt request vừa rồi.
TOKEN=$(login_otp "$ADMIN_EMAIL" "$ADMIN_PASS")
AUTH="Authorization: Bearer $TOKEN"

# ------------------------------------------------------------- phân trang
echo
echo "── Phân trang & tìm kiếm ──"
check "page_size=999999 bị cắt về 100" '"page_size":100' \
  "$(json "$BASE/employees?page_size=999999" -H "$AUTH" | grep -o '"page_size":[0-9]*')"

# Tìm theo tên của CHÍNH tài khoản quản trị ("Quản trị viên"), gõ không dấu.
#
# Trước đây phép thử này tìm "nguyen van anh" — một cái tên không có bộ smoke
# nào tạo ra. Nó đạt trên máy có sẵn dữ liệu demo và hỏng trên mọi cài đặt
# sạch, tức là nó đo môi trường chứ không đo sản phẩm.
#
# Tài khoản quản trị thì luôn có, và tên nó có ả, ị, ê — đủ để chứng minh
# unaccent đang làm việc.
FOUND=$(json "$BASE/employees?search=quan%20tri%20vien" -H "$AUTH" | grep -c 'full_name')
check "Tìm không dấu 'quan tri vien' có kết quả" "yes" \
  "$([ "$FOUND" -gt 0 ] && echo yes || echo no)"

# ------------------------------------------------------------ cây phòng ban
echo
echo "── Cây phòng ban ──"
DEPT_ID=$(json "$BASE/departments" -H "$AUTH" \
  | sed 's/},{/}\n{/g' | grep '"code":"KT"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)

if [ -n "$DEPT_ID" ]; then
  check "Đặt phòng làm cha của chính nó → 400" 400 \
    "$(code -X PUT "$BASE/departments/$DEPT_ID" -H "$AUTH" -H 'Content-Type: application/json' \
       -d "{\"code\":\"KT\",\"name\":\"Phòng Kỹ thuật\",\"parent_id\":\"$DEPT_ID\"}")"
else
  echo "  · bỏ qua (chưa có phòng KT)"
fi

check "Lấy cây phòng ban → 200" 200 "$(code "$BASE/departments/tree" -H "$AUTH")"

# -------------------------------------------------------------- quên mật khẩu
echo
echo "── Quên mật khẩu ──"
check "Email tồn tại → 200" 200 \
  "$(code -X POST "$BASE/auth/forgot-password" -H 'Content-Type: application/json' \
     -d "{\"email\":\"$ADMIN_EMAIL\"}")"
check "Email KHÔNG tồn tại cũng → 200 (không lộ danh sách)" 200 \
  "$(code -X POST "$BASE/auth/forgot-password" -H 'Content-Type: application/json' \
     -d '{"email":"khongcothat@example.com"}')"
check "Token đặt lại mật khẩu bịa → 400" 400 \
  "$(code -X POST "$BASE/auth/reset-password" -H 'Content-Type: application/json' \
     -d '{"token":"khong-ton-tai","new_password":"MatKhauMoi123"}')"

# ----------------------------------------------------------------- tổng kết
echo
echo "═══════════════════════════════════════"
printf '  Đạt: \033[32m%d\033[0m   Hỏng: \033[31m%d\033[0m\n' "$PASS" "$FAIL"
echo "═══════════════════════════════════════"
echo

[ "$FAIL" -eq 0 ]
