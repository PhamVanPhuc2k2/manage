package payroll

import (
	"bytes"
	"os"
	"strings"
	"testing"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
)

// samplePayslip dùng nhiều dấu chồng nhất có thể (ầ ẫ ặ ỗ ữ ự) — đúng những
// ký tự mà font lõi của PDF vẽ thành ô vuông.
func samplePayslip() (*domainpay.Payslip, *domainpay.Period) {
	s := &domainpay.Payslip{
		EmployeeName:       "Nguyễn Thị Ngọc Hường",
		EmployeeCode:       "NV/042",
		DepartmentName:     "Phòng Kế toán – Tổng hợp",
		PositionName:       "Kế toán trưởng",
		StandardWorkdays:   22,
		ActualWorkdays:     21.5,
		LeaveDays:          0.5,
		Dependents:         2,
		BankAccountMasked:  "•••• 6789",
		BankName:           "Vietcombank",
		GrossSalary:        32_500_000,
		InsuranceBase:      25_000_000,
		InsuranceEmployee:  2_625_000,
		TaxableIncome:      31_770_000,
		PersonalDeduction:  11_000_000,
		DependentDeduction: 8_800_000,
		AssessableIncome:   9_345_000,
		IncomeTax:          684_500,
		NetSalary:          28_190_500,
		Note:               "Đã cộng thưởng dự án quý ba và trừ tạm ứng hôm mười lăm. Mọi thắc mắc xin gửi phòng nhân sự trước ngày mười của tháng sau, kèm ảnh chụp bảng công nếu có chênh lệch.",
		Items: []*domainpay.PayslipItem{
			{Kind: domainpay.KindAllowance, Name: "Lương cơ bản", Amount: 25_000_000, Taxable: true},
			{Kind: domainpay.KindAllowance, Name: "Phụ cấp ăn trưa", Amount: 730_000},
			{Kind: domainpay.KindAllowance, Name: "Phụ cấp điện thoại", Amount: 500_000, Taxable: true},
			{Kind: domainpay.KindBonus, Name: "Thưởng dự án quý III", Amount: 6_270_000, Taxable: true},
			{Kind: domainpay.KindDeduction, Name: "Trừ tạm ứng", Amount: 1_000_000},
		},
	}
	p := &domainpay.Period{Year: 2026, Month: 9, Name: "Lương tháng 9/2026"}
	return s, p
}

func TestRenderPayslipPDF(t *testing.T) {
	s, p := samplePayslip()
	data, err := RenderPayslipPDF(s, p)
	if err != nil {
		t.Fatalf("RenderPayslipPDF: %v", err)
	}

	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatal("không phải tệp PDF")
	}
	// Font phải được NHÚNG — không nhúng thì trình đọc PDF thay bằng font
	// mặc định và dấu tiếng Việt thành ô vuông trên máy người nhận.
	if !bytes.Contains(data, []byte("FontFile2")) {
		t.Error("PDF không nhúng font TrueType")
	}
	// Chỉ nhúng phần font thực sự dùng: hai tệp font hơn 260 KB, một phiếu
	// lương phải nhỏ hơn nhiều.
	if len(data) > 120_000 {
		t.Errorf("PDF nặng %d byte — font không được cắt gọn?", len(data))
	}

	// Xem bằng mắt: PDF_OUT=/đường/dẫn.pdf go test -run RenderPayslipPDF
	if out := os.Getenv("PDF_OUT"); out != "" {
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Nhiều khoản mục tới mức tràn trang: phải sang trang mới chứ không vẽ
// chữ ra ngoài mép giấy.
func TestRenderPayslipPDFOverflowsToSecondPage(t *testing.T) {
	s, p := samplePayslip()
	for i := 0; i < 60; i++ {
		s.Items = append(s.Items, &domainpay.PayslipItem{
			Kind: domainpay.KindAllowance, Name: "Phụ cấp " + strings.Repeat("x", i%5), Amount: 100_000,
		})
	}
	data, err := RenderPayslipPDF(s, p)
	if err != nil {
		t.Fatal(err)
	}
	// "/Type /Page" khớp cả "/Type /Pages" (nút gốc của cây trang) — trừ đi.
	pages := bytes.Count(data, []byte("/Type /Page")) - bytes.Count(data, []byte("/Type /Pages"))
	if pages < 2 {
		t.Errorf("60 khoản mục phải tràn sang trang hai, được %d trang", pages)
	}
}
