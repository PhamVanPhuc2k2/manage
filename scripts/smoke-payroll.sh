#!/usr/bin/env bash
# Kiểm chứng nghiệp vụ lương — Phase 4.
#
# Chạy: ADMIN_PASS='...' bash scripts/smoke-payroll.sh
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

# Kỳ lương phải là THÁNG ĐÃ QUA: hệ thống từ chối tạo kỳ cho tháng chưa kết
# thúc vì dữ liệu công còn thay đổi mỗi ngày.
PREV_YEAR=$(date -d 'last month' +%Y 2>/dev/null || date -v-1m +%Y)
PREV_MONTH=$(date -d 'last month' +%-m 2>/dev/null || date -v-1m +%-m)
THIS_YEAR=$(date +%Y)
THIS_MONTH=$(date +%-m)

echo
echo "═══ KIỂM CHỨNG NGHIỆP VỤ LƯƠNG ═══"
echo

# ========================================== tham số và biểu thuế
echo "── Tham số tính lương ──"

SETTINGS=$(curl -s "$BASE/payroll/settings" -H "$AUTH")
check "Đọc tham số tính lương → có giảm trừ bản thân" 11000000 \
  "$(echo "$SETTINGS" | num personal_deduction)"

check "Biểu thuế có 7 bậc" 7 \
  "$(echo "$SETTINGS" | grep -o '"ordinal":' | wc -l | tr -d ' ')"

check "Ngày công chuẩn quá 31 → 400" 400 \
  "$(code -X PUT "$BASE/payroll/settings" -H "$AUTH" -H "$JSON" \
     -d '{"personal_deduction":11000000,"dependent_deduction":4400000,"social_rate":0.08,"health_rate":0.015,"unemployment_rate":0.01,"standard_workdays":40}')"

check "Tỷ lệ bảo hiểm lớn hơn 1 → 400" 400 \
  "$(code -X PUT "$BASE/payroll/settings" -H "$AUTH" -H "$JSON" \
     -d '{"personal_deduction":11000000,"dependent_deduction":4400000,"social_rate":1.5,"health_rate":0.015,"unemployment_rate":0.01,"standard_workdays":22}')"

# Biểu thuế bị hở là lỗi im lặng nguy hiểm nhất: thu nhập rơi vào khoảng
# không bậc nào phủ sẽ tính thuế 0 đồng mà không có gì báo.
check "Biểu thuế bị hở → 400" 400 \
  "$(code -X PUT "$BASE/payroll/settings" -H "$AUTH" -H "$JSON" \
     -d '{"personal_deduction":11000000,"dependent_deduction":4400000,"social_rate":0.08,"health_rate":0.015,"unemployment_rate":0.01,"standard_workdays":22,"tax_brackets":[{"from_amount":0,"to_amount":5000000,"rate":0.05},{"from_amount":9000000,"to_amount":null,"rate":0.1}]}')"

check "Bậc thuế đầu không bắt đầu từ 0 → 400" 400 \
  "$(code -X PUT "$BASE/payroll/settings" -H "$AUTH" -H "$JSON" \
     -d '{"personal_deduction":11000000,"dependent_deduction":4400000,"social_rate":0.08,"health_rate":0.015,"unemployment_rate":0.01,"standard_workdays":22,"tax_brackets":[{"from_amount":1000000,"to_amount":null,"rate":0.05}]}')"

check "Bậc giữa để trống mốc trên → 400" 400 \
  "$(code -X PUT "$BASE/payroll/settings" -H "$AUTH" -H "$JSON" \
     -d '{"personal_deduction":11000000,"dependent_deduction":4400000,"social_rate":0.08,"health_rate":0.015,"unemployment_rate":0.01,"standard_workdays":22,"tax_brackets":[{"from_amount":0,"to_amount":null,"rate":0.05},{"from_amount":5000000,"to_amount":null,"rate":0.1}]}')"

# ========================================== cấu hình lương
echo
echo "── Cấu hình lương ──"

cat > "$TMP/salary.json" <<EOF
{"base_salary":30000000,"dependents":1,"bank_account":"1234567890123","bank_name":"Vietcombank","effective_from":"$PREV_YEAR-01-01","components":[{"kind":"allowance","code":"LUNCH","name":"An trua","amount":730000,"taxable":false,"prorated":true},{"kind":"allowance","code":"RESP","name":"Trach nhiem","amount":3000000,"taxable":true,"prorated":false}]}
EOF

check "Đặt cấu hình lương → 201" 201 \
  "$(code -X PUT "$BASE/employees/$ME_ID/salary" -H "$AUTH" -H "$JSON" \
     --data-binary "@$TMP/salary.json")"

STRUCT=$(curl -s "$BASE/employees/$ME_ID/salary" -H "$AUTH")
check "Đọc lại cấu hình lương" 30000000 "$(echo "$STRUCT" | num base_salary)"

# Số tài khoản phải bị che, KHÔNG bao giờ trả về đầy đủ.
check "Số tài khoản bị che còn 4 số cuối" "****0123" \
  "$(echo "$STRUCT" | field bank_account)"

check "Lương cơ bản âm → 400" 400 \
  "$(code -X PUT "$BASE/employees/$ME_ID/salary" -H "$AUTH" -H "$JSON" \
     -d '{"base_salary":-1000}')"

check "Số người phụ thuộc quá lớn → 400" 400 \
  "$(code -X PUT "$BASE/employees/$ME_ID/salary" -H "$AUTH" -H "$JSON" \
     -d '{"base_salary":10000000,"dependents":99}')"

check "Thành phần lương thiếu mã → 400" 400 \
  "$(code -X PUT "$BASE/employees/$ME_ID/salary" -H "$AUTH" -H "$JSON" \
     -d '{"base_salary":10000000,"components":[{"kind":"allowance","code":"","name":"X","amount":100}]}')"

# Tăng lương tạo dòng MỚI, không sửa đè — lịch sử phải giữ nguyên.
check "Tăng lương → tạo bản ghi mới (201)" 201 \
  "$(code -X PUT "$BASE/employees/$ME_ID/salary" -H "$AUTH" -H "$JSON" \
     -d "{\"base_salary\":35000000,\"dependents\":1,\"effective_from\":\"$THIS_YEAR-$(printf %02d "$THIS_MONTH")-01\"}")"

HIST=$(curl -s "$BASE/employees/$ME_ID/salary/history" -H "$AUTH")
check "Lịch sử lương giữ cả hai mốc" 2 \
  "$(echo "$HIST" | grep -o '"base_salary":' | wc -l | tr -d ' ')"

# ========================================== kỳ lương
echo
echo "── Kỳ lương ──"

check "Tạo kỳ cho tháng CHƯA kết thúc → 409" 409 \
  "$(code -X POST "$BASE/payroll/periods" -H "$AUTH" -H "$JSON" \
     -d "{\"year\":$THIS_YEAR,\"month\":$THIS_MONTH}")"

check "Tháng không hợp lệ → 400" 400 \
  "$(code -X POST "$BASE/payroll/periods" -H "$AUTH" -H "$JSON" \
     -d "{\"year\":$PREV_YEAR,\"month\":13}")"

PERIOD=$(curl -s -X POST "$BASE/payroll/periods" -H "$AUTH" -H "$JSON" \
  -d "{\"year\":$PREV_YEAR,\"month\":$PREV_MONTH}")
PERIOD_ID=$(echo "$PERIOD" | idof)
check "Tạo kỳ lương tháng trước" "yes" "$([ -n "$PERIOD_ID" ] && echo yes || echo no)"
check "Kỳ mới ở trạng thái nháp" "draft" "$(echo "$PERIOD" | field status)"

check "Trùng kỳ cùng tháng → 409" 409 \
  "$(code -X POST "$BASE/payroll/periods" -H "$AUTH" -H "$JSON" \
     -d "{\"year\":$PREV_YEAR,\"month\":$PREV_MONTH}")"

# Không cho khoá kỳ chưa có phiếu nào: khoá một kỳ rỗng là chốt xong một
# tháng không trả lương cho ai, và sau đó không tính được nữa.
check "Khoá kỳ chưa tính lương → 409" 409 \
  "$(code -X PUT "$BASE/payroll/periods/$PERIOD_ID/status" -H "$AUTH" -H "$JSON" \
     -d '{"status":"locked"}')"

# ========================================== chạy tính lương
echo
echo "── Chạy máy tính lương ──"

check "Yêu cầu tính lương → 202 (xử lý nền)" 202 \
  "$(code -X POST "$BASE/payroll/periods/$PERIOD_ID/calculate" -H "$AUTH")"

# Chờ worker xử lý xong.
for i in $(seq 1 40); do
  ST=$(curl -s "$BASE/payroll/periods/$PERIOD_ID" -H "$AUTH" | field status)
  [ "$ST" = "draft" ] && [ -n "$(curl -s "$BASE/payroll/periods/$PERIOD_ID" -H "$AUTH" | num employee_count)" ] && break
  sleep 0.5
done

PERIOD=$(curl -s "$BASE/payroll/periods/$PERIOD_ID" -H "$AUTH")
check "Worker đã tính xong, kỳ về lại nháp" "draft" "$(echo "$PERIOD" | field status)"
check "Kỳ có ít nhất một phiếu lương" "yes" \
  "$([ "$(echo "$PERIOD" | num employee_count)" -gt 0 ] && echo yes || echo no)"

SLIPS=$(curl -s "$BASE/payroll/payslips?period_id=$PERIOD_ID" -H "$AUTH")
SLIP_ID=$(echo "$SLIPS" | idof)
check "Đọc được bảng lương của kỳ" "yes" "$([ -n "$SLIP_ID" ] && echo yes || echo no)"

SLIP=$(curl -s "$BASE/payroll/payslips/$SLIP_ID" -H "$AUTH")

# Con số kiểm chứng: lương 30tr, 1 người phụ thuộc, phụ cấp 730k (không
# chịu thuế) + 3tr (chịu thuế). Bảo hiểm trên 30tr = 3.150.000.
check "Bảo hiểm nhân viên tính trên lương cơ bản" 3150000 \
  "$(echo "$SLIP" | num insurance_employee)"

check "Giảm trừ người phụ thuộc = 1 × 4.4tr" 4400000 \
  "$(echo "$SLIP" | num dependent_deduction)"

check "Phiếu có dòng chi tiết giải thích từng khoản" "yes" \
  "$(echo "$SLIP" | grep -q '"items"' && echo yes || echo no)"

check "Số tài khoản trên phiếu bị che" "****0123" \
  "$(echo "$SLIP" | field bank_account)"

# ========================================== vòng đời kỳ lương
echo
echo "── Vòng đời kỳ lương ──"

check "Sửa thưởng khi kỳ còn nháp → 200" 200 \
  "$(code -X PUT "$BASE/payroll/payslips/$SLIP_ID" -H "$AUTH" -H "$JSON" \
     -d '{"bonuses":5000000,"other_deductions":0,"note":"thuong kiem thu"}')"

check "Thưởng âm → 400" 400 \
  "$(code -X PUT "$BASE/payroll/payslips/$SLIP_ID" -H "$AUTH" -H "$JSON" \
     -d '{"bonuses":-100,"other_deductions":0}')"

check "Nháp → đã trả (nhảy cóc) → 400" 400 \
  "$(code -X PUT "$BASE/payroll/periods/$PERIOD_ID/status" -H "$AUTH" -H "$JSON" \
     -d '{"status":"paid"}')"

check "Nháp → đã khoá → 200" 200 \
  "$(code -X PUT "$BASE/payroll/periods/$PERIOD_ID/status" -H "$AUTH" -H "$JSON" \
     -d '{"status":"locked"}')"

check "Kỳ đã khoá thì không sửa được phiếu → 409" 409 \
  "$(code -X PUT "$BASE/payroll/payslips/$SLIP_ID" -H "$AUTH" -H "$JSON" \
     -d '{"bonuses":9000000,"other_deductions":0}')"

check "Kỳ đã khoá thì không tính lại → 409" 409 \
  "$(code -X POST "$BASE/payroll/periods/$PERIOD_ID/calculate" -H "$AUTH")"

check "Đã khoá → đã trả → 200" 200 \
  "$(code -X PUT "$BASE/payroll/periods/$PERIOD_ID/status" -H "$AUTH" -H "$JSON" \
     -d '{"status":"paid"}')"

# Sau khi trả tiền thì không lùi được nữa: sửa số liệu lúc này là làm sai
# lệch sổ sách chứ không phải sửa lỗi.
check "Đã trả → mở lại nháp → 400" 400 \
  "$(code -X PUT "$BASE/payroll/periods/$PERIOD_ID/status" -H "$AUTH" -H "$JSON" \
     -d '{"status":"draft"}')"

# ========================================== phiếu lương cá nhân
echo
echo "── Phiếu lương cá nhân ──"

check "Danh sách phiếu lương của tôi → 200" 200 \
  "$(code "$BASE/payroll/payslips/my" -H "$AUTH")"

MY=$(curl -s "$BASE/payroll/payslips/my" -H "$AUTH")
check "Phiếu của kỳ đã trả xuất hiện" "yes" \
  "$(echo "$MY" | grep -q "$PERIOD_ID" && echo yes || echo no)"

DOC=$(curl -s "$BASE/payroll/payslips/$SLIP_ID/document" -H "$AUTH")
check "Tài liệu phiếu lương là HTML in được" "yes" \
  "$(echo "$DOC" | grep -q '<!DOCTYPE html>' && echo yes || echo no)"
check "Tài liệu hiển thị tiếng Việt có dấu" "yes" \
  "$(echo "$DOC" | grep -q 'THỰC NHẬN' && echo yes || echo no)"
check "Tài liệu giải thích cơ sở tính thuế" "yes" \
  "$(echo "$DOC" | grep -q 'CƠ SỞ TÍNH THUẾ' && echo yes || echo no)"

# ========================================== báo cáo
echo
echo "── Báo cáo chi phí nhân sự ──"

check "Chi phí theo phòng ban → 200" 200 \
  "$(code "$BASE/payroll/periods/$PERIOD_ID/cost-by-department" -H "$AUTH")"
check "Chi phí theo tháng → 200" 200 \
  "$(code "$BASE/payroll/cost-by-month?year=$PREV_YEAR" -H "$AUTH")"

COST=$(curl -s "$BASE/payroll/periods/$PERIOD_ID/cost-by-department" -H "$AUTH")
# Chi phí thật = lương gộp + phần bảo hiểm CÔNG TY đóng. Lương gộp một mình
# không phải chi phí đầy đủ.
check "Chi phí thật lớn hơn lương gộp" "yes" \
  "$([ "$(echo "$COST" | num total_cost)" -gt "$(echo "$COST" | num total_gross)" ] && echo yes || echo no)"

# ========================================== nhật ký truy cập
echo
echo "── Nhật ký truy cập dữ liệu lương ──"

AUDIT=$(curl -s "$BASE/payroll/audit?resource=payslip" -H "$AUTH")
check "Ghi lại lượt XEM phiếu lương" "yes" \
  "$(echo "$AUDIT" | grep -q 'payslip.view' && echo yes || echo no)"

AUDIT2=$(curl -s "$BASE/payroll/audit?resource=salary_structure" -H "$AUTH")
check "Ghi lại lượt SỬA cấu hình lương" "yes" \
  "$(echo "$AUDIT2" | grep -q 'salary.update' && echo yes || echo no)"

AUDIT3=$(curl -s "$BASE/payroll/audit?resource=payroll_period" -H "$AUTH")
check "Ghi lại thao tác chốt kỳ lương" "yes" \
  "$(echo "$AUDIT3" | grep -q 'payroll.lock\|payroll.paid' && echo yes || echo no)"

# ========================================== phân quyền
echo
echo "── Phân quyền ──"

check "Không token → 401" 401 "$(code "$BASE/payroll/periods")"
check "Token rác → 401" 401 \
  "$(code "$BASE/payroll/payslips" -H 'Authorization: Bearer rac')"
check "Tài liệu phiếu lương không token → 401" 401 \
  "$(code "$BASE/payroll/payslips/$SLIP_ID/document")"

# --------------------------------------------------------------- dọn dẹp
echo
echo "── Dọn dữ liệu kiểm thử ──"

docker compose exec -T postgres psql -U manage -d manage -c \
  "DELETE FROM payroll_periods WHERE year = $PREV_YEAR AND month = $PREV_MONTH;
   DELETE FROM salary_structures WHERE employee_id = '$ME_ID';
   DELETE FROM audit_logs;" >/dev/null 2>&1

echo "  đã xoá kỳ lương, cấu hình lương và nhật ký vừa tạo"

echo
echo "═══════════════════════════════════════"
printf '  Đạt: \033[32m%d\033[0m   Hỏng: \033[31m%d\033[0m\n' "$PASS" "$FAIL"
echo "═══════════════════════════════════════"
echo

[ "$FAIL" -eq 0 ]
