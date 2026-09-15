#!/usr/bin/env bash
# Kiểm chứng nghiệp vụ nhân sự Phase 1.
#
# Chạy: ADMIN_PASS='...' bash scripts/smoke-hr.sh
#
# Script tạo dữ liệu thật rồi dọn sạch ở cuối, nên chạy lại bao nhiêu lần
# cũng được.

set -uo pipefail
cd "$(dirname "$0")/.."

# shellcheck disable=SC1091
set -a; [ -f .env ] && . ./.env; set +a

BASE="${PUBLIC_BASE_URL:-http://localhost}/api/v1"
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

TOKEN=$(curl -s -X POST "$BASE/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASS\"}" \
  | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
AUTH="Authorization: Bearer $TOKEN"
JSON='Content-Type: application/json'

code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
idof() { grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4; }

echo
echo "═══ KIỂM CHỨNG NGHIỆP VỤ NHÂN SỰ ═══"
echo

# ------------------------------------------------------------------ chức vụ
echo "── Chức vụ ──"
# Ghi JSON ra file để giữ đúng UTF-8: gõ tiếng Việt thẳng trên dòng lệnh
# Git Bash sẽ làm hỏng mã.
cat > "$TMP/pos.json" <<'EOF'
{"code":"SMOKE_DEV","name":"Lập trình viên kiểm thử","salary_min":15000000,"salary_max":25000000}
EOF
POS=$(curl -s -X POST "$BASE/positions" -H "$AUTH" -H "$JSON" --data-binary "@$TMP/pos.json")
POS_ID=$(echo "$POS" | idof)
check "Tạo chức vụ" "yes" "$([ -n "$POS_ID" ] && echo yes || echo no)"
check "Trùng mã chức vụ → 409" 409 \
  "$(code -X POST "$BASE/positions" -H "$AUTH" -H "$JSON" --data-binary "@$TMP/pos.json")"
check "Lương min > max → 400" 400 \
  "$(code -X POST "$BASE/positions" -H "$AUTH" -H "$JSON" \
     -d '{"code":"SMOKE_X","name":"X","salary_min":9,"salary_max":1}')"

# ---------------------------------------------------------------- phòng ban
echo
echo "── Phòng ban ──"
cat > "$TMP/dept.json" <<'EOF'
{"code":"SMOKE_A","name":"Phòng kiểm thử A"}
EOF
DEPT=$(curl -s -X POST "$BASE/departments" -H "$AUTH" -H "$JSON" --data-binary "@$TMP/dept.json")
DEPT_ID=$(echo "$DEPT" | idof)
check "Tạo phòng ban" "yes" "$([ -n "$DEPT_ID" ] && echo yes || echo no)"

cat > "$TMP/dept2.json" <<EOF
{"code":"SMOKE_B","name":"Phòng kiểm thử B","parent_id":"$DEPT_ID"}
EOF
CHILD_ID=$(curl -s -X POST "$BASE/departments" -H "$AUTH" -H "$JSON" \
  --data-binary "@$TMP/dept2.json" | idof)
check "Tạo phòng con" "yes" "$([ -n "$CHILD_ID" ] && echo yes || echo no)"

# Đây là phép thử quan trọng nhất của cây: chuyển phòng cha vào bên trong
# phòng con của chính nó sẽ tạo vòng lặp và treo mọi truy vấn đệ quy.
cat > "$TMP/cycle.json" <<EOF
{"code":"SMOKE_A","name":"Phòng kiểm thử A","parent_id":"$CHILD_ID"}
EOF
check "Chuyển cha vào trong con (vòng lặp) → 400" 400 \
  "$(code -X PUT "$BASE/departments/$DEPT_ID" -H "$AUTH" -H "$JSON" \
     --data-binary "@$TMP/cycle.json")"

check "Xoá phòng còn phòng con → 409" 409 \
  "$(code -X DELETE "$BASE/departments/$DEPT_ID" -H "$AUTH")"

# ---------------------------------------------------------------- nhân viên
echo
echo "── Nhân viên ──"
cat > "$TMP/emp.json" <<EOF
{"employee_code":"SMOKE001","full_name":"Lê Thị Kiểm Thử","email":"smoke001@test.local",
 "phone":"0901112223","department_id":"$CHILD_ID","position_id":"$POS_ID",
 "work_mode":"hybrid","status":"official","joined_at":"2026-01-10"}
EOF
EMP=$(curl -s -X POST "$BASE/employees" -H "$AUTH" -H "$JSON" --data-binary "@$TMP/emp.json")
EMP_ID=$(echo "$EMP" | idof)
check "Tạo nhân viên" "yes" "$([ -n "$EMP_ID" ] && echo yes || echo no)"
check "Trùng mã nhân viên → 409" 409 \
  "$(code -X POST "$BASE/employees" -H "$AUTH" -H "$JSON" --data-binary "@$TMP/emp.json")"

check "Tìm không dấu 'le thi kiem thu'" "yes" \
  "$(curl -s "$BASE/employees?search=le%20thi%20kiem%20thu" -H "$AUTH" \
     | grep -q SMOKE001 && echo yes || echo no)"

check "Lọc theo phòng ban" "yes" \
  "$(curl -s "$BASE/employees?department_id=$CHILD_ID" -H "$AUTH" \
     | grep -q SMOKE001 && echo yes || echo no)"

check "Tự làm cấp trên của mình → 400" 400 \
  "$(code -X PUT "$BASE/employees/$EMP_ID" -H "$AUTH" -H "$JSON" \
     -d "{\"employee_code\":\"SMOKE001\",\"full_name\":\"X\",\"email\":\"smoke001@test.local\",\"manager_id\":\"$EMP_ID\",\"work_mode\":\"onsite\",\"status\":\"official\",\"joined_at\":\"2026-01-10\"}")"

check "Xoá chức vụ đang có người dùng → 409" 409 \
  "$(code -X DELETE "$BASE/positions/$POS_ID" -H "$AUTH")"
check "Xoá phòng ban còn nhân viên → 409" 409 \
  "$(code -X DELETE "$BASE/departments/$CHILD_ID" -H "$AUTH")"

# ------------------------------------------------------- tài khoản & vai trò
echo
echo "── Tài khoản và vai trò ──"
ACC=$(curl -s -X POST "$BASE/employees/$EMP_ID/account" -H "$AUTH")
TEMP_PW=$(echo "$ACC" | grep -o '"temp_password":"[^"]*"' | cut -d'"' -f4)
check "Tạo tài khoản, có mật khẩu tạm" "yes" \
  "$([ -n "$TEMP_PW" ] && echo yes || echo no)"
check "Tạo tài khoản lần hai → 409" 409 \
  "$(code -X POST "$BASE/employees/$EMP_ID/account" -H "$AUTH")"

check "Đăng nhập bằng mật khẩu tạm → 200" 200 \
  "$(code -X POST "$BASE/auth/login" -H "$JSON" \
     -d "{\"email\":\"smoke001@test.local\",\"password\":\"$TEMP_PW\"}")"
check "Bị bắt đổi mật khẩu ngay lần đầu" "true" \
  "$(curl -s -X POST "$BASE/auth/login" -H "$JSON" \
     -d "{\"email\":\"smoke001@test.local\",\"password\":\"$TEMP_PW\"}" \
     | grep -o '"must_change_password":[a-z]*' | cut -d: -f2)"

check "Vai trò mặc định là employee" '"roles":["employee"]' \
  "$(curl -s "$BASE/employees/$EMP_ID/roles" -H "$AUTH" | grep -o '"roles":\[[^]]*\]')"
check "Gán vai trò không tồn tại → 400" 400 \
  "$(code -X PUT "$BASE/employees/$EMP_ID/roles" -H "$AUTH" -H "$JSON" \
     -d '{"roles":["khong_co_that"]}')"
check "Danh sách vai trò rỗng → 400" 400 \
  "$(code -X PUT "$BASE/employees/$EMP_ID/roles" -H "$AUTH" -H "$JSON" -d '{"roles":[]}')"
check "Nâng lên manager → 200" 200 \
  "$(code -X PUT "$BASE/employees/$EMP_ID/roles" -H "$AUTH" -H "$JSON" \
     -d '{"roles":["employee","manager"]}')"
check "Phạm vi dữ liệu đổi theo vai trò" '"scope":"department"' \
  "$(curl -s "$BASE/auth/me" -H "Authorization: Bearer $(curl -s -X POST "$BASE/auth/login" \
     -H "$JSON" -d "{\"email\":\"smoke001@test.local\",\"password\":\"$TEMP_PW\"}" \
     | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)" | grep -o '"scope":"[^"]*"')"

check "Vô hiệu hoá tài khoản → 200" 200 \
  "$(code -X PUT "$BASE/employees/$EMP_ID/account/active" -H "$AUTH" -H "$JSON" \
     -d '{"active":false}')"
check "Tài khoản đã tắt không đăng nhập được → 403" 403 \
  "$(code -X POST "$BASE/auth/login" -H "$JSON" \
     -d "{\"email\":\"smoke001@test.local\",\"password\":\"$TEMP_PW\"}")"

# ----------------------------------------------------------------- ảnh đại diện
echo
echo "── Ảnh đại diện ──"
check "Kiểu tệp không phải ảnh → 400" 400 \
  "$(code -X POST "$BASE/employees/$EMP_ID/avatar/upload-url" -H "$AUTH" -H "$JSON" \
     -d '{"content_type":"application/x-msdownload"}')"

R2_CODE=$(code -X POST "$BASE/employees/$EMP_ID/avatar/upload-url" -H "$AUTH" -H "$JSON" \
  -d '{"content_type":"image/png"}')
if [ "$R2_CODE" = "422" ]; then
  echo "  · R2 chưa cấu hình — bỏ qua phần tải ảnh (đúng như thiết kế)"
else
  check "Xin URL tải ảnh → 200" 200 "$R2_CODE"
fi

# --------------------------------------------------------------------- dọn dẹp
echo
echo "── Dọn dữ liệu kiểm thử ──"
curl -s -o /dev/null -X DELETE "$BASE/employees/$EMP_ID" -H "$AUTH"
curl -s -o /dev/null -X DELETE "$BASE/departments/$CHILD_ID" -H "$AUTH"
curl -s -o /dev/null -X DELETE "$BASE/departments/$DEPT_ID" -H "$AUTH"
curl -s -o /dev/null -X DELETE "$BASE/positions/$POS_ID" -H "$AUTH"
echo "  đã xoá nhân viên, 2 phòng ban và chức vụ vừa tạo"

echo
echo "═══════════════════════════════════════"
printf '  Đạt: \033[32m%d\033[0m   Hỏng: \033[31m%d\033[0m\n' "$PASS" "$FAIL"
echo "═══════════════════════════════════════"
echo

[ "$FAIL" -eq 0 ]
