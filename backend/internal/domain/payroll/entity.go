// Package payroll chứa entity và port của module lương.
package payroll

import (
	"errors"
	"math"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("không tìm thấy")
	ErrLocked   = errors.New("kỳ lương đã khoá")
)

// Money là số tiền, đơn vị ĐỒNG.
//
// Là int64 chứ không phải float64. Lương Việt Nam không có đơn vị nhỏ hơn
// đồng, và số thực tích luỹ sai số: cộng vài chục khoản phụ cấp rồi nhân tỷ
// lệ thuế sẽ cho ra con số lệch vài đồng. Bảng lương lệch một đồng là bảng
// lương sai — kế toán sẽ trả lại.
//
// Là kiểu RIÊNG chứ không phải int64 trần để trình biên dịch bắt được việc
// trộn nhầm tiền với số lượng (số ngày công, số người phụ thuộc).
type Money int64

// Apply nhân tiền với một tỷ lệ rồi làm tròn về đồng.
//
// Làm tròn NỬA LÊN, khớp với thông lệ kế toán Việt Nam. Cắt cụt (int64
// conversion) sẽ luôn có lợi cho công ty một cách hệ thống — với hàng nghìn
// phiếu lương mỗi tháng, đó không còn là sai số ngẫu nhiên.
func (m Money) Apply(rate float64) Money {
	return Money(math.Round(float64(m) * rate))
}

// AtMost giới hạn trên. Dùng cho trần đóng bảo hiểm.
func (m Money) AtMost(cap Money) Money {
	if cap > 0 && m > cap {
		return cap
	}
	return m
}

// NonNegative kẹp về 0. Thu nhập tính thuế âm nghĩa là được miễn, không
// phải được hoàn tiền.
func (m Money) NonNegative() Money {
	if m < 0 {
		return 0
	}
	return m
}

// =========================================================================
// THAM SỐ TÍNH LƯƠNG
// =========================================================================

type Settings struct {
	ID        uuid.UUID
	CompanyID uuid.UUID

	PersonalDeduction  Money
	DependentDeduction Money

	SocialRate       float64
	HealthRate       float64
	UnemploymentRate float64

	EmployerSocialRate       float64
	EmployerHealthRate       float64
	EmployerUnemploymentRate float64

	SocialCap       Money
	UnemploymentCap Money

	StandardWorkdays float64

	EffectiveFrom time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time

	// Brackets nạp kèm lúc đọc. Biểu thuế không có nghĩa nếu tách khỏi tham
	// số sinh ra nó, nên hai thứ luôn đi cùng nhau.
	Brackets []TaxBracket
}

// EmployeeInsuranceRate là tổng tỷ lệ bảo hiểm nhân viên phải đóng.
func (s *Settings) EmployeeInsuranceRate() float64 {
	return s.SocialRate + s.HealthRate + s.UnemploymentRate
}

// TaxBracket là một bậc của biểu thuế luỹ tiến từng phần.
type TaxBracket struct {
	ID      uuid.UUID
	Ordinal int
	From    Money
	To      *Money // nil = bậc cuối, không giới hạn
	Rate    float64
}

// CalculateTax tính thuế thu nhập cá nhân theo biểu luỹ tiến TỪNG PHẦN.
//
// Từng phần nghĩa là mỗi bậc chỉ áp tỷ lệ của nó cho PHẦN thu nhập nằm
// trong bậc đó. Áp tỷ lệ của bậc cao nhất cho toàn bộ thu nhập là cách hiểu
// sai phổ biến, và nó cho ra con số cao hơn nhiều — người bị tính sai sẽ
// phát hiện ngay và mất niềm tin vào cả hệ thống.
//
// Ví dụ thu nhập tính thuế 12 triệu với biểu 3 bậc đầu:
//
//	5 triệu đầu       × 5%  = 250.000
//	5 triệu tiếp theo × 10% = 500.000
//	2 triệu còn lại   × 15% = 300.000
//	                  tổng  = 1.050.000
//
// chứ KHÔNG phải 12 triệu × 15% = 1.800.000.
func (s *Settings) CalculateTax(assessable Money) Money {
	if assessable <= 0 || len(s.Brackets) == 0 {
		return 0
	}

	var tax Money
	for _, b := range s.Brackets {
		if assessable <= b.From {
			break // chưa chạm tới bậc này
		}

		upper := assessable
		if b.To != nil && *b.To < upper {
			upper = *b.To
		}

		portion := upper - b.From
		if portion > 0 {
			tax += portion.Apply(b.Rate)
		}
	}
	return tax
}

// =========================================================================
// CẤU HÌNH LƯƠNG THEO NHÂN VIÊN
// =========================================================================

type Structure struct {
	ID         uuid.UUID
	EmployeeID uuid.UUID

	BaseSalary      Money
	InsuranceSalary *Money
	Dependents      int

	BankAccount string
	BankName    string

	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	Note          string

	CreatedBy *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time

	// JOIN hoặc nạp kèm lúc đọc.
	EmployeeName   string
	EmployeeCode   string
	DepartmentName string
	Components     []*Component
}

// InsuranceBase là mức lương dùng để tính bảo hiểm, chưa áp trần.
func (s *Structure) InsuranceBase() Money {
	if s.InsuranceSalary != nil {
		return *s.InsuranceSalary
	}
	return s.BaseSalary
}

type ComponentKind string

const (
	KindAllowance ComponentKind = "allowance"
	KindBonus     ComponentKind = "bonus"
	KindDeduction ComponentKind = "deduction"
)

func (k ComponentKind) Valid() bool {
	switch k {
	case KindAllowance, KindBonus, KindDeduction:
		return true
	}
	return false
}

type Component struct {
	ID          uuid.UUID
	StructureID uuid.UUID
	Kind        ComponentKind
	Code        string
	Name        string
	Amount      Money
	Taxable     bool
	Prorated    bool
	CreatedAt   time.Time
}

// =========================================================================
// KỲ LƯƠNG
// =========================================================================

type Status string

const (
	StatusDraft       Status = "draft"
	StatusCalculating Status = "calculating"
	StatusLocked      Status = "locked"
	StatusPaid        Status = "paid"
	StatusCancelled   Status = "cancelled"
)

func (s Status) Valid() bool {
	switch s {
	case StatusDraft, StatusCalculating, StatusLocked, StatusPaid, StatusCancelled:
		return true
	}
	return false
}

// Editable: còn sửa được phiếu lương hay không.
func (s Status) Editable() bool { return s == StatusDraft }

// Final: đã chốt, mọi thay đổi phải qua bút toán điều chỉnh riêng.
func (s Status) Final() bool { return s == StatusPaid || s == StatusCancelled }

// allowedTransitions liệt kê bước chuyển hợp lệ của kỳ lương.
//
// draft → calculating → draft: chạy tính lương xong thì quay về nháp để rà
// soát và sửa. draft → locked → paid là đường chốt. locked → draft để mở
// lại khi phát hiện sai TRƯỚC khi trả tiền. Sau `paid` thì không lùi được
// nữa — tiền đã ra khỏi tài khoản, sửa số liệu lúc này là làm sai lệch sổ
// sách chứ không phải sửa lỗi.
var allowedTransitions = map[Status][]Status{
	StatusDraft:       {StatusCalculating, StatusLocked, StatusCancelled},
	StatusCalculating: {StatusDraft},
	StatusLocked:      {StatusDraft, StatusPaid, StatusCancelled},
	StatusPaid:        {},
	StatusCancelled:   {},
}

func (s Status) CanTransitionTo(next Status) bool {
	if s == next {
		return true
	}
	for _, a := range allowedTransitions[s] {
		if a == next {
			return true
		}
	}
	return false
}

type Period struct {
	ID        uuid.UUID
	CompanyID uuid.UUID

	Year  int
	Month int
	Name  string

	PeriodStart time.Time
	PeriodEnd   time.Time
	Status      Status

	TotalGross     Money
	TotalNet       Money
	TotalTax       Money
	TotalInsurance Money
	EmployeeCount  int

	CalculatedAt *time.Time
	LockedAt     *time.Time
	PaidAt       *time.Time

	CreatedBy *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// =========================================================================
// PHIẾU LƯƠNG
// =========================================================================

type Payslip struct {
	ID         uuid.UUID
	PeriodID   uuid.UUID
	EmployeeID uuid.UUID

	StandardWorkdays float64
	ActualWorkdays   float64
	LeaveDays        float64
	AbsentDays       float64

	BaseSalary  Money
	Allowances  Money
	Bonuses     Money
	GrossSalary Money

	InsuranceBase     Money
	InsuranceEmployee Money
	InsuranceEmployer Money

	TaxableIncome      Money
	PersonalDeduction  Money
	DependentDeduction Money
	AssessableIncome   Money
	IncomeTax          Money

	OtherDeductions Money
	NetSalary       Money

	Dependents int
	Note       string

	CreatedAt time.Time
	UpdatedAt time.Time

	// JOIN hoặc nạp kèm lúc đọc.
	EmployeeName   string
	EmployeeCode   string
	DepartmentName string
	PositionName   string
	PeriodName     string
	PeriodYear     int
	PeriodMonth    int
	Items          []*PayslipItem

	// BankAccountMasked chỉ hiện 4 số cuối. Số đầy đủ không bao giờ rời
	// khỏi tầng repository — xem usecase/payroll.
	BankAccountMasked string
	BankName          string
}

type PayslipItem struct {
	ID        uuid.UUID
	PayslipID uuid.UUID
	Kind      ComponentKind
	Code      string
	Name      string
	Amount    Money
	Taxable   bool
	SortOrder int
	CreatedAt time.Time
}

type PayslipFilter struct {
	PeriodID     *uuid.UUID
	EmployeeID   *uuid.UUID
	DepartmentID *uuid.UUID

	// ScopedEmployeeIDs giới hạn kết quả. KHÔNG đến từ query string —
	// usecase đặt theo quyền của actor.
	ScopedEmployeeIDs []uuid.UUID
	RestrictScope     bool

	Page     int
	PageSize int
}

// =========================================================================
// BÁO CÁO CHI PHÍ NHÂN SỰ
// =========================================================================

// CostRow là chi phí nhân sự của một nhóm (phòng ban hoặc tháng).
type CostRow struct {
	Key   string // mã phòng ban hoặc "2026-09"
	Label string

	EmployeeCount     int
	TotalGross        Money
	TotalNet          Money
	TotalTax          Money
	InsuranceEmployee Money
	InsuranceEmployer Money
}

// TotalCost là chi phí THẬT của công ty: lương gộp cộng phần bảo hiểm công
// ty đóng.
//
// Lương gộp một mình không phải chi phí đầy đủ — phần bảo hiểm công ty đóng
// (21.5%) không xuất hiện trên phiếu lương của ai nhưng vẫn ra khỏi tài
// khoản công ty mỗi tháng.
func (c *CostRow) TotalCost() Money {
	return c.TotalGross + c.InsuranceEmployer
}

// =========================================================================
// NHẬT KÝ TRUY CẬP
// =========================================================================

// Tên hành động ghi vào nhật ký. Khai báo hằng để không gõ lệch chuỗi.
const (
	AuditViewPayslip    = "payslip.view"
	AuditListPayroll    = "payroll.list"
	AuditExportPayroll  = "payroll.export"
	AuditCalculate      = "payroll.calculate"
	AuditLockPeriod     = "payroll.lock"
	AuditMarkPaid       = "payroll.paid"
	AuditUpdateSalary   = "salary.update"
	AuditViewSalary     = "salary.view"
	AuditUpdateSettings = "payroll.settings_update"
)

type AuditEntry struct {
	ID         uuid.UUID
	ActorID    *uuid.UUID
	Action     string
	Resource   string
	ResourceID *uuid.UUID
	Detail     map[string]any
	IP         string
	RequestID  string
	CreatedAt  time.Time

	ActorName string // JOIN lúc đọc
}
