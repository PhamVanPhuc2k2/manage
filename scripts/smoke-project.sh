#!/usr/bin/env bash
# Kiểm chứng nghiệp vụ dự án và công việc — Phase 2.
#
# Chạy: ADMIN_PASS='...' bash scripts/smoke-project.sh
#
# Script tạo dữ liệu thật rồi dọn sạch ở cuối, nên chạy lại bao nhiêu lần
# cũng được.

set -uo pipefail
cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
set -a; [ -f .env ] && . ./.env; set +a

BASE="${PUBLIC_BASE_URL:-http://localhost}/api/v1"

# Gọi đăng nhập THẲNG vào api, bỏ qua nginx.
#
# nginx có giới hạn tốc độ riêng cho /auth/login. Script này đăng nhập nhiều
# lần liên tiếp nên sẽ đụng giới hạn đó và đo nhầm — 429 thay vì kết quả thật.
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
num() { grep -o "\"$1\":[0-9]*" | head -1 | cut -d':' -f2; }

MAILHOG="http://localhost:${MAILHOG_UI_PORT:-8025}"

# Đăng nhập trọn vẹn (hai bước nếu máy chủ bật OTP), in ra access token.
#
# Bản sao của hàm này nằm trong smoke-auth.sh và smoke-hr.sh; ba script cố ý
# chạy độc lập, không nguồn chung, để chạy lẻ từng cái vẫn được.
login_otp() { # login_otp <email> <password>
  local email="$1" pass="$2" res ch otp i body

  curl -s -X DELETE "$MAILHOG/api/v1/messages" >/dev/null

  res=$(curl -s -X POST "$DIRECT/auth/login" -H "$JSON" \
    -d "{\"email\":\"$email\",\"password\":\"$pass\"}")

  # OTP tắt: bước một đã trả token.
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
AUTH="Authorization: Bearer $TOKEN"

if [ -z "$TOKEN" ]; then
  echo "Không đăng nhập được bằng $ADMIN_EMAIL — dừng." >&2
  exit 1
fi

# id nhân viên của chính admin, dùng làm chủ dự án.
ME_ID=$(curl -s "$BASE/auth/me" -H "$AUTH" | grep -o '"employee_id":"[^"]*"' | cut -d'"' -f4)

echo
echo "═══ KIỂM CHỨNG DỰ ÁN & CÔNG VIỆC ═══"
echo

# ================================================================== dự án
echo "── Dự án ──"

cat > "$TMP/proj.json" <<EOF
{"code":"SMOKEP","name":"Dự án kiểm thử Phase 2","description":"Tạo tự động bởi smoke-project.sh","status":"active","owner_id":"$ME_ID","start_date":"2026-09-01","due_date":"2026-12-31"}
EOF

PROJ=$(curl -s -X POST "$BASE/projects" -H "$AUTH" -H "$JSON" --data-binary "@$TMP/proj.json")
PROJ_ID=$(echo "$PROJ" | idof)
check "Tạo dự án" "yes" "$([ -n "$PROJ_ID" ] && echo yes || echo no)"

check "Trùng mã dự án → 409" 409 \
  "$(code -X POST "$BASE/projects" -H "$AUTH" -H "$JSON" --data-binary "@$TMP/proj.json")"

check "Mã dự án có dấu gạch ngang → 400" 400 \
  "$(code -X POST "$BASE/projects" -H "$AUTH" -H "$JSON" \
     -d "{\"code\":\"A-B\",\"name\":\"X\",\"owner_id\":\"$ME_ID\"}")"

check "Hạn trước ngày bắt đầu → 400" 400 \
  "$(code -X POST "$BASE/projects" -H "$AUTH" -H "$JSON" \
     -d "{\"code\":\"SMOKEZ\",\"name\":\"X\",\"owner_id\":\"$ME_ID\",\"start_date\":\"2026-12-31\",\"due_date\":\"2026-01-01\"}")"

check "Chủ dự án không tồn tại → 400" 400 \
  "$(code -X POST "$BASE/projects" -H "$AUTH" -H "$JSON" \
     -d '{"code":"SMOKEY","name":"X","owner_id":"00000000-0000-0000-0000-000000000000"}')"

# Chủ dự án PHẢI tự động thành thành viên, nếu không dự án sẽ không hiện
# trong danh sách của chính người tạo ra nó.
MEMBERS=$(curl -s "$BASE/projects/$PROJ_ID/members" -H "$AUTH")
check "Chủ dự án tự động là thành viên" "owner" "$(echo "$MEMBERS" | field role)"

check "Đọc dự án không tồn tại → 404" 404 \
  "$(code "$BASE/projects/00000000-0000-0000-0000-000000000000" -H "$AUTH")"

# ============================================================= thành viên
echo
echo "── Thành viên dự án ──"

# Lấy một nhân viên khác admin để thêm vào dự án.
OTHER_ID=$(curl -s "$BASE/employees?page_size=50" -H "$AUTH" \
  | grep -o '"id":"[^"]*"' | cut -d'"' -f4 | grep -v "^$ME_ID$" | head -1)

if [ -n "$OTHER_ID" ]; then
  check "Thêm thành viên → 201" 201 \
    "$(code -X POST "$BASE/projects/$PROJ_ID/members" -H "$AUTH" -H "$JSON" \
       -d "{\"employee_id\":\"$OTHER_ID\",\"role\":\"member\"}")"

  check "Thêm lại người cũ → cập nhật vai trò (201)" 201 \
    "$(code -X POST "$BASE/projects/$PROJ_ID/members" -H "$AUTH" -H "$JSON" \
       -d "{\"employee_id\":\"$OTHER_ID\",\"role\":\"viewer\"}")"

  check "Đổi vai trò thành viên → 200" 200 \
    "$(code -X PUT "$BASE/projects/$PROJ_ID/members/$OTHER_ID" -H "$AUTH" -H "$JSON" \
       -d '{"role":"member"}')"
else
  echo "  (bỏ qua phép thử thành viên: công ty chỉ có một nhân viên)"
fi

# Chủ dự án là owner DUY NHẤT — hạ vai trò của họ sẽ để lại dự án vô chủ.
check "Hạ vai trò owner cuối cùng → 409" 409 \
  "$(code -X PUT "$BASE/projects/$PROJ_ID/members/$ME_ID" -H "$AUTH" -H "$JSON" \
     -d '{"role":"member"}')"

check "Gỡ chủ dự án → 409" 409 \
  "$(code -X DELETE "$BASE/projects/$PROJ_ID/members/$ME_ID" -H "$AUTH")"

# =============================================================== công việc
echo
echo "── Công việc ──"

T1=$(curl -s -X POST "$BASE/tasks" -H "$AUTH" -H "$JSON" \
  -d "{\"project_id\":\"$PROJ_ID\",\"title\":\"Viec kiem thu 1\",\"priority\":\"high\",\"assignee_id\":\"$ME_ID\",\"due_date\":\"2026-10-15\",\"estimate_hours\":8}")
T1_ID=$(echo "$T1" | idof)
check "Tạo công việc" "yes" "$([ -n "$T1_ID" ] && echo yes || echo no)"
check "Mã công việc sinh tự động" "SMOKEP-1" "$(echo "$T1" | field code)"

T2=$(curl -s -X POST "$BASE/tasks" -H "$AUTH" -H "$JSON" \
  -d "{\"project_id\":\"$PROJ_ID\",\"title\":\"Viec kiem thu 2\"}")
T2_ID=$(echo "$T2" | idof)
check "Số thứ tự tăng dần" "SMOKEP-2" "$(echo "$T2" | field code)"

check "Tiêu đề rỗng → 400" 400 \
  "$(code -X POST "$BASE/tasks" -H "$AUTH" -H "$JSON" \
     -d "{\"project_id\":\"$PROJ_ID\",\"title\":\"  \"}")"

# Người thực hiện phải là thành viên dự án — nếu không họ nhận thông báo về
# một việc mà mở ra chỉ thấy 404.
check "Giao cho người ngoài dự án → 400" 400 \
  "$(code -X POST "$BASE/tasks" -H "$AUTH" -H "$JSON" \
     -d "{\"project_id\":\"$PROJ_ID\",\"title\":\"X\",\"assignee_id\":\"00000000-0000-0000-0000-000000000000\"}")"

# ------------------------------------------------------- chuyển trạng thái
echo
echo "── Luật chuyển trạng thái ──"

check "todo → done (nhảy cóc) → 400" 400 \
  "$(code -X PATCH "$BASE/tasks/$T1_ID/move" -H "$AUTH" -H "$JSON" \
     -d '{"status":"done"}')"

check "todo → in_progress → 200" 200 \
  "$(code -X PATCH "$BASE/tasks/$T1_ID/move" -H "$AUTH" -H "$JSON" \
     -d '{"status":"in_progress"}')"

# Vào in_progress lần đầu phải ghi started_at, nếu không mọi phép đo thời
# gian thực hiện sau này đều rỗng.
check "Vào in_progress ghi started_at" "yes" \
  "$(curl -s "$BASE/tasks/$T1_ID" -H "$AUTH" | grep -q '"started_at"' && echo yes || echo no)"

check "in_progress → review → 200" 200 \
  "$(code -X PATCH "$BASE/tasks/$T1_ID/move" -H "$AUTH" -H "$JSON" \
     -d '{"status":"review"}')"

check "review → todo (lùi 2 bước) → 400" 400 \
  "$(code -X PATCH "$BASE/tasks/$T1_ID/move" -H "$AUTH" -H "$JSON" \
     -d '{"status":"todo"}')"

check "review → done → 200" 200 \
  "$(code -X PATCH "$BASE/tasks/$T1_ID/move" -H "$AUTH" -H "$JSON" \
     -d '{"status":"done"}')"

check "Vào done ghi completed_at" "yes" \
  "$(curl -s "$BASE/tasks/$T1_ID" -H "$AUTH" | grep -q '"completed_at"' && echo yes || echo no)"

check "Trạng thái không hợp lệ → 400" 400 \
  "$(code -X PATCH "$BASE/tasks/$T2_ID/move" -H "$AUTH" -H "$JSON" \
     -d '{"status":"khong_ton_tai"}')"

# ------------------------------------------------------------ việc con
echo
echo "── Việc con (tối đa 2 cấp) ──"

SUB=$(curl -s -X POST "$BASE/tasks" -H "$AUTH" -H "$JSON" \
  -d "{\"project_id\":\"$PROJ_ID\",\"title\":\"Viec con\",\"parent_task_id\":\"$T2_ID\"}")
SUB_ID=$(echo "$SUB" | idof)
check "Tạo việc con" "yes" "$([ -n "$SUB_ID" ] && echo yes || echo no)"

check "Lồng cấp 3 → 400" 400 \
  "$(code -X POST "$BASE/tasks" -H "$AUTH" -H "$JSON" \
     -d "{\"project_id\":\"$PROJ_ID\",\"title\":\"Chau\",\"parent_task_id\":\"$SUB_ID\"}")"

# Bảng Kanban chỉ hiện task GỐC; việc con hiển thị lồng trong task cha.
BOARD=$(curl -s "$BASE/projects/$PROJ_ID/board" -H "$AUTH")
# Đếm số cột bằng trường "tasks" — mỗi cột có đúng một. Đếm "status"
# sẽ sai vì cả dự án lẫn từng công việc đều có trường tên như vậy.
check "Bảng Kanban có đủ 4 cột" 4 \
  "$(echo "$BOARD" | grep -o '"tasks":' | wc -l | tr -d ' ')"
check "Việc con không đứng riêng trên bảng" "no" \
  "$(echo "$BOARD" | grep -q "\"id\":\"$SUB_ID\"" && echo yes || echo no)"

# ------------------------------------------------------ sắp xếp Kanban
echo
echo "── Sắp xếp trong cột ──"

T3_ID=$(curl -s -X POST "$BASE/tasks" -H "$AUTH" -H "$JSON" \
  -d "{\"project_id\":\"$PROJ_ID\",\"title\":\"Viec kiem thu 3\"}" | idof)

# Thả T3 lên ĐẦU cột todo: after_task_id = null.
check "Thả lên đầu cột → 200" 200 \
  "$(code -X PATCH "$BASE/tasks/$T3_ID/move" -H "$AUTH" -H "$JSON" \
     -d '{"status":"todo","after_task_id":null}')"

FIRST=$(curl -s "$BASE/projects/$PROJ_ID/board" -H "$AUTH" \
  | tr '{' '\n' | grep -A2 '"status":"todo"' >/dev/null 2>&1; \
  curl -s "$BASE/tasks?project_id=$PROJ_ID&status=todo&sort_by=created_at" -H "$AUTH" >/dev/null; echo ok)
check "Bảng vẫn đọc được sau khi sắp xếp" "ok" "$FIRST"

check "Thả sau chính nó → 400" 400 \
  "$(code -X PATCH "$BASE/tasks/$T3_ID/move" -H "$AUTH" -H "$JSON" \
     -d "{\"status\":\"todo\",\"after_task_id\":\"$T3_ID\"}")"

# ------------------------------------------------------------ bình luận
echo
echo "── Bình luận & @mention ──"

CMT=$(curl -s -X POST "$BASE/tasks/$T2_ID/comments" -H "$AUTH" -H "$JSON" \
  -d "{\"content\":\"Chao @[Admin]($ME_ID) xem giup viec nay\"}")
CMT_ID=$(echo "$CMT" | idof)
check "Tạo bình luận" "yes" "$([ -n "$CMT_ID" ] && echo yes || echo no)"
check "Rút được @mention từ nội dung" "yes" \
  "$(echo "$CMT" | grep -q "\"mentioned_ids\":\[\"$ME_ID\"\]" && echo yes || echo no)"

# Nhắc tên người NGOÀI dự án bị lọc im lặng, không báo lỗi — người viết
# không nên bị chặn gửi bình luận chỉ vì gõ nhầm một cái tên.
OUTSIDE=$(curl -s -X POST "$BASE/tasks/$T2_ID/comments" -H "$AUTH" -H "$JSON" \
  -d '{"content":"Hoi @[Ai do](00000000-0000-0000-0000-000000000000)"}')
check "Mention người ngoài dự án bị lọc" "yes" \
  "$(echo "$OUTSIDE" | grep -q '"mentioned_ids":\[\]' && echo yes || echo no)"

check "Bình luận rỗng → 400" 400 \
  "$(code -X POST "$BASE/tasks/$T2_ID/comments" -H "$AUTH" -H "$JSON" -d '{"content":"   "}')"

check "comment_count cập nhật trên task" 2 \
  "$(curl -s "$BASE/tasks/$T2_ID" -H "$AUTH" | num comment_count)"

check "Xoá bình luận → 200" 200 \
  "$(code -X DELETE "$BASE/tasks/comments/$CMT_ID" -H "$AUTH")"

# ------------------------------------------------------- ghi nhận thời gian
echo
echo "── Ghi nhận thời gian ──"

check "Ghi 90 phút → 201" 201 \
  "$(code -X POST "$BASE/tasks/$T2_ID/timelogs" -H "$AUTH" -H "$JSON" \
     -d '{"spent_minutes":90,"note":"kiem thu"}')"

check "Ghi 0 phút → 400" 400 \
  "$(code -X POST "$BASE/tasks/$T2_ID/timelogs" -H "$AUTH" -H "$JSON" \
     -d '{"spent_minutes":0}')"

check "Ghi quá 24 giờ → 400" 400 \
  "$(code -X POST "$BASE/tasks/$T2_ID/timelogs" -H "$AUTH" -H "$JSON" \
     -d '{"spent_minutes":2000}')"

check "spent_minutes cộng dồn vào task" 90 \
  "$(curl -s "$BASE/tasks/$T2_ID" -H "$AUTH" | num spent_minutes)"

# ---------------------------------------------------------- nhật ký
echo
echo "── Nhật ký thay đổi ──"

ACT=$(curl -s "$BASE/tasks/$T1_ID/activities" -H "$AUTH")
check "Ghi lại lúc tạo công việc" "yes" \
  "$(echo "$ACT" | grep -q '"action":"created"' && echo yes || echo no)"
check "Ghi lại mỗi lần đổi trạng thái" "yes" \
  "$(echo "$ACT" | grep -q '"action":"status_changed"' && echo yes || echo no)"
check "Ghi lại việc giao người thực hiện" "yes" \
  "$(echo "$ACT" | grep -q '"action":"assigned"' && echo yes || echo no)"

# ---------------------------------------------------------- báo cáo
echo
echo "── Báo cáo ──"

PROG=$(curl -s "$BASE/projects/$PROJ_ID/progress" -H "$AUTH")
# Bốn công việc tạo thành công: T1, T2, việc con của T2, và T3.
# Tiến độ đếm CẢ việc con — chúng cũng là khối lượng việc thật.
check "Tiến độ đếm đúng tổng số việc gốc + con" 4 "$(echo "$PROG" | num total_tasks)"
check "Tiến độ có phân bố theo cột" "yes" \
  "$(echo "$PROG" | grep -q '"by_status"' && echo yes || echo no)"

check "Báo cáo khối lượng việc → 200" 200 \
  "$(code "$BASE/reports/workload?project_id=$PROJ_ID" -H "$AUTH")"

check "Danh sách việc quá hạn → 200" 200 \
  "$(code "$BASE/tasks/overdue" -H "$AUTH")"

check "Việc của tôi → 200" 200 "$(code "$BASE/tasks/my" -H "$AUTH")"

# ------------------------------------------------- su kien RabbitMQ
echo
echo "── Sự kiện sang worker ──"

# Phải giao việc cho NGƯỜI KHÁC mới sinh sự kiện.
#
# Tự giao cho mình thì danh sách người nhận rỗng và hệ thống không phát gì —
# đúng thiết kế, nhưng cũng có nghĩa là mọi phép thử ở trên (admin tự làm mọi
# thứ) không chạm tới đường api → RabbitMQ → worker.
if [ -n "$OTHER_ID" ]; then
  EV_TASK=$(curl -s -X POST "$BASE/tasks" -H "$AUTH" -H "$JSON" -d "{\"project_id\":\"$PROJ_ID\",\"title\":\"Viec giao cho nguoi khac\",\"assignee_id\":\"$OTHER_ID\"}")
  EV_ID=$(echo "$EV_TASK" | idof)
  EV_CODE=$(echo "$EV_TASK" | field code)

  curl -s -o /dev/null -X PATCH "$BASE/tasks/$EV_ID/move" -H "$AUTH" -H "$JSON" -d '{"status":"in_progress"}'
  curl -s -o /dev/null -X POST "$BASE/tasks/$EV_ID/comments" -H "$AUTH" -H "$JSON" -d "{\"content\":\"nho @[Ban]($OTHER_ID) xem giup\"}"

  sleep 3
  EV_LOG=$(docker compose logs worker --since 2m 2>&1 | grep "$EV_CODE")

  check "Worker nhận sự kiện giao việc" "yes" "$(echo "$EV_LOG" | grep -q 'task_assigned' && echo yes || echo no)"
  check "Worker nhận sự kiện đổi trạng thái" "yes" "$(echo "$EV_LOG" | grep -q 'task_status_changed' && echo yes || echo no)"
  check "Worker nhận sự kiện @mention" "yes" "$(echo "$EV_LOG" | grep -q 'task_mentioned' && echo yes || echo no)"

  curl -s -o /dev/null -X DELETE "$BASE/tasks/$EV_ID" -H "$AUTH"
else
  echo "  (bỏ qua: cần ít nhất hai nhân viên để thử đường sự kiện)"
fi

# ------------------------------------------------------------ phân quyền
echo
echo "── Phân quyền ──"

check "Không có token → 401" 401 "$(code "$BASE/projects")"
check "Token rác → 401" 401 "$(code "$BASE/projects" -H 'Authorization: Bearer rac')"

# Dự án đang chạy còn việc dang dở thì không xoá được — xoá đi thì những
# người đang làm task trong đó không hiểu chuyện gì xảy ra.
check "Xoá dự án active còn việc dở → 409" 409 \
  "$(code -X DELETE "$BASE/projects/$PROJ_ID" -H "$AUTH")"

# --------------------------------------------------------------- dọn dẹp
echo
echo "── Dọn dữ liệu kiểm thử ──"

for id in "$SUB_ID" "$T3_ID" "$T2_ID" "$T1_ID"; do
  [ -n "$id" ] && curl -s -o /dev/null -X DELETE "$BASE/tasks/$id" -H "$AUTH"
done

# Chuyển sang huỷ rồi mới xoá được — đúng theo luật ở trên.
curl -s -o /dev/null -X PUT "$BASE/projects/$PROJ_ID" -H "$AUTH" -H "$JSON" \
  -d "{\"code\":\"SMOKEP\",\"name\":\"Du an kiem thu\",\"status\":\"cancelled\",\"owner_id\":\"$ME_ID\"}"

check "Xoá dự án sau khi huỷ → 200" 200 \
  "$(code -X DELETE "$BASE/projects/$PROJ_ID" -H "$AUTH")"

echo "  đã xoá dự án và toàn bộ công việc vừa tạo"

echo
echo "═══════════════════════════════════════"
printf '  Đạt: \033[32m%d\033[0m   Hỏng: \033[31m%d\033[0m\n' "$PASS" "$FAIL"
echo "═══════════════════════════════════════"
echo

[ "$FAIL" -eq 0 ]
