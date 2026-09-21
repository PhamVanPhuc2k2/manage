package payroll

import (
	"math"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
)

// Calculate tính một phiếu lương từ cấu hình, dữ liệu công và tham số.
//
// Hàm này là TRÁI TIM của cả module, và nó cố ý THUẦN KHIẾT: không chạm
// database, không đọc đồng hồ, không ghi log. Mọi thứ nó cần đều đi vào qua
// tham số, và cùng đầu vào luôn cho cùng đầu ra.
//
// Vì sao quan trọng: đây là đoạn code mà kế toán sẽ chất vấn từng dòng khi
// một nhân viên thắc mắc. Nó phải đọc được như một công thức, và phải kiểm
// chứng được bằng một phép thử không cần dựng cả hệ thống.
//
// Thứ tự phép tính theo đúng quy định Việt Nam:
//
//  1. Lương theo công  = lương cơ bản × (ngày công thực tế / ngày công chuẩn)
//  2. Thu nhập gộp     = lương theo công + phụ cấp + thưởng
//  3. Bảo hiểm NV đóng = min(lương đóng BH, trần) × (8% + 1.5% + 1%)
//  4. Thu nhập chịu thuế = phần chịu thuế của thu nhập gộp − bảo hiểm NV đóng
//  5. Thu nhập tính thuế = thu nhập chịu thuế − giảm trừ bản thân
//     − số người phụ thuộc × giảm trừ người phụ thuộc
//  6. Thuế TNCN        = luỹ tiến từng phần trên thu nhập tính thuế
//  7. Thực nhận        = thu nhập gộp − bảo hiểm NV đóng − thuế − khấu trừ khác
func Calculate(
	structure *domainpay.Structure,
	work domainpay.Workdays,
	settings *domainpay.Settings,
) *domainpay.Payslip {
	standard := settings.StandardWorkdays
	if standard <= 0 {
		standard = 22 // phòng dữ liệu hỏng; ràng buộc database đã chặn nhưng đừng chia cho 0
	}

	// Tỷ lệ công. Chặn trên bằng 1: làm thêm ngày không tự động thành lương
	// cao hơn — làm thêm giờ là một khoản riêng, có quy định tính khác, và
	// để nó lẫn vào đây sẽ trả thừa mà không ai nhận ra.
	ratio := work.Present / standard
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}

	slip := &domainpay.Payslip{
		EmployeeID:       structure.EmployeeID,
		StandardWorkdays: standard,
		ActualWorkdays:   work.Present,
		LeaveDays:        work.Leave,
		AbsentDays:       work.Absent,
		Dependents:       structure.Dependents,
		Items:            make([]*domainpay.PayslipItem, 0, len(structure.Components)+1),
	}

	// --- 1. Lương theo công ---
	slip.BaseSalary = domainpay.Money(math.Round(float64(structure.BaseSalary) * ratio))

	slip.Items = append(slip.Items, &domainpay.PayslipItem{
		Kind:    domainpay.KindAllowance,
		Code:    "BASE",
		Name:    "Lương cơ bản theo ngày công",
		Amount:  slip.BaseSalary,
		Taxable: true,
	})

	// --- 2. Phụ cấp, thưởng, khấu trừ ---
	//
	// taxableGross gom riêng phần CHỊU THUẾ. Không gom riêng thì tới bước
	// tính thuế phải duyệt lại danh sách, và rất dễ quên cờ taxable.
	var taxableGross domainpay.Money = slip.BaseSalary

	for _, c := range structure.Components {
		amount := c.Amount
		// Phụ cấp có prorated thì chia theo công; loại cố định trả đủ.
		if c.Prorated {
			amount = domainpay.Money(math.Round(float64(c.Amount) * ratio))
		}
		if amount == 0 {
			continue
		}

		switch c.Kind {
		case domainpay.KindAllowance:
			slip.Allowances += amount
			if c.Taxable {
				taxableGross += amount
			}
		case domainpay.KindBonus:
			slip.Bonuses += amount
			if c.Taxable {
				taxableGross += amount
			}
		case domainpay.KindDeduction:
			slip.OtherDeductions += amount
		}

		slip.Items = append(slip.Items, &domainpay.PayslipItem{
			Kind:    c.Kind,
			Code:    c.Code,
			Name:    c.Name,
			Amount:  amount,
			Taxable: c.Taxable,
		})
	}

	slip.GrossSalary = slip.BaseSalary + slip.Allowances + slip.Bonuses

	// --- 3. Bảo hiểm ---
	//
	// Trần BHXH/BHYT và trần BHTN khác nhau, nên phải tính riêng từng loại
	// chứ không áp một trần chung cho tổng tỷ lệ. Gộp lại sẽ tính sai cho
	// người có lương nằm giữa hai trần.
	base := structure.InsuranceBase()

	socialBase := base.AtMost(settings.SocialCap)
	unemploymentBase := base.AtMost(settings.UnemploymentCap)

	slip.InsuranceBase = socialBase
	slip.InsuranceEmployee =
		socialBase.Apply(settings.SocialRate) +
			socialBase.Apply(settings.HealthRate) +
			unemploymentBase.Apply(settings.UnemploymentRate)

	slip.InsuranceEmployer =
		socialBase.Apply(settings.EmployerSocialRate) +
			socialBase.Apply(settings.EmployerHealthRate) +
			unemploymentBase.Apply(settings.EmployerUnemploymentRate)

	// --- 4, 5. Thu nhập chịu thuế và thu nhập tính thuế ---
	slip.TaxableIncome = (taxableGross - slip.InsuranceEmployee).NonNegative()

	slip.PersonalDeduction = settings.PersonalDeduction
	slip.DependentDeduction =
		settings.DependentDeduction * domainpay.Money(structure.Dependents)

	slip.AssessableIncome =
		(slip.TaxableIncome - slip.PersonalDeduction - slip.DependentDeduction).NonNegative()

	// --- 6. Thuế ---
	slip.IncomeTax = settings.CalculateTax(slip.AssessableIncome)

	// --- 7. Thực nhận ---
	slip.NetSalary = slip.GrossSalary -
		slip.InsuranceEmployee - slip.IncomeTax - slip.OtherDeductions

	return slip
}
