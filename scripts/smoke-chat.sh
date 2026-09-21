#!/usr/bin/env bash
# Kiểm chứng thông báo và chat — Phase 5.
#
# Chạy: ADMIN_PASS='...' bash scripts/smoke-chat.sh
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
num() { grep -o "\"$1\":-\?[0-9]*" | head -1 | cut -d':' -f2; }

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

# cleanup xoá sạch mọi dấu vết của lần chạy trước.
#
# Gọi Ở CẢ HAI ĐẦU: đầu script để một lần chạy hỏng giữa chừng không chặn lần
# sau (mã nhân viên trùng là 409 và mọi phép thử phía sau đổ theo), cuối script
# để không để lại rác.
#
# Viết trong một khối DO vì thứ tự xoá phụ thuộc lẫn nhau: project_members và
# tasks tham chiếu employees với ON DELETE RESTRICT, nên phải dọn chúng trước.
# Xoá cứng ở đây chứ không gọi API vì API chỉ xoá mềm — bản ghi vẫn còn và vẫn
# giữ khoá ngoại.
cleanup() {
  docker compose exec -T postgres psql -U manage -d manage -v ON_ERROR_STOP=1 -c "
    DO \$\$
    DECLARE
      eid uuid;
      pid uuid;
    BEGIN
      SELECT id INTO eid FROM employees WHERE employee_code = 'CHAT001';
      SELECT id INTO pid FROM projects   WHERE code = 'CHATSMOKE';

      DELETE FROM conversations
       WHERE name IN ('Nhóm kiểm thử', 'Nhóm đã đổi tên')
          OR (pid IS NOT NULL AND project_id = pid)
          OR (eid IS NOT NULL AND direct_key LIKE '%' || eid::text || '%');

      IF pid IS NOT NULL THEN
        DELETE FROM task_timelogs    WHERE task_id IN (SELECT id FROM tasks WHERE project_id = pid);
        DELETE FROM task_comments    WHERE task_id IN (SELECT id FROM tasks WHERE project_id = pid);
        DELETE FROM task_attachments WHERE task_id IN (SELECT id FROM tasks WHERE project_id = pid);
        DELETE FROM tasks            WHERE project_id = pid;
        DELETE FROM project_members  WHERE project_id = pid;
        DELETE FROM projects         WHERE id = pid;
      END IF;

      DELETE FROM notifications;
      DELETE FROM notification_mutes;

      IF eid IS NOT NULL THEN
        DELETE FROM project_members WHERE employee_id = eid;
        DELETE FROM users           WHERE employee_id = eid;
        DELETE FROM employees       WHERE id = eid;
      END IF;
    END \$\$;" >/dev/null
}

cleanup

# ============================================================ chuẩn bị
echo
echo "── Chuẩn bị: một nhân viên thứ hai có tài khoản ──"

DEPT_ID=$(curl -s "$BASE/departments" -H "$AUTH" | idof)
POS_ID=$(curl -s "$BASE/positions" -H "$AUTH" | idof)

cat > "$TMP/emp.json" <<EOF
{"employee_code":"CHAT001","full_name":"Trần Văn Trò Chuyện","email":"chat001@test.local",
 "phone":"0909998887","department_id":"$DEPT_ID","position_id":"$POS_ID",
 "work_mode":"onsite","status":"official","joined_at":"2026-01-05"}
EOF

PEER_ID=$(curl -s -X POST "$BASE/employees" -H "$AUTH" -H "$JSON" \
  --data-binary "@$TMP/emp.json" | idof)
check "Tạo nhân viên thứ hai" "yes" "$([ -n "$PEER_ID" ] && echo yes || echo no)"

PEER_PW=$(curl -s -X POST "$BASE/employees/$PEER_ID/account" -H "$AUTH" \
  | grep -o '"temp_password":"[^"]*"' | cut -d'"' -f4)

# Cấp vai trò nhân viên để có quyền chat:read / chat:create.
# API nhận MÃ vai trò, không phải id — xem handler SetRoles.
curl -s -o /dev/null -X PUT "$BASE/employees/$PEER_ID/roles" -H "$AUTH" -H "$JSON" \
  -d '{"roles":["employee"]}'

# Người thứ hai phải đổi mật khẩu trước khi dùng được API khác.
PEER_TOKEN=$(login_otp "chat001@test.local" "$PEER_PW")
if [ -n "$PEER_TOKEN" ]; then
  curl -s -o /dev/null -X POST "$DIRECT/auth/change-password" \
    -H "Authorization: Bearer $PEER_TOKEN" -H "$JSON" \
    -d "{\"old_password\":\"$PEER_PW\",\"new_password\":\"ChatSmoke#2026\"}"
  PEER_TOKEN=$(login_otp "chat001@test.local" "ChatSmoke#2026")
fi
PEER_AUTH="Authorization: Bearer $PEER_TOKEN"
check "Người thứ hai đăng nhập được" 200 \
  "$([ -n "$PEER_TOKEN" ] && code "$DIRECT/auth/me" -H "$PEER_AUTH" || echo no-token)"

# ======================================================== hội thoại 1-1
echo
echo "── Hội thoại 1-1 ──"

D1=$(curl -s -X POST "$BASE/chat/conversations" -H "$AUTH" -H "$JSON" \
  -d "{\"kind\":\"direct\",\"peer_id\":\"$PEER_ID\"}")
D1_ID=$(echo "$D1" | idof)
check "Mở hội thoại 1-1 → có id" "yes" "$([ -n "$D1_ID" ] && echo yes || echo no)"
check "Tên hiển thị là tên người đối diện" "Trần Văn Trò Chuyện" \
  "$(echo "$D1" | field name)"

# Đây là phép thử quan trọng nhất của hội thoại 1-1: mở lại phải ra ĐÚNG
# hội thoại cũ, nếu không mỗi người sẽ thấy một nửa lịch sử.
D2_ID=$(curl -s -X POST "$BASE/chat/conversations" -H "$AUTH" -H "$JSON" \
  -d "{\"kind\":\"direct\",\"peer_id\":\"$PEER_ID\"}" | idof)
check "Mở lại 1-1 → TÁI SỬ DỤNG hội thoại cũ" "$D1_ID" "$D2_ID"

# Người kia bấm nhắn tin ngược lại cũng phải rơi vào cùng hội thoại đó.
D3_ID=$(curl -s -X POST "$BASE/chat/conversations" -H "$PEER_AUTH" -H "$JSON" \
  -d "{\"kind\":\"direct\",\"peer_id\":\"$ME_ID\"}" | idof)
check "Chiều ngược lại cũng ra cùng hội thoại" "$D1_ID" "$D3_ID"

check "Tự nhắn tin cho chính mình → 400" 400 \
  "$(code -X POST "$BASE/chat/conversations" -H "$AUTH" -H "$JSON" \
     -d "{\"kind\":\"direct\",\"peer_id\":\"$ME_ID\"}")"

check "Nhắn tin cho id không tồn tại → 404" 404 \
  "$(code -X POST "$BASE/chat/conversations" -H "$AUTH" -H "$JSON" \
     -d '{"kind":"direct","peer_id":"00000000-0000-0000-0000-000000000000"}')"

check "Tạo nhóm phòng ban qua API → 400" 400 \
  "$(code -X POST "$BASE/chat/conversations" -H "$AUTH" -H "$JSON" \
     -d '{"kind":"department","name":"Giả mạo"}')"

# ============================================================= tin nhắn
echo
echo "── Tin nhắn ──"

# Nội dung tiếng Việt đi qua TỆP, không qua tham số -d trên dòng lệnh.
#
# Git Bash trên Windows làm hỏng UTF-8 trong tham số dòng lệnh: chuỗi tới nơi
# thành ký tự rác, và phép thử tìm kiếm bỏ dấu sẽ hỏng vì lý do không liên
# quan gì tới thứ nó định kiểm tra.
CID1="smoke-$(date +%s)-1"
cat > "$TMP/m1.json" <<EOF
{"content":"Xin chào, đây là tin nhắn kiểm thử","client_message_id":"$CID1"}
EOF

M1=$(curl -s -X POST "$BASE/chat/conversations/$D1_ID/messages" -H "$AUTH" -H "$JSON" \
  --data-binary "@$TMP/m1.json")
M1_ID=$(echo "$M1" | idof)
check "Gửi tin nhắn → có id" "yes" "$([ -n "$M1_ID" ] && echo yes || echo no)"

# Chống trùng: gửi lại cùng client_message_id phải trả về ĐÚNG tin cũ.
M1_AGAIN=$(curl -s -X POST "$BASE/chat/conversations/$D1_ID/messages" -H "$AUTH" -H "$JSON" \
  --data-binary "@$TMP/m1.json" | idof)
check "Gửi lại cùng client_message_id → KHÔNG tạo tin mới" "$M1_ID" "$M1_AGAIN"

check "Tin rỗng → 400" 400 \
  "$(code -X POST "$BASE/chat/conversations/$D1_ID/messages" -H "$AUTH" -H "$JSON" \
     -d '{"content":"   "}')"

check "Client tự gửi tin hệ thống → 400" 400 \
  "$(code -X POST "$BASE/chat/conversations/$D1_ID/messages" -H "$AUTH" -H "$JSON" \
     -d '{"content":"X đã rời nhóm","kind":"system"}')"

# Tin quá dài. Dựng bằng printf để không phải dán 4001 ký tự vào script.
LONG=$(printf 'a%.0s' $(seq 1 4001))
check "Tin quá 4000 ký tự → 400" 400 \
  "$(code -X POST "$BASE/chat/conversations/$D1_ID/messages" -H "$AUTH" -H "$JSON" \
     -d "{\"content\":\"$LONG\"}")"

# Trả lời
cat > "$TMP/m2.json" <<EOF
{"content":"Đã nhận, cảm ơn","reply_to_id":"$M1_ID"}
EOF
M2=$(curl -s -X POST "$BASE/chat/conversations/$D1_ID/messages" -H "$PEER_AUTH" -H "$JSON" \
  --data-binary "@$TMP/m2.json")
M2_ID=$(echo "$M2" | idof)
check "Trả lời tin nhắn, có trích dẫn" "yes" \
  "$(echo "$M2" | grep -q '"reply_to_content"' && echo yes || echo no)"

HIST=$(curl -s "$BASE/chat/conversations/$D1_ID/messages" -H "$AUTH")
check "Lịch sử có đủ 2 tin" 2 \
  "$(echo "$HIST" | grep -o '"conversation_id"' | wc -l | tr -d ' ')"

# Tìm kiếm toàn văn, gõ KHÔNG DẤU.
FOUND=$(curl -s "$BASE/chat/conversations/$D1_ID/messages?q=kiem%20thu" -H "$AUTH")
check "Tìm không dấu 'kiem thu' ra tin có dấu" "yes" \
  "$(echo "$FOUND" | grep -q "$M1_ID" && echo yes || echo no)"

# ======================================================= sửa và thu hồi
echo
echo "── Sửa và thu hồi ──"

# Dấu hiệu chỉ gồm ký tự ASCII: phép thử phía sau tìm nó trong dữ liệu trả
# về, và một chuỗi có dấu bị hỏng mã hoá sẽ làm phép thử "không lộ nội dung"
# luôn đạt vì lý do sai.
LEAK_MARK="NOI-DUNG-DA-SUA-$$"
check "Sửa tin của mình → 200" 200 \
  "$(code -X PUT "$BASE/chat/messages/$M1_ID" -H "$AUTH" -H "$JSON" \
     -d "{\"content\":\"$LEAK_MARK\"}")"

check "Sửa tin của người khác → 403" 403 \
  "$(code -X PUT "$BASE/chat/messages/$M2_ID" -H "$AUTH" -H "$JSON" \
     -d '{"content":"Sửa trộm"}')"

check "Thu hồi tin của mình → 200" 200 \
  "$(code -X DELETE "$BASE/chat/messages/$M1_ID" -H "$AUTH")"

# Sau khi thu hồi, nội dung KHÔNG được còn trong dữ liệu trả về. Đây là phép
# thử bảo mật, không phải phép thử giao diện.
AFTER=$(curl -s "$BASE/chat/conversations/$D1_ID/messages" -H "$AUTH")
check "Tin đã thu hồi không lộ nội dung cũ" "yes" \
  "$(echo "$AFTER" | grep -q "$LEAK_MARK" && echo no || echo yes)"
check "Tin đã thu hồi vẫn nằm trong lịch sử" "yes" \
  "$(echo "$AFTER" | grep -q '"deleted":true' && echo yes || echo no)"

# ========================================================== chưa đọc
echo
echo "── Đếm chưa đọc ──"

# Gửi một tin MỚI trước khi đếm.
#
# Tin đầu tiên đã bị thu hồi ở phần trên, và tin đã thu hồi cố ý không tính
# vào số chưa đọc — không có bước này thì phép thử đo đúng con số 0 và báo
# hỏng vì một lý do hoàn toàn khác với thứ nó định kiểm tra.
curl -s -o /dev/null -X POST "$BASE/chat/conversations/$D1_ID/messages" \
  -H "$AUTH" -H "$JSON" -d '{"content":"Tin moi de dem chua doc"}'

LIST_PEER=$(curl -s "$BASE/chat/conversations" -H "$PEER_AUTH")
check "Người nhận thấy hội thoại trong danh sách" "yes" \
  "$(echo "$LIST_PEER" | grep -q "$D1_ID" && echo yes || echo no)"

UNREAD_BEFORE=$(curl -s "$BASE/chat/unread" -H "$PEER_AUTH" | num unread)
check "Người nhận có tin chưa đọc" "yes" \
  "$([ "${UNREAD_BEFORE:-0}" -gt 0 ] && echo yes || echo no)"

LAST_ID=$(curl -s "$BASE/chat/conversations/$D1_ID/messages" -H "$PEER_AUTH" \
  | grep -o '"id":"[^"]*"' | tail -1 | cut -d'"' -f4)
curl -s -o /dev/null -X POST "$BASE/chat/conversations/$D1_ID/read" \
  -H "$PEER_AUTH" -H "$JSON" -d "{\"message_id\":\"$LAST_ID\"}"

UNREAD_AFTER=$(curl -s "$BASE/chat/unread" -H "$PEER_AUTH" | num unread)
check "Đọc xong → số chưa đọc về 0" 0 "${UNREAD_AFTER:-x}"

# =============================================================== nhóm
echo
echo "── Nhóm ──"

G=$(curl -s -X POST "$BASE/chat/conversations" -H "$AUTH" -H "$JSON" \
  -d "{\"kind\":\"group\",\"name\":\"Nhóm kiểm thử\",\"member_ids\":[\"$PEER_ID\"]}")
G_ID=$(echo "$G" | idof)
check "Tạo nhóm → có id" "yes" "$([ -n "$G_ID" ] && echo yes || echo no)"
check "Người tạo là quản trị nhóm" "true" \
  "$(echo "$G" | grep -o '"is_admin":[a-z]*' | cut -d':' -f2)"

check "Nhóm không tên → 400" 400 \
  "$(code -X POST "$BASE/chat/conversations" -H "$AUTH" -H "$JSON" \
     -d '{"kind":"group","name":"  "}')"

GDET=$(curl -s "$BASE/chat/conversations/$G_ID" -H "$AUTH")
check "Nhóm có 2 thành viên" 2 \
  "$(echo "$GDET" | grep -o '"employee_id"' | wc -l | tr -d ' ')"
check "Có tin nhắn hệ thống 'đã tạo nhóm'" "yes" \
  "$(curl -s "$BASE/chat/conversations/$G_ID/messages" -H "$AUTH" \
     | grep -q 'đã tạo nhóm' && echo yes || echo no)"

check "Đổi tên nhóm → 200" 200 \
  "$(code -X PUT "$BASE/chat/conversations/$G_ID" -H "$AUTH" -H "$JSON" \
     -d '{"name":"Nhóm đã đổi tên"}')"

check "Thành viên thường đổi tên nhóm → 403" 403 \
  "$(code -X PUT "$BASE/chat/conversations/$G_ID" -H "$PEER_AUTH" -H "$JSON" \
     -d '{"name":"Đổi trộm"}')"

check "Quản trị tự bỏ quyền của mình → 400" 400 \
  "$(code -X PUT "$BASE/chat/conversations/$G_ID/members/$ME_ID/admin" \
     -H "$AUTH" -H "$JSON" -d '{"is_admin":false}')"

check "Gỡ thành viên → 200" 200 \
  "$(code -X DELETE "$BASE/chat/conversations/$G_ID/members/$PEER_ID" -H "$AUTH")"

# Người đã bị gỡ KHÔNG được đọc tiếp. Trả 404 chứ không 403: 403 xác nhận
# hội thoại đó có tồn tại, và với chat thì chính điều đó đã là thông tin.
check "Người đã bị gỡ đọc hội thoại → 404" 404 \
  "$(code "$BASE/chat/conversations/$G_ID" -H "$PEER_AUTH")"
check "Người đã bị gỡ gửi tin → 404" 404 \
  "$(code -X POST "$BASE/chat/conversations/$G_ID/messages" -H "$PEER_AUTH" -H "$JSON" \
     -d '{"content":"Lẻn vào"}')"

check "Thêm lại thành viên → 200" 200 \
  "$(code -X POST "$BASE/chat/conversations/$G_ID/members" -H "$AUTH" -H "$JSON" \
     -d "{\"member_ids\":[\"$PEER_ID\"]}")"
check "Thêm lại xong thì đọc được" 200 \
  "$(code "$BASE/chat/conversations/$G_ID" -H "$PEER_AUTH")"

check "Rời nhóm → 200" 200 \
  "$(code -X POST "$BASE/chat/conversations/$G_ID/leave" -H "$PEER_AUTH")"
check "Rời hội thoại 1-1 → 400" 400 \
  "$(code -X POST "$BASE/chat/conversations/$D1_ID/leave" -H "$AUTH")"

# ================================================== ghim và tắt thông báo
echo
echo "── Ghim và tắt thông báo ──"

check "Ghim hội thoại → 200" 200 \
  "$(code -X PATCH "$BASE/chat/conversations/$D1_ID/flags" -H "$AUTH" -H "$JSON" \
     -d '{"pinned":true}')"
check "Tắt thông báo hội thoại → 200" 200 \
  "$(code -X PATCH "$BASE/chat/conversations/$D1_ID/flags" -H "$AUTH" -H "$JSON" \
     -d '{"muted":true}')"

FLAGGED=$(curl -s "$BASE/chat/conversations" -H "$AUTH")
check "Hội thoại đã ghim nằm đầu danh sách" "$D1_ID" \
  "$(echo "$FLAGGED" | idof)"
check "Cờ tắt thông báo được lưu" "yes" \
  "$(echo "$FLAGGED" | grep -q '"is_muted":true' && echo yes || echo no)"

# ==================================================== nhóm tự động
echo
echo "── Nhóm tự động theo phòng ban và dự án ──"

AUTO=$(curl -s "$BASE/chat/conversations" -H "$AUTH")
check "Có nhóm phòng ban do hệ thống dựng" "yes" \
  "$(echo "$AUTO" | grep -q '"kind":"department"' && echo yes || echo no)"

AUTO_ID=$(echo "$AUTO" \
  | tr '}' '\n' | grep '"kind":"department"' | head -1 \
  | grep -o '"id":"[^"]*"' | cut -d'"' -f4)

if [ -n "$AUTO_ID" ]; then
  check "Nhóm tự động báo cờ managed" "yes" \
    "$(curl -s "$BASE/chat/conversations/$AUTO_ID" -H "$AUTH" \
       | grep -q '"managed":true' && echo yes || echo no)"
  # Thành viên nhóm tự động do job đồng bộ quyết định; sửa tay sẽ tạo ra hai
  # nguồn sự thật và người vừa chuyển phòng vẫn đọc được tin của phòng cũ.
  check "Sửa thành viên nhóm tự động → 400" 400 \
    "$(code -X POST "$BASE/chat/conversations/$AUTO_ID/members" -H "$AUTH" -H "$JSON" \
       -d "{\"member_ids\":[\"$PEER_ID\"]}")"
else
  echo "  (bỏ qua: chưa có nhóm phòng ban nào)"
fi

# ============================================================ thông báo
echo
echo "── Thông báo ──"

# Giao việc cho người khác phải sinh thông báo, qua đường
# api → RabbitMQ → worker → bảng notifications.
PROJ_ID=$(curl -s -X POST "$BASE/projects" -H "$AUTH" -H "$JSON" \
  -d "{\"code\":\"CHATSMOKE\",\"name\":\"Dự án kiểm thử chat\",\"owner_id\":\"$ME_ID\",\"status\":\"active\"}" | idof)

curl -s -o /dev/null -X POST "$BASE/projects/$PROJ_ID/members" -H "$AUTH" -H "$JSON" \
  -d "{\"employee_id\":\"$PEER_ID\",\"role\":\"member\"}"

TASK_ID=$(curl -s -X POST "$BASE/tasks" -H "$AUTH" -H "$JSON" \
  -d "{\"project_id\":\"$PROJ_ID\",\"title\":\"Việc sinh thông báo\",\"assignee_id\":\"$PEER_ID\"}" | idof)

# Sự kiện đi qua hàng đợi nên không tức thì. Chờ có giới hạn thay vì sleep
# cố định: chờ đủ thì xong sớm, hàng đợi chậm thì vẫn kịp.
NOTIF_FOUND=no
for _ in $(seq 1 40); do
  if curl -s "$BASE/notifications" -H "$PEER_AUTH" | grep -q 'task_assigned'; then
    NOTIF_FOUND=yes
    break
  fi
  sleep 0.5
done
check "Giao việc sinh thông báo cho người được giao" "yes" "$NOTIF_FOUND"

# Người TỰ giao việc cho mình không được nhận thông báo về việc mình vừa làm.
check "Người giao việc KHÔNG tự nhận thông báo" "yes" \
  "$(curl -s "$BASE/notifications" -H "$AUTH" | grep -q "$TASK_ID" && echo no || echo yes)"

SUM=$(curl -s "$BASE/notifications/summary" -H "$PEER_AUTH" | num unread)
check "Số chưa đọc lớn hơn 0" "yes" \
  "$([ "${SUM:-0}" -gt 0 ] && echo yes || echo no)"

NOTIF_ID=$(curl -s "$BASE/notifications" -H "$PEER_AUTH" | idof)
check "Đánh dấu đã đọc → 200" 200 \
  "$(code -X POST "$BASE/notifications/read" -H "$PEER_AUTH" -H "$JSON" \
     -d "{\"ids\":[\"$NOTIF_ID\"]}")"

# Đánh dấu hộ thông báo của người khác phải VÔ HIỆU, không phải báo lỗi:
# điều kiện employee_id nằm ngay trong câu UPDATE.
curl -s -o /dev/null -X POST "$BASE/notifications/read" -H "$AUTH" -H "$JSON" \
  -d "{\"ids\":[\"$NOTIF_ID\"]}"
check "Không đánh dấu hộ được thông báo người khác" "yes" \
  "$(curl -s "$BASE/notifications?unread=true" -H "$PEER_AUTH" \
     | grep -q "$NOTIF_ID" && echo no || echo yes)"

check "Đánh dấu đã đọc tất cả → 200" 200 \
  "$(code -X POST "$BASE/notifications/read-all" -H "$PEER_AUTH")"
check "Sau read-all, số chưa đọc = 0" 0 \
  "$(curl -s "$BASE/notifications/summary" -H "$PEER_AUTH" | num unread)"

# --------------------------------------------------- cấu hình thông báo
PREFS=$(curl -s "$BASE/notifications/preferences" -H "$PEER_AUTH")
check "Danh mục cấu hình có đủ 11 loại" 11 \
  "$(echo "$PREFS" | grep -o '"type"' | wc -l | tr -d ' ')"
check "Loại bắt buộc bị khoá" "yes" \
  "$(echo "$PREFS" | grep -q '"locked":true' && echo yes || echo no)"

check "Tắt một loại tắt được → 200" 200 \
  "$(code -X PUT "$BASE/notifications/preferences" -H "$PEER_AUTH" -H "$JSON" \
     -d '{"type":"task_status_changed","enabled":false}')"
check "Tắt loại bắt buộc → 400" 400 \
  "$(code -X PUT "$BASE/notifications/preferences" -H "$PEER_AUTH" -H "$JSON" \
     -d '{"type":"leave_decided","enabled":false}')"
check "Loại không tồn tại → 400" 400 \
  "$(code -X PUT "$BASE/notifications/preferences" -H "$PEER_AUTH" -H "$JSON" \
     -d '{"type":"khong_co_that","enabled":false}')"

# Đã tắt thì sự kiện tiếp theo KHÔNG được sinh thông báo.
curl -s -o /dev/null -X PUT "$BASE/tasks/$TASK_ID" -H "$AUTH" -H "$JSON" \
  -d '{"status":"in_progress"}'
sleep 3
check "Loại đã tắt không sinh thông báo mới" "yes" \
  "$(curl -s "$BASE/notifications?type=task_status_changed" -H "$PEER_AUTH" \
     | grep -q 'task_status_changed' && echo no || echo yes)"

curl -s -o /dev/null -X PUT "$BASE/notifications/preferences" -H "$PEER_AUTH" -H "$JSON" \
  -d '{"type":"task_status_changed","enabled":true}'

# ========================================== phân quyền và cách ly dữ liệu
echo
echo "── Phân quyền ──"

check "Chat không token → 401" 401 "$(code "$BASE/chat/conversations")"
check "Thông báo không token → 401" 401 "$(code "$BASE/notifications")"
check "Token rác → 401" 401 \
  "$(code "$BASE/chat/conversations" -H 'Authorization: Bearer rac')"

check "Đọc hội thoại không tồn tại → 404" 404 \
  "$(code "$BASE/chat/conversations/00000000-0000-0000-0000-000000000000" -H "$AUTH")"
check "id hội thoại sai định dạng → 400" 400 \
  "$(code "$BASE/chat/conversations/khong-phai-uuid" -H "$AUTH")"

# Hội thoại 1-1 của hai người khác: người thứ ba không được thấy nó tồn tại.
# Ở đây dùng chính hội thoại admin↔peer và kiểm tra rằng nhóm đã rời không
# còn trong danh sách của peer.
check "Nhóm đã rời biến khỏi danh sách" "yes" \
  "$(curl -s "$BASE/chat/conversations" -H "$PEER_AUTH" \
     | grep -q "$G_ID" && echo no || echo yes)"

# --------------------------------------------------------------- dọn dẹp
echo
echo "── Dọn dữ liệu kiểm thử ──"

cleanup

echo "  đã xoá hội thoại, thông báo, dự án và nhân viên vừa tạo"

echo
echo "═══════════════════════════════════════"
printf '  Đạt: \033[32m%d\033[0m   Hỏng: \033[31m%d\033[0m\n' "$PASS" "$FAIL"
echo "═══════════════════════════════════════"
echo

[ "$FAIL" -eq 0 ]
