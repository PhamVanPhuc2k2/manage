package payroll

import (
	"testing"

	"github.com/google/uuid"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
)

// Máy tính lương là đoạn code mà kế toán sẽ chất vấn từng dòng khi có thắc
// mắc. Nó thuần khiết nên kiểm chứng được bằng số cụ thể, không cần dựng
// database — và những con số dưới đây tính tay đối chiếu được.

func money(v int64) domainpay.Money { return domainpay.Money(v) }

func toPtr(v domainpay.Money) *domainpay.Money { return &v }

// defaultSettings dựng tham số theo quy định hiện hành: giảm trừ 11 triệu,
// 4.4 triệu mỗi người phụ thuộc, biểu thuế 7 bậc.
func defaultSettings() *domainpay.Settings {
	return &domainpay.Settings{
		PersonalDeduction:  money(11_000_000),
		DependentDeduction: money(4_400_000),

		SocialRate:       0.08,
		HealthRate:       0.015,
		UnemploymentRate: 0.01,

		EmployerSocialRate:       0.175,
		EmployerHealthRate:       0.03,
		EmployerUnemploymentRate: 0.01,

		SocialCap:       money(20 * 2_340_000),
		UnemploymentCap: money(20 * 4_960_000),

		StandardWorkdays: 22,

		Brackets: []domainpay.TaxBracket{
			{Ordinal: 1, From: money(0), To: toPtr(money(5_000_000)), Rate: 0.05},
			{Ordinal: 2, From: money(5_000_000), To: toPtr(money(10_000_000)), Rate: 0.10},
			{Ordinal: 3, From: money(10_000_000), To: toPtr(money(18_000_000)), Rate: 0.15},
			{Ordinal: 4, From: money(18_000_000), To: toPtr(money(32_000_000)), Rate: 0.20},
			{Ordinal: 5, From: money(32_000_000), To: toPtr(money(52_000_000)), Rate: 0.25},
			{Ordinal: 6, From: money(52_000_000), To: toPtr(money(80_000_000)), Rate: 0.30},
			{Ordinal: 7, From: money(80_000_000), To: nil, Rate: 0.35},
		},
	}
}

func TestCalculateTax_LuyTienTungPhan(t *testing.T) {
	s := defaultSettings()

	tests := []struct {
		name       string
		assessable int64
		want       int64
	}{
		{"không có thu nhập tính thuế", 0, 0},
		{"thu nhập âm được kẹp về 0", -5_000_000, 0},

		// Trọn bậc 1: 5tr × 5%
		{"đúng hết bậc 1", 5_000_000, 250_000},

		// Nửa bậc 1
		{"giữa bậc 1", 3_000_000, 150_000},

		// 5tr×5% + 5tr×10% = 250.000 + 500.000
		{"đúng hết bậc 2", 10_000_000, 750_000},

		// 250.000 + 500.000 + 2tr×15% = 1.050.000
		//
		// Đây là phép thử quan trọng nhất: cách hiểu SAI phổ biến là lấy
		// 12tr × 15% = 1.800.000. Sai gần gấp đôi, và người bị tính sai sẽ
		// phát hiện ngay.
		{"giữa bậc 3 — luỹ tiến từng phần, không phải toàn phần", 12_000_000, 1_050_000},

		// 250k + 500k + 1.2tr + 2.8tr = 4.750.000
		{"đúng hết bậc 4", 32_000_000, 4_750_000},

		// Bậc cuối không giới hạn trên: 4.75tr + 5tr + 8.4tr + 20tr×35%
		{"vượt bậc cuối", 100_000_000, 25_150_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.CalculateTax(money(tt.assessable))
			if int64(got) != tt.want {
				t.Errorf("thuế = %d, mong %d", got, tt.want)
			}
		})
	}
}

func TestCalculate_DuCong_KhongPhuThuoc(t *testing.T) {
	s := defaultSettings()
	st := &domainpay.Structure{
		EmployeeID: uuid.New(),
		BaseSalary: money(30_000_000),
		Dependents: 0,
	}

	slip := Calculate(st, domainpay.Workdays{Present: 22}, s)

	if int64(slip.GrossSalary) != 30_000_000 {
		t.Fatalf("lương gộp = %d, mong 30.000.000", slip.GrossSalary)
	}

	// Lương 30tr vượt trần BHXH (20 × 2.34tr = 46.8tr)? Không — 30tr < 46.8tr
	// nên đóng trên toàn bộ: 30tr × 10.5% = 3.150.000
	if int64(slip.InsuranceEmployee) != 3_150_000 {
		t.Errorf("bảo hiểm nhân viên = %d, mong 3.150.000", slip.InsuranceEmployee)
	}

	// Thu nhập chịu thuế = 30tr − 3.15tr = 26.85tr
	if int64(slip.TaxableIncome) != 26_850_000 {
		t.Errorf("thu nhập chịu thuế = %d, mong 26.850.000", slip.TaxableIncome)
	}

	// Thu nhập tính thuế = 26.85tr − 11tr = 15.85tr
	if int64(slip.AssessableIncome) != 15_850_000 {
		t.Errorf("thu nhập tính thuế = %d, mong 15.850.000", slip.AssessableIncome)
	}

	// Thuế: 250k + 500k + 5.85tr×15% = 1.627.500
	if int64(slip.IncomeTax) != 1_627_500 {
		t.Errorf("thuế = %d, mong 1.627.500", slip.IncomeTax)
	}

	// Thực nhận = 30tr − 3.15tr − 1.6275tr = 25.222.500
	if int64(slip.NetSalary) != 25_222_500 {
		t.Errorf("thực nhận = %d, mong 25.222.500", slip.NetSalary)
	}
}

func TestCalculate_NguoiPhuThuoc_GiamThue(t *testing.T) {
	s := defaultSettings()
	st := &domainpay.Structure{
		EmployeeID: uuid.New(),
		BaseSalary: money(30_000_000),
		Dependents: 2,
	}

	slip := Calculate(st, domainpay.Workdays{Present: 22}, s)

	// Giảm trừ người phụ thuộc = 2 × 4.4tr = 8.8tr
	if int64(slip.DependentDeduction) != 8_800_000 {
		t.Errorf("giảm trừ người phụ thuộc = %d, mong 8.800.000", slip.DependentDeduction)
	}

	// Thu nhập tính thuế = 26.85tr − 11tr − 8.8tr = 7.05tr
	if int64(slip.AssessableIncome) != 7_050_000 {
		t.Errorf("thu nhập tính thuế = %d, mong 7.050.000", slip.AssessableIncome)
	}

	// Thuế: 250k + 2.05tr×10% = 455.000
	if int64(slip.IncomeTax) != 455_000 {
		t.Errorf("thuế = %d, mong 455.000", slip.IncomeTax)
	}
}

func TestCalculate_ThieuCong_ChiaTheoNgay(t *testing.T) {
	s := defaultSettings()
	st := &domainpay.Structure{
		EmployeeID: uuid.New(),
		BaseSalary: money(22_000_000),
	}

	// Làm 11/22 ngày → đúng một nửa lương cơ bản.
	slip := Calculate(st, domainpay.Workdays{Present: 11, Absent: 11}, s)

	if int64(slip.BaseSalary) != 11_000_000 {
		t.Errorf("lương theo công = %d, mong 11.000.000", slip.BaseSalary)
	}

	// Bảo hiểm KHÔNG chia theo công: đóng trên lương hợp đồng, không phụ
	// thuộc số ngày đi làm. Đây là chỗ rất dễ làm sai.
	if int64(slip.InsuranceEmployee) != 2_310_000 {
		t.Errorf("bảo hiểm = %d, mong 2.310.000 (tính trên lương đầy đủ)",
			slip.InsuranceEmployee)
	}
}

func TestCalculate_LamThemNgay_KhongTuDongTangLuong(t *testing.T) {
	s := defaultSettings()
	st := &domainpay.Structure{
		EmployeeID: uuid.New(),
		BaseSalary: money(22_000_000),
	}

	// Đi làm 26 ngày trên chuẩn 22: tỷ lệ bị chặn ở 1.
	//
	// Làm thêm giờ là khoản riêng có quy định tính khác; để nó lẫn vào đây
	// sẽ trả thừa mà không ai nhận ra.
	slip := Calculate(st, domainpay.Workdays{Present: 26}, s)

	if int64(slip.BaseSalary) != 22_000_000 {
		t.Errorf("lương theo công = %d, mong 22.000.000 (không vượt lương cơ bản)",
			slip.BaseSalary)
	}
}

func TestCalculate_PhuCap_ChiuThueVaKhongChiuThue(t *testing.T) {
	s := defaultSettings()
	st := &domainpay.Structure{
		EmployeeID: uuid.New(),
		BaseSalary: money(20_000_000),
		Components: []*domainpay.Component{
			{Kind: domainpay.KindAllowance, Code: "LUNCH", Name: "Ăn trưa",
				Amount: money(730_000), Taxable: false, Prorated: false},
			{Kind: domainpay.KindAllowance, Code: "RESP", Name: "Trách nhiệm",
				Amount: money(3_000_000), Taxable: true, Prorated: false},
			{Kind: domainpay.KindDeduction, Code: "UNION", Name: "Phí công đoàn",
				Amount: money(100_000)},
		},
	}

	slip := Calculate(st, domainpay.Workdays{Present: 22}, s)

	// Gộp = 20tr + 730k + 3tr = 23.730.000
	if int64(slip.GrossSalary) != 23_730_000 {
		t.Errorf("lương gộp = %d, mong 23.730.000", slip.GrossSalary)
	}

	// Bảo hiểm tính trên lương cơ bản (20tr), không tính trên phụ cấp:
	// 20tr × 10.5% = 2.100.000
	if int64(slip.InsuranceEmployee) != 2_100_000 {
		t.Errorf("bảo hiểm = %d, mong 2.100.000", slip.InsuranceEmployee)
	}

	// Thu nhập chịu thuế chỉ gồm phần CHỊU THUẾ: 20tr + 3tr − 2.1tr = 20.9tr.
	// Phụ cấp ăn trưa 730k nằm ngoài.
	if int64(slip.TaxableIncome) != 20_900_000 {
		t.Errorf("thu nhập chịu thuế = %d, mong 20.900.000 (không gồm ăn trưa)",
			slip.TaxableIncome)
	}

	// Khấu trừ khác trừ vào thực nhận nhưng KHÔNG giảm thu nhập chịu thuế.
	if int64(slip.OtherDeductions) != 100_000 {
		t.Errorf("khấu trừ khác = %d, mong 100.000", slip.OtherDeductions)
	}

	want := int64(23_730_000 - 2_100_000 - int64(slip.IncomeTax) - 100_000)
	if int64(slip.NetSalary) != want {
		t.Errorf("thực nhận = %d, mong %d", slip.NetSalary, want)
	}
}

func TestCalculate_PhuCapChiaTheoCong(t *testing.T) {
	s := defaultSettings()
	st := &domainpay.Structure{
		EmployeeID: uuid.New(),
		BaseSalary: money(22_000_000),
		Components: []*domainpay.Component{
			// Ăn trưa theo ngày đi làm.
			{Kind: domainpay.KindAllowance, Code: "LUNCH", Name: "Ăn trưa",
				Amount: money(660_000), Taxable: false, Prorated: true},
			// Điện thoại trả đủ dù nghỉ.
			{Kind: domainpay.KindAllowance, Code: "PHONE", Name: "Điện thoại",
				Amount: money(500_000), Taxable: false, Prorated: false},
		},
	}

	slip := Calculate(st, domainpay.Workdays{Present: 11, Absent: 11}, s)

	// Ăn trưa chia đôi (330k), điện thoại giữ nguyên (500k).
	if int64(slip.Allowances) != 830_000 {
		t.Errorf("phụ cấp = %d, mong 830.000 (ăn trưa chia đôi, điện thoại đủ)",
			slip.Allowances)
	}
}

func TestCalculate_VuotTranBaoHiem(t *testing.T) {
	s := defaultSettings()
	st := &domainpay.Structure{
		EmployeeID: uuid.New(),
		// 200tr vượt cả trần BHXH (46.8tr) lẫn trần BHTN (99.2tr).
		BaseSalary: money(200_000_000),
	}

	slip := Calculate(st, domainpay.Workdays{Present: 22}, s)

	// BHXH + BHYT trên trần 46.8tr: 46.8tr × 9.5% = 4.446.000
	// BHTN trên trần 99.2tr:        99.2tr × 1%   =   992.000
	// Tổng = 5.438.000
	//
	// Hai trần KHÁC NHAU nên phải tính riêng. Áp một trần chung cho tổng tỷ
	// lệ 10.5% sẽ ra 4.914.000 — sai với mọi người có lương nằm giữa hai trần.
	if int64(slip.InsuranceEmployee) != 5_438_000 {
		t.Errorf("bảo hiểm = %d, mong 5.438.000 (hai trần tính riêng)",
			slip.InsuranceEmployee)
	}
}

func TestCalculate_LuongDongBaoHiemKhacLuongCoBan(t *testing.T) {
	s := defaultSettings()
	insurance := money(10_000_000)
	st := &domainpay.Structure{
		EmployeeID:      uuid.New(),
		BaseSalary:      money(30_000_000),
		InsuranceSalary: &insurance,
	}

	slip := Calculate(st, domainpay.Workdays{Present: 22}, s)

	// Đóng trên 10tr chứ không phải 30tr: 10tr × 10.5% = 1.050.000
	if int64(slip.InsuranceEmployee) != 1_050_000 {
		t.Errorf("bảo hiểm = %d, mong 1.050.000 (theo lương đóng BH)",
			slip.InsuranceEmployee)
	}
	// Lương gộp vẫn là 30tr.
	if int64(slip.GrossSalary) != 30_000_000 {
		t.Errorf("lương gộp = %d, mong 30.000.000", slip.GrossSalary)
	}
}

func TestCalculate_LuongThap_KhongPhaiNopThue(t *testing.T) {
	s := defaultSettings()
	st := &domainpay.Structure{
		EmployeeID: uuid.New(),
		BaseSalary: money(8_000_000),
	}

	slip := Calculate(st, domainpay.Workdays{Present: 22}, s)

	// 8tr − 840k bảo hiểm = 7.16tr, dưới mức giảm trừ 11tr → không phải nộp.
	if int64(slip.AssessableIncome) != 0 {
		t.Errorf("thu nhập tính thuế = %d, mong 0", slip.AssessableIncome)
	}
	if int64(slip.IncomeTax) != 0 {
		t.Errorf("thuế = %d, mong 0", slip.IncomeTax)
	}
}

func TestCalculate_NghiCaThang(t *testing.T) {
	s := defaultSettings()
	st := &domainpay.Structure{
		EmployeeID: uuid.New(),
		BaseSalary: money(22_000_000),
	}

	slip := Calculate(st, domainpay.Workdays{Present: 0, Absent: 22}, s)

	if int64(slip.BaseSalary) != 0 {
		t.Errorf("lương theo công = %d, mong 0", slip.BaseSalary)
	}
	// Thực nhận âm: bảo hiểm vẫn phải đóng dù không đi làm ngày nào.
	//
	// KHÔNG kẹp về 0 — con số âm là sự thật cần nhìn thấy, và kế toán phải
	// tự quyết cách xử lý. Che đi bằng cách kẹp về 0 sẽ làm mất một khoản
	// công ty đã ứng ra.
	if slip.NetSalary >= 0 {
		t.Errorf("thực nhận = %d, mong âm (vẫn phải đóng bảo hiểm)", slip.NetSalary)
	}
}
