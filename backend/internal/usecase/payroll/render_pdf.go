package payroll

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/signintech/gopdf"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
)

// Font nhúng vào binary.
//
// Be Vietnam Pro, giấy phép SIL Open Font License 1.1 (fonts/OFL.txt) —
// được nhúng và phân phối kèm phần mềm, kể cả phần mềm thương mại, miễn
// là giữ nguyên tệp giấy phép. Chọn font này vì nó được thiết kế cho tiếng
// Việt: dấu chồng (ầ, ẫ, ặ) đặt đúng chỗ, không đè lên dòng trên.
//
// Bộ font lõi của PDF chỉ có Latin-1 — không nhúng font thì mọi dấu tiếng
// Việt thành ô vuông. gopdf chỉ nhúng những ký tự thực sự dùng (subset),
// nên một phiếu lương chỉ nặng vài chục KB dù tệp font hơn 100 KB.
var (
	//go:embed fonts/BeVietnamPro-Regular.ttf
	fontRegular []byte
	//go:embed fonts/BeVietnamPro-SemiBold.ttf
	fontBold []byte
)

const (
	pdfRegular = "regular"
	pdfBold    = "bold"

	pageW   = 595.28 // A4, đơn vị point
	pageH   = 841.89
	marginX = 56.0
	marginY = 56.0
	right   = pageW - marginX
)

// RenderPayslipPDF dựng phiếu lương thành PDF, cùng nội dung và thứ tự với
// RenderPayslipHTML. Sửa bố cục ở một bên thì sửa cả bên kia — phiếu lương
// tải về và phiếu lương trong email phải nói cùng một điều.
func RenderPayslipPDF(s *domainpay.Payslip, p *domainpay.Period) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.SetInfo(gopdf.PdfInfo{
		Title:   "Phiếu lương " + s.EmployeeName + " – " + p.Name,
		Subject: fmt.Sprintf("Kỳ %02d/%d", p.Month, p.Year),
		Creator: "manage",
	})
	if err := pdf.AddTTFFontData(pdfRegular, fontRegular); err != nil {
		return nil, fmt.Errorf("nạp font thường: %w", err)
	}
	if err := pdf.AddTTFFontData(pdfBold, fontBold); err != nil {
		return nil, fmt.Errorf("nạp font đậm: %w", err)
	}
	pdf.AddPage()

	w := &pdfWriter{pdf: pdf, y: marginY}

	// --- Tiêu đề ---
	w.text(marginX, pdfBold, 18, 0x11, p.Name)
	w.y += 24
	w.text(marginX, pdfRegular, 10, 0x66, fmt.Sprintf("Kỳ %02d/%d", p.Month, p.Year))
	w.y += 26

	// --- Thông tin nhân viên: hai cột nhãn / giá trị ---
	type kv struct{ k, v string }
	info := []kv{
		{"Họ tên", s.EmployeeName},
		{"Mã nhân viên", s.EmployeeCode},
		{"Phòng ban", s.DepartmentName},
		{"Chức vụ", s.PositionName},
		{"Ngày công", fmt.Sprintf("%.1f / %.1f", s.ActualWorkdays, s.StandardWorkdays)},
	}
	if s.LeaveDays > 0 {
		info = append(info, kv{"Ngày nghỉ phép", fmt.Sprintf("%.1f", s.LeaveDays)})
	}
	if s.Dependents > 0 {
		info = append(info, kv{"Người phụ thuộc", fmt.Sprintf("%d", s.Dependents)})
	}
	if s.BankAccountMasked != "" {
		info = append(info, kv{"Tài khoản", s.BankAccountMasked + " · " + s.BankName})
	}
	col := (right - marginX) / 2
	shown := 0
	for _, it := range info {
		if it.v == "" {
			continue
		}
		x := marginX + float64(shown%2)*col
		w.text(x, pdfRegular, 9.5, 0x66, it.k)
		w.text(x+90, pdfRegular, 9.5, 0x11, it.v)
		if shown%2 == 1 {
			w.y += 16
		}
		shown++
	}
	if shown%2 == 1 {
		w.y += 16
	}
	w.y += 12

	// --- Thu nhập ---
	w.section("THU NHẬP")
	for _, it := range s.Items {
		if it.Kind == domainpay.KindDeduction {
			continue
		}
		label := it.Name
		if !it.Taxable {
			label += " (không chịu thuế)"
		}
		w.sub(label, FormatVND(it.Amount))
	}
	w.row("Tổng thu nhập", FormatVND(s.GrossSalary))

	// --- Khấu trừ ---
	w.section("KHẤU TRỪ")
	w.sub("Bảo hiểm bắt buộc (trên "+FormatVND(s.InsuranceBase)+")", FormatVND(s.InsuranceEmployee))
	w.sub("Thuế thu nhập cá nhân", FormatVND(s.IncomeTax))
	for _, it := range s.Items {
		if it.Kind == domainpay.KindDeduction {
			w.sub(it.Name, FormatVND(it.Amount))
		}
	}

	// --- Cơ sở tính thuế: để phiếu lương tự giải thích được ---
	w.section("CƠ SỞ TÍNH THUẾ")
	w.sub("Thu nhập chịu thuế", FormatVND(s.TaxableIncome))
	w.sub("Giảm trừ bản thân", "-"+FormatVND(s.PersonalDeduction))
	if s.DependentDeduction > 0 {
		w.sub(fmt.Sprintf("Giảm trừ %d người phụ thuộc", s.Dependents), "-"+FormatVND(s.DependentDeduction))
	}
	w.sub("Thu nhập tính thuế", FormatVND(s.AssessableIncome))

	// --- Thực nhận ---
	w.y += 10
	w.need(40)
	pdf.SetLineWidth(1.5)
	pdf.SetStrokeColor(0x11, 0x11, 0x11)
	pdf.Line(marginX, w.y, right, w.y)
	// Chữ hoa có dấu (Ự, Ậ) cao hơn chữ thường: chừa đủ chỗ để dấu không
	// chạm vào đường kẻ đậm phía trên.
	w.y += 16
	w.text(marginX, pdfBold, 13, 0x11, "THỰC NHẬN")
	w.rightText(pdfBold, 13, 0x11, FormatVND(s.NetSalary))
	w.y += 28

	if s.Note != "" {
		w.paragraph(pdfRegular, 9.5, 0x55, "Ghi chú: "+s.Note)
		w.y += 8
	}
	w.paragraph(pdfRegular, 8.5, 0x77,
		"Phiếu lương này do hệ thống sinh tự động. Thấy số liệu chưa đúng, liên hệ bộ phận nhân sự trước ngày 10 của tháng kế tiếp.")

	if w.err != nil {
		return nil, w.err
	}
	return pdf.GetBytesPdf(), nil
}

// pdfWriter giữ vị trí dòng hiện tại và lỗi đầu tiên gặp phải, để phần dựng
// phiếu đọc như một danh sách dòng thay vì một chuỗi if err != nil.
type pdfWriter struct {
	pdf *gopdf.GoPdf
	y   float64
	err error
}

func (w *pdfWriter) font(family string, size float64, gray uint8) {
	if w.err != nil {
		return
	}
	w.err = w.pdf.SetFont(family, "", size)
	w.pdf.SetTextColor(gray, gray, gray)
}

func (w *pdfWriter) text(x float64, family string, size float64, gray uint8, s string) {
	w.font(family, size, gray)
	if w.err != nil {
		return
	}
	w.pdf.SetXY(x, w.y)
	w.err = w.pdf.Text(s)
}

// rightText căn phải sát lề — cột số tiền phải thẳng hàng theo hàng đơn vị.
func (w *pdfWriter) rightText(family string, size float64, gray uint8, s string) {
	w.font(family, size, gray)
	if w.err != nil {
		return
	}
	width, err := w.pdf.MeasureTextWidth(s)
	if err != nil {
		w.err = err
		return
	}
	w.pdf.SetXY(right-width, w.y)
	w.err = w.pdf.Text(s)
}

// need sang trang mới khi phần còn lại không đủ chỗ.
func (w *pdfWriter) need(h float64) {
	if w.y+h > pageH-marginY {
		w.pdf.AddPage()
		w.y = marginY
	}
}

func (w *pdfWriter) section(title string) {
	w.y += 14
	w.need(40)
	w.text(marginX, pdfBold, 10.5, 0x11, title)
	w.y += 6
	w.pdf.SetLineWidth(0.5)
	w.pdf.SetStrokeColor(0xdd, 0xdd, 0xdd)
	w.pdf.Line(marginX, w.y, right, w.y)
	w.y += 16
}

func (w *pdfWriter) sub(label, value string) {
	w.need(18)
	w.text(marginX+12, pdfRegular, 10, 0x55, label)
	w.rightText(pdfRegular, 10, 0x33, value)
	w.y += 18
}

func (w *pdfWriter) row(label, value string) {
	w.need(18)
	w.text(marginX, pdfBold, 10, 0x11, label)
	w.rightText(pdfBold, 10, 0x11, value)
	w.y += 18
}

// paragraph ngắt dòng theo bề rộng trang.
func (w *pdfWriter) paragraph(family string, size float64, gray uint8, s string) {
	w.font(family, size, gray)
	if w.err != nil {
		return
	}
	lines, err := w.pdf.SplitText(strings.TrimSpace(s), right-marginX)
	if err != nil {
		w.err = err
		return
	}
	for _, l := range lines {
		// SplitText để lại dấu cách ở đầu dòng sau chỗ ngắt.
		l = strings.TrimSpace(l)
		w.need(size + 4)
		w.text(marginX, family, size, gray, l)
		w.y += size + 4
	}
}
