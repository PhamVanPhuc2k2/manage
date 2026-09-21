package payroll

import (
	"fmt"
	"html"
	"strings"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
)

// FormatVND in số tiền theo cách người Việt đọc: 1.234.567 ₫
//
// Dùng dấu chấm phân cách nghìn, không dùng dấu phẩy. Trộn hai quy ước trên
// cùng một tài liệu là cách chắc chắn khiến người đọc nghi ngờ con số.
func FormatVND(m domainpay.Money) string {
	n := int64(m)
	neg := n < 0
	if neg {
		n = -n
	}

	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}

	out := b.String() + " ₫"
	if neg {
		return "-" + out
	}
	return out
}

// RenderPayslipHTML dựng phiếu lương thành một tài liệu HTML hoàn chỉnh.
//
// Vì sao HTML chứ không phải PDF sinh ở server:
//
// Sinh PDF có tiếng Việt cần NHÚNG một font TTF hỗ trợ Latin Extended
// Additional (các ký tự ạ ả ấ ầ...). Bộ font lõi của PDF chỉ có Latin-1,
// và mọi dấu tiếng Việt sẽ biến thành ô vuông — một phiếu lương không đọc
// được thì tệ hơn là không có. Thêm một tệp font vào repo là quyết định về
// giấy phép và dung lượng, nên nó phải là lựa chọn có ý thức của chủ dự án,
// không phải thứ lẳng lặng kéo vào.
//
// Tài liệu này có sẵn CSS cho in ấn, nên "In / Lưu thành PDF" của trình
// duyệt cho ra PDF đúng như nhìn thấy. Cùng một HTML dùng luôn làm nội dung
// email — người nhận đọc được ngay trong hộp thư, không phải tải tệp về.
func RenderPayslipHTML(s *domainpay.Payslip, p *domainpay.Period) string {
	var b strings.Builder

	esc := html.EscapeString

	b.WriteString(`<!DOCTYPE html>
<html lang="vi">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Phiếu lương ` + esc(s.EmployeeName) + ` – ` + esc(p.Name) + `</title>
<style>
  :root { color-scheme: light; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto,
                 "Helvetica Neue", Arial, sans-serif;
    color: #111; background: #f5f5f5; margin: 0; padding: 24px;
    line-height: 1.5;
  }
  .sheet {
    max-width: 720px; margin: 0 auto; background: #fff;
    padding: 32px; border-radius: 8px;
    box-shadow: 0 1px 3px rgba(0,0,0,.12);
  }
  h1 { font-size: 20px; margin: 0 0 4px; }
  .muted { color: #666; font-size: 13px; }
  table { width: 100%; border-collapse: collapse; margin-top: 16px; }
  th, td { padding: 8px 0; text-align: left; font-size: 14px; }
  td.num { text-align: right; font-variant-numeric: tabular-nums; }
  tr.section td { padding-top: 18px; font-weight: 600; border-bottom: 1px solid #ddd; }
  tr.total td { border-top: 2px solid #111; font-weight: 700; font-size: 16px; padding-top: 12px; }
  tr.sub td { color: #555; padding-left: 12px; }
  .grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 4px 16px; margin-top: 16px; }
  .grid div { font-size: 13px; }
  .grid .label { color: #666; }
  footer { margin-top: 24px; font-size: 12px; color: #777; }

  /* Khi in: bỏ nền xám và đổ bóng để không tốn mực, và ép một trang. */
  @media print {
    body { background: #fff; padding: 0; }
    .sheet { box-shadow: none; padding: 0; max-width: none; }
    .no-print { display: none; }
  }
</style>
</head>
<body>
<div class="sheet">
`)

	b.WriteString(`<h1>` + esc(p.Name) + `</h1>`)
	b.WriteString(`<div class="muted">Kỳ ` +
		fmt.Sprintf("%02d/%d", p.Month, p.Year) + `</div>`)

	b.WriteString(`<div class="grid">`)
	row := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(`<div class="label">` + esc(label) + `</div><div>` + esc(value) + `</div>`)
	}
	row("Họ tên", s.EmployeeName)
	row("Mã nhân viên", s.EmployeeCode)
	row("Phòng ban", s.DepartmentName)
	row("Chức vụ", s.PositionName)
	row("Ngày công", fmt.Sprintf("%.1f / %.1f", s.ActualWorkdays, s.StandardWorkdays))
	if s.LeaveDays > 0 {
		row("Ngày nghỉ phép", fmt.Sprintf("%.1f", s.LeaveDays))
	}
	if s.Dependents > 0 {
		row("Người phụ thuộc", fmt.Sprintf("%d", s.Dependents))
	}
	if s.BankAccountMasked != "" {
		row("Tài khoản", s.BankAccountMasked+" · "+s.BankName)
	}
	b.WriteString(`</div>`)

	b.WriteString(`<table>`)

	line := func(class, label, value string) {
		b.WriteString(`<tr class="` + class + `"><td>` + esc(label) +
			`</td><td class="num">` + esc(value) + `</td></tr>`)
	}

	// --- Thu nhập ---
	line("section", "THU NHẬP", "")
	for _, it := range s.Items {
		if it.Kind == domainpay.KindDeduction {
			continue
		}
		label := it.Name
		if !it.Taxable {
			label += " (không chịu thuế)"
		}
		line("sub", label, FormatVND(it.Amount))
	}
	line("", "Tổng thu nhập", FormatVND(s.GrossSalary))

	// --- Khấu trừ ---
	line("section", "KHẤU TRỪ", "")
	line("sub",
		"Bảo hiểm bắt buộc (trên "+FormatVND(s.InsuranceBase)+")",
		FormatVND(s.InsuranceEmployee))
	line("sub", "Thuế thu nhập cá nhân", FormatVND(s.IncomeTax))
	for _, it := range s.Items {
		if it.Kind == domainpay.KindDeduction {
			line("sub", it.Name, FormatVND(it.Amount))
		}
	}

	// --- Cơ sở tính thuế ---
	//
	// Hiển thị đầy đủ để phiếu lương TỰ GIẢI THÍCH được. Chỉ đưa con số
	// thuế cuối cùng thì mọi thắc mắc đều phải hỏi kế toán, và kế toán sẽ
	// phải mở lại từng phiếu để trả lời cùng một câu hỏi.
	line("section", "CƠ SỞ TÍNH THUẾ", "")
	line("sub", "Thu nhập chịu thuế", FormatVND(s.TaxableIncome))
	line("sub", "Giảm trừ bản thân", "-"+FormatVND(s.PersonalDeduction))
	if s.DependentDeduction > 0 {
		line("sub",
			fmt.Sprintf("Giảm trừ %d người phụ thuộc", s.Dependents),
			"-"+FormatVND(s.DependentDeduction))
	}
	line("sub", "Thu nhập tính thuế", FormatVND(s.AssessableIncome))

	line("total", "THỰC NHẬN", FormatVND(s.NetSalary))
	b.WriteString(`</table>`)

	if s.Note != "" {
		b.WriteString(`<p class="muted">Ghi chú: ` + esc(s.Note) + `</p>`)
	}

	b.WriteString(`<footer>
Phiếu lương này do hệ thống sinh tự động. Thấy số liệu chưa đúng, liên hệ bộ
phận nhân sự trước ngày 10 của tháng kế tiếp.
</footer>`)

	b.WriteString(`
  <p class="no-print" style="margin-top:20px">
    <button onclick="window.print()"
      style="padding:8px 16px;font-size:14px;cursor:pointer">
      In / Lưu thành PDF
    </button>
  </p>
</div>
</body>
</html>`)

	return b.String()
}
