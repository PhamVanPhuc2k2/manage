#!/usr/bin/env bash
# Kiểm chứng gọi thoại / gọi video — Phase 7.
#
# Chạy: ADMIN_PASS='...' bash scripts/smoke-call.sh
#
# Script tạo dữ liệu thật rồi dọn sạch ở cuối, nên chạy lại bao nhiêu lần
# cũng được.
#
# KHÔNG kiểm chứng được ở đây: hình và tiếng có thật sự đi qua hay không.
# Việc đó cần hai trình duyệt thật với micro và camera — xem bộ Playwright
# và mục nghiệm thu Phase 7 trong doc/TASKS.md. Script này kiểm phần mà
# curl kiểm được: vòng đời, phân quyền, và chỗ token đến từ đâu.

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

MAILHOG="http://localhost:${MAILHOG_UI_PORT:-8025}"

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

# cleanup gọi ở CẢ HAI ĐẦU: đầu script để một lần chạy hỏng giữa chừng không
# chặn lần sau, cuối script để không để lại rác.
#
# Quan trọng hơn ở đây so với các bộ khác: chỉ mục một phần
# uq_calls_active_per_conversation chỉ cho MỘT cuộc gọi sống mỗi hội thoại,
# nên một cuộc gọi sót lại ở trạng thái 'ringing' sẽ làm mọi phép thử sau
# đó trả 409 mà không rõ vì sao.
cleanup() {
  docker compose exec -T postgres psql -U manage -d manage -v ON_ERROR_STOP=1 -c "
    DO \$\$
    DECLARE
      eid uuid;
    BEGIN
      SELECT id INTO eid FROM employees WHERE employee_code = 'CALL001';

      DELETE FROM call_participants WHERE call_id IN (
        SELECT c.id FROM calls c
        WHERE eid IS NOT NULL AND c.conversation_id IN (
          SELECT conversation_id FROM conversation_members WHERE employee_id = eid
        )
      );
      DELETE FROM calls WHERE eid IS NOT NULL AND conversation_id IN (
        SELECT conversation_id FROM conversation_members WHERE employee_id = eid
      );

      IF eid IS NOT NULL THEN
        DELETE FROM conversations
         WHERE direct_key LIKE '%' || eid::text || '%';
        DELETE FROM users     WHERE employee_id = eid;
        DELETE FROM employees WHERE id = eid;
      END IF;
    END \$\$;" >/dev/null
}

cleanup

# ============================================================ chuẩn bị
echo
echo "── Chuẩn bị: một nhân viên thứ hai và một hội thoại 1-1 ──"

DEPT_ID=$(curl -s "$BASE/departments" -H "$AUTH" | idof)
POS_ID=$(curl -s "$BASE/positions" -H "$AUTH" | idof)

cat > "$TMP/emp.json" <<EOF
{"employee_code":"CALL001","full_name":"Lê Thị Gọi Điện","email":"call001@test.local",
 "phone":"0909111222","department_id":"$DEPT_ID","position_id":"$POS_ID",
 "work_mode":"onsite","status":"official","joined_at":"2026-01-05"}
EOF

PEER_ID=$(curl -s -X POST "$BASE/employees" -H "$AUTH" -H "$JSON" \
  --data-binary "@$TMP/emp.json" | idof)
check "Tạo nhân viên thứ hai" "yes" "$([ -n "$PEER_ID" ] && echo yes || echo no)"

PEER_PW=$(curl -s -X POST "$BASE/employees/$PEER_ID/account" -H "$AUTH" \
  | grep -o '"temp_password":"[^"]*"' | cut -d'"' -f4)

curl -s -o /dev/null -X PUT "$BASE/employees/$PEER_ID/roles" -H "$AUTH" -H "$JSON" \
  -d '{"roles":["employee"]}'

PEER_TOKEN=$(login_otp "call001@test.local" "$PEER_PW")
if [ -n "$PEER_TOKEN" ]; then
  curl -s -o /dev/null -X POST "$DIRECT/auth/change-password" \
    -H "Authorization: Bearer $PEER_TOKEN" -H "$JSON" \
    -d "{\"old_password\":\"$PEER_PW\",\"new_password\":\"CallSmoke#2026\"}"
  PEER_TOKEN=$(login_otp "call001@test.local" "CallSmoke#2026")
fi
PEER_AUTH="Authorization: Bearer $PEER_TOKEN"
check "Người thứ hai đăng nhập được" 200 \
  "$([ -n "$PEER_TOKEN" ] && code "$DIRECT/auth/me" -H "$PEER_AUTH" || echo no-token)"

CONV_ID=$(curl -s -X POST "$BASE/chat/conversations" -H "$AUTH" -H "$JSON" \
  -d "{\"kind\":\"direct\",\"peer_id\":\"$PEER_ID\"}" | idof)
check "Mở hội thoại 1-1" "yes" "$([ -n "$CONV_ID" ] && echo yes || echo no)"

# ======================================================== hạ tầng media
echo
echo "── Hạ tầng media ──"

ICE=$(curl -s "$BASE/calls/ice-servers" -H "$AUTH")
check "GET /calls/ice-servers → 200" 200 \
  "$(code "$BASE/calls/ice-servers" -H "$AUTH")"
check "Có ít nhất một máy chủ ICE" "yes" \
  "$(echo "$ICE" | grep -q '"urls"' && echo yes || echo no)"

# Endpoint này cấp credential relay băng thông thật. Mở công khai là mời
# người lạ dùng chùa, và không thu hồi được ngoài đổi khoá cho tất cả.
check "Chưa đăng nhập → 401" 401 "$(code "$BASE/calls/ice-servers")"

# ======================================================== mở cuộc gọi
echo
echo "── Mở cuộc gọi ──"

START=$(curl -s -X POST "$BASE/calls" -H "$AUTH" -H "$JSON" \
  -d "{\"conversation_id\":\"$CONV_ID\",\"kind\":\"video\"}")
CALL_ID=$(echo "$START" | grep -o '"call":{"id":"[^"]*"' | cut -d'"' -f6)

check "POST /calls → 201" 201 \
  "$(code -X POST "$BASE/calls" -H "$AUTH" -H "$JSON" \
     -d "{\"conversation_id\":\"$CONV_ID\",\"kind\":\"video\"}")"
check "Trả về id cuộc gọi" "yes" "$([ -n "$CALL_ID" ] && echo yes || echo no)"
check "Trạng thái ban đầu là ringing" "ringing" "$(echo "$START" | field status)"

# Token và địa chỉ SFU trả luôn trong một lượt: mỗi lượt gọi thêm là thêm
# một khoảng lặng giữa lúc bấm nút và lúc thấy hình mình.
check "Trả kèm access token vào phòng" "yes" \
  "$(echo "$START" | grep -q '"token":"ey' && echo yes || echo no)"
check "Trả kèm địa chỉ SFU cho trình duyệt" "yes" \
  "$(echo "$START" | grep -q '"media_url":"ws' && echo yes || echo no)"

# Tên phòng theo call_id chứ không theo conversation_id: một hội thoại có
# nhiều cuộc gọi theo thời gian, dùng chung tên phòng là người của cuộc cũ
# rơi vào cuộc mới.
check "Tên phòng gắn với id cuộc gọi" "manage-call-$CALL_ID" \
  "$(echo "$START" | field room_name)"

# Phép thử quan trọng nhất của khối này: bấm gọi lần hai KHÔNG được tạo
# cuộc thứ hai. Hai người cùng bấm trong một giây mà ra hai phòng thì mỗi
# người vào một phòng và cả hai ngồi nhìn màn hình trống.
SECOND_ID=$(curl -s -X POST "$BASE/calls" -H "$AUTH" -H "$JSON" \
  -d "{\"conversation_id\":\"$CONV_ID\",\"kind\":\"video\"}" \
  | grep -o '"call":{"id":"[^"]*"' | cut -d'"' -f6)
check "Gọi lại → THAM GIA cuộc đang chạy, không tạo cuộc mới" "$CALL_ID" "$SECOND_ID"

LIVE_ID=$(curl -s "$BASE/calls/live/$CONV_ID" -H "$PEER_AUTH" | idof)
check "Người kia thấy cuộc gọi đang diễn ra" "$CALL_ID" "$LIVE_ID"

# ======================================================== phân quyền
echo
echo "── Phân quyền ──"

# 404 chứ KHÔNG phải 403: trả 403 là xác nhận cuộc gọi đó có thật, và đó
# là một rò rỉ — người ngoài biết được hai người kia vừa gọi nhau.
check "Người ngoài hội thoại xem cuộc gọi → 404" 404 \
  "$(code "$BASE/calls/00000000-0000-0000-0000-000000000000" -H "$AUTH")"
check "Chưa đăng nhập → 401" 401 "$(code "$BASE/calls/$CALL_ID")"
check "Gọi vào hội thoại không tồn tại → 404" 404 \
  "$(code -X POST "$BASE/calls" -H "$AUTH" -H "$JSON" \
     -d '{"conversation_id":"00000000-0000-0000-0000-000000000000","kind":"audio"}')"
check "kind sai → 400" 400 \
  "$(code -X POST "$BASE/calls" -H "$AUTH" -H "$JSON" \
     -d "{\"conversation_id\":\"$CONV_ID\",\"kind\":\"hologram\"}")"

# ======================================================== bắt máy
echo
echo "── Bắt máy ──"

ACCEPT=$(curl -s -X POST "$BASE/calls/$CALL_ID/accept" -H "$PEER_AUTH")
check "Người nhận bắt máy → có token riêng" "yes" \
  "$(echo "$ACCEPT" | grep -q '"token":"ey' && echo yes || echo no)"
check "Trạng thái chuyển sang active" "active" \
  "$(curl -s "$BASE/calls/$CALL_ID" -H "$AUTH" | field status)"

# Token phải hết hạn nhanh vì nó cho phép publish media vào phòng. Đường
# xin lại là thứ giữ cho cuộc họp dài hơn TTL không bị rớt giữa buổi.
check "Xin lại token giữa cuộc gọi → 200" 200 \
  "$(code -X POST "$BASE/calls/$CALL_ID/token" -H "$PEER_AUTH")"

# ======================================================== kết thúc
echo
echo "── Kết thúc ──"

# Mốc thời gian để soi nhật ký ở cuối khối này.
#
# Đặt mốc thay vì dùng một khoảng cố định kiểu "--since 2m": khoảng cố
# định sẽ vớt cả cảnh báo của LẦN CHẠY TRƯỚC, và phép thử báo hỏng
# trong khi mã đã được sửa xong.
#
# Chữ Z ở cuối là bắt buộc: thiếu nó, docker đọc mốc này theo giờ địa
# phương. Máy ở múi +7 thì cửa sổ rộng ra bảy tiếng — đã lừa một lần.
LOG_MARK=$(date -u +%Y-%m-%dT%H:%M:%SZ)

# Chỉ người khởi tạo kết thúc được cho tất cả. Người khác chỉ rời được —
# "tôi xong rồi" và "cuộc họp này xong rồi" là hai việc khác nhau.
check "Người không khởi tạo bấm kết thúc → 403" 403 \
  "$(code -X POST "$BASE/calls/$CALL_ID/end" -H "$PEER_AUTH")"
check "Người khởi tạo kết thúc → 200" 200 \
  "$(code -X POST "$BASE/calls/$CALL_ID/end" -H "$AUTH")"

ENDED=$(curl -s "$BASE/calls/$CALL_ID" -H "$AUTH")
check "Trạng thái cuối là ended" "ended" "$(echo "$ENDED" | field status)"
check "Có mốc kết thúc" "yes" \
  "$(echo "$ENDED" | grep -q '"ended_at"' && echo yes || echo no)"

# Kết thúc lần nữa KHÔNG phải lỗi: hai thiết bị của cùng một người cùng
# bấm cúp máy là chuyện bình thường.
check "Kết thúc lần nữa → vẫn 200" 200 \
  "$(code -X POST "$BASE/calls/$CALL_ID/end" -H "$AUTH")"

check "Hội thoại không còn cuộc gọi đang chạy" "" \
  "$(curl -s "$BASE/calls/live/$CONV_ID" -H "$AUTH" | idof)"

# Phép thử này soi NHẬT KÝ chứ không soi phản hồi HTTP, vì lỗi đóng phòng
# cố ý bị nuốt: cuộc gọi vẫn phải kết thúc đúng dù SFU có trục trặc.
#
# Đã hỏng thật một lần: token quản trị mang quyền RoomAdmin trong khi
# DeleteRoom của LiveKit cần RoomCreate. Mọi phép thử HTTP vẫn xanh, triệu
# chứng duy nhất là phòng rỗng nằm lại trên SFU cho tới lúc nó tự dọn.
check "Kết thúc không sinh cảnh báo nào từ SFU" "0" \
  "$(docker compose logs api --since "$LOG_MARK" 2>/dev/null \
     | grep -c 'không đóng được phòng trên SFU')"

# Chỉ mục một phần đã nhả ra: gọi lại được sau khi cuộc trước kết thúc.
# Đây là chỗ hỏng nguy hiểm nhất của module này — một cuộc gọi kẹt sẽ
# chặn vĩnh viễn mọi cuộc gọi sau trong cùng hội thoại.
NEW_ID=$(curl -s -X POST "$BASE/calls" -H "$AUTH" -H "$JSON" \
  -d "{\"conversation_id\":\"$CONV_ID\",\"kind\":\"audio\"}" | idof)
check "Gọi lại được sau khi cuộc trước đã xong" "yes" \
  "$([ -n "$NEW_ID" ] && [ "$NEW_ID" != "$CALL_ID" ] && echo yes || echo no)"

# ======================================================== từ chối
echo
echo "── Từ chối ──"

curl -s -o /dev/null -X POST "$BASE/calls/$NEW_ID/reject" -H "$PEER_AUTH"

# Gọi 1-1 mà đầu kia từ chối: người gọi VẪN đang trong phòng, nên câu hỏi
# "phòng có rỗng không" trả lời sai. Luật đúng là "đây còn là một cuộc gọi
# không" — dưới hai người và không ai đang đổ chuông thì không.
check "Đầu kia từ chối → cuộc gọi kết thúc ngay" "yes" \
  "$(s=$(curl -s "$BASE/calls/$NEW_ID" -H "$AUTH" | field status); \
     [ "$s" = "ended" ] || [ "$s" = "rejected" ] && echo yes || echo "$s")"
check "Hội thoại rảnh trở lại" "" \
  "$(curl -s "$BASE/calls/live/$CONV_ID" -H "$AUTH" | idof)"

# ======================================================== lịch sử
echo
echo "── Lịch sử và tin nhắn hệ thống ──"

HIST=$(curl -s "$BASE/calls/history/$CONV_ID" -H "$AUTH")
check "GET /calls/history → 200" 200 \
  "$(code "$BASE/calls/history/$CONV_ID" -H "$AUTH")"
check "Lịch sử có cuộc gọi vừa kết thúc" "yes" \
  "$(echo "$HIST" | grep -q "$CALL_ID" && echo yes || echo no)"

# Dòng "Cuộc gọi video · ..." phải nằm trong chính khung chat, không phải
# một dòng thời gian riêng: người dùng đọc lịch sử theo thứ tự thời gian,
# và tách ra hai nơi buộc họ ghép lại trong đầu.
sleep 1
MSGS=$(curl -s "$BASE/chat/conversations/$CONV_ID/messages" -H "$AUTH")
check "Có tin nhắn hệ thống về cuộc gọi trong khung chat" "yes" \
  "$(echo "$MSGS" | grep -q 'Cuộc gọi' && echo yes || echo no)"

# ======================================================== chỉ số vận hành
echo
echo "── Chỉ số vận hành ──"

# Đọc /metrics THẲNG trong container: nginx cố ý không để đường này lọt ra
# ngoài, và số cuộc gọi đang diễn ra không nên để ai cũng xem được.
#
# Ba chỉ số này trả lời câu hỏi quyết định chi phí băng thông — "mỗi ngày
# bao nhiêu cuộc gọi, dài bao lâu" — mà không phải quét bảng calls.
METRICS="$(docker compose exec -T api wget -q -O- http://localhost:8080/metrics 2>/dev/null)"

check "Có đếm cuộc gọi mở ra" "yes" \
  "$(echo "$METRICS" | grep -q '^manage_calls_started_total{kind="video"}' && echo yes || echo no)"
check "Có đếm cuộc gọi kết thúc theo lý do" "yes" \
  "$(echo "$METRICS" | grep -q '^manage_calls_ended_total{reason=' && echo yes || echo no)"
check "Có histogram thời lượng cuộc gọi" "yes" \
  "$(echo "$METRICS" | grep -q '^manage_call_duration_seconds_count' && echo yes || echo no)"

# --------------------------------------------------------------- dọn dẹp
echo
echo "── Dọn dữ liệu kiểm thử ──"

cleanup

echo "  đã xoá cuộc gọi, hội thoại và nhân viên vừa tạo"

echo
echo "═══════════════════════════════════════"
printf '  Đạt: \033[32m%d\033[0m   Hỏng: \033[31m%d\033[0m\n' "$PASS" "$FAIL"
echo "═══════════════════════════════════════"
echo

[ "$FAIL" -eq 0 ]
