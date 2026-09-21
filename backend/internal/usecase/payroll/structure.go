package payroll

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

type StructureInput struct {
	EmployeeID      uuid.UUID
	BaseSalary      domainpay.Money
	InsuranceSalary *domainpay.Money
	Dependents      int
	BankAccount     string
	BankName        string
	EffectiveFrom   time.Time
	Note            string
	Components      []*domainpay.Component
}

// GetStructure đọc cấu hình lương đang có hiệu lực của một nhân viên.
func (u *Usecase) GetStructure(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
) (*domainpay.Structure, error) {
	// Xem cấu hình lương của NGƯỜI KHÁC cần quyền salary:read. Xem của
	// chính mình thì luôn được — người ta có quyền biết lương của mình.
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if actor.EmployeeID != employeeID && !actor.Can(domainauth.PermSalaryRead) {
		return nil, apperror.NotFound("cấu hình lương")
	}

	s, err := u.structures.EffectiveOn(ctx, employeeID, u.clock.Now())
	if err != nil {
		return nil, apperror.NotFound("cấu hình lương")
	}

	u.logAccess(ctx, actor, domainpay.AuditViewSalary, "salary_structure", &s.ID,
		map[string]any{"employee_id": employeeID.String()})

	return s, nil
}

// StructureHistory trả về lịch sử lương của một người.
//
// Lịch sử tăng lương là dữ liệu nhạy cảm hơn cả mức lương hiện tại — nó cho
// thấy cả quá trình đàm phán. Chỉ người có salary:read xem được, kể cả khi
// đó là lịch sử của chính mình thì vẫn cho xem (họ đã biết rồi).
func (u *Usecase) StructureHistory(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
) ([]*domainpay.Structure, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if actor.EmployeeID != employeeID && !actor.Can(domainauth.PermSalaryRead) {
		return nil, apperror.NotFound("cấu hình lương")
	}

	list, err := u.structures.History(ctx, employeeID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	u.logAccess(ctx, actor, domainpay.AuditViewSalary, "salary_history", nil,
		map[string]any{"employee_id": employeeID.String(), "rows": len(list)})

	return list, nil
}

// SetStructure tạo một bản cấu hình lương MỚI có hiệu lực từ một ngày.
//
// Luôn tạo dòng mới, không sửa đè dòng cũ. Sửa đè sẽ xoá mất lịch sử, và
// không ai trả lời được "tháng ba năm ngoái lương người này là bao nhiêu" —
// câu hỏi luôn xuất hiện khi tính lại hoặc khi có tranh chấp lao động.
func (u *Usecase) SetStructure(
	ctx context.Context,
	actor *domainauth.Actor,
	in StructureInput,
) (*domainpay.Structure, error) {
	if in.BaseSalary < 0 {
		return nil, apperror.Invalid("Lương cơ bản không được âm", nil)
	}
	if in.InsuranceSalary != nil && *in.InsuranceSalary < 0 {
		return nil, apperror.Invalid("Lương đóng bảo hiểm không được âm", nil)
	}
	if in.Dependents < 0 || in.Dependents > 20 {
		return nil, apperror.Invalid("Số người phụ thuộc không hợp lệ", nil)
	}
	if in.EffectiveFrom.IsZero() {
		in.EffectiveFrom = u.clock.Now()
	}

	ok, err := u.employees.Exists(ctx, in.EmployeeID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if !ok {
		return nil, apperror.Invalid("Nhân viên không tồn tại hoặc đã nghỉ việc", nil)
	}

	for _, c := range in.Components {
		if !c.Kind.Valid() {
			return nil, apperror.Invalid("Loại thành phần lương không hợp lệ", nil)
		}
		if strings.TrimSpace(c.Code) == "" || strings.TrimSpace(c.Name) == "" {
			return nil, apperror.Invalid("Thành phần lương phải có mã và tên", nil)
		}
		if c.Amount < 0 {
			return nil, apperror.Invalid("Số tiền thành phần lương không được âm", nil)
		}
	}

	// Kế thừa thông tin ngân hàng từ bản cũ khi bản mới không gửi lên.
	//
	// Ghi nhận tăng lương thường chỉ gửi mức lương mới. Không kế thừa thì
	// mỗi lần tăng lương là một lần xoá số tài khoản, và kế toán không
	// chuyển khoản được cho người vừa được tăng lương — một lỗi vận hành
	// nghiêm trọng mà không có thông báo nào.
	if prev, err := u.structures.EffectiveOn(ctx, in.EmployeeID, u.clock.Now()); err == nil {
		if in.BankAccount == "" {
			in.BankAccount = prev.BankAccount
		}
		if in.BankName == "" {
			in.BankName = prev.BankName
		}
	}

	// Đóng khoảng hiệu lực của cấu hình cũ TRƯỚC khi thêm bản mới.
	//
	// Hai dòng cùng để mở sẽ khiến EffectiveOn trả về kết quả phụ thuộc
	// thứ tự sắp xếp — tức là không xác định, và lương của người đó sẽ nhảy
	// qua lại giữa hai mức mà không ai hiểu vì sao.
	if err := u.structures.CloseOpenEnded(ctx, in.EmployeeID, in.EffectiveFrom); err != nil {
		return nil, apperror.Internal(err)
	}

	s := &domainpay.Structure{
		EmployeeID:      in.EmployeeID,
		BaseSalary:      in.BaseSalary,
		InsuranceSalary: in.InsuranceSalary,
		Dependents:      in.Dependents,
		BankAccount:     strings.TrimSpace(in.BankAccount),
		BankName:        strings.TrimSpace(in.BankName),
		EffectiveFrom:   in.EffectiveFrom,
		Note:            strings.TrimSpace(in.Note),
		CreatedBy:       actorEmployeeID(actor),
	}
	if err := u.structures.Create(ctx, s); err != nil {
		return nil, apperror.Internal(err)
	}

	if len(in.Components) > 0 {
		if err := u.structures.ReplaceComponents(ctx, s.ID, in.Components); err != nil {
			return nil, apperror.Internal(err)
		}
	}

	u.logAccess(ctx, actor, domainpay.AuditUpdateSalary, "salary_structure", &s.ID,
		map[string]any{
			"employee_id":    in.EmployeeID.String(),
			"base_salary":    int64(in.BaseSalary),
			"effective_from": in.EffectiveFrom.Format("2006-01-02"),
		})

	return u.structures.GetByID(ctx, s.ID)
}

// =========================================================================
// THAM SỐ TÍNH LƯƠNG
// =========================================================================

func (u *Usecase) GetSettings(ctx context.Context) (*domainpay.Settings, error) {
	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}

	s, err := u.settings.Current(ctx, companyID, u.clock.Now())
	if err != nil {
		return nil, apperror.New(apperror.KindUnprocessable,
			"Chưa cấu hình tham số tính lương cho công ty")
	}
	return s, nil
}

type SettingsInput struct {
	PersonalDeduction  domainpay.Money
	DependentDeduction domainpay.Money

	SocialRate       float64
	HealthRate       float64
	UnemploymentRate float64

	EmployerSocialRate       float64
	EmployerHealthRate       float64
	EmployerUnemploymentRate float64

	SocialCap       domainpay.Money
	UnemploymentCap domainpay.Money

	StandardWorkdays float64

	Brackets []domainpay.TaxBracket
}

func (u *Usecase) UpdateSettings(
	ctx context.Context,
	actor *domainauth.Actor,
	in SettingsInput,
) (*domainpay.Settings, error) {
	current, err := u.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	if in.PersonalDeduction < 0 || in.DependentDeduction < 0 {
		return nil, apperror.Invalid("Mức giảm trừ không được âm", nil)
	}
	if in.StandardWorkdays <= 0 || in.StandardWorkdays > 31 {
		return nil, apperror.Invalid("Ngày công chuẩn phải từ 1 tới 31", nil)
	}
	for _, r := range []float64{
		in.SocialRate, in.HealthRate, in.UnemploymentRate,
		in.EmployerSocialRate, in.EmployerHealthRate, in.EmployerUnemploymentRate,
	} {
		if r < 0 || r > 1 {
			return nil, apperror.Invalid("Tỷ lệ bảo hiểm phải từ 0 tới 1", nil)
		}
	}

	if err := validateBrackets(in.Brackets); err != nil {
		return nil, err
	}

	// Ghi một BẢN MỚI có hiệu lực từ HÔM NAY, không sửa đè bản cũ.
	//
	// Nhờ vậy kỳ lương của những tháng trước vẫn thấy tham số cũ khi tính
	// lại. Sửa đè sẽ khiến việc tính lại tháng Mười Hai dùng mức giảm trừ
	// vừa đổi hồi tháng Một — sai, và không có gì báo.
	loc := u.location(ctx)
	now := u.clock.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	next := &domainpay.Settings{
		CompanyID:                current.CompanyID,
		EffectiveFrom:            today,
		PersonalDeduction:        in.PersonalDeduction,
		DependentDeduction:       in.DependentDeduction,
		SocialRate:               in.SocialRate,
		HealthRate:               in.HealthRate,
		UnemploymentRate:         in.UnemploymentRate,
		EmployerSocialRate:       in.EmployerSocialRate,
		EmployerHealthRate:       in.EmployerHealthRate,
		EmployerUnemploymentRate: in.EmployerUnemploymentRate,
		SocialCap:                in.SocialCap,
		UnemploymentCap:          in.UnemploymentCap,
		StandardWorkdays:         in.StandardWorkdays,
	}

	id, err := u.settings.SaveVersion(ctx, next)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	// Biểu thuế gắn vào BẢN MỚI. Không truyền biểu mới thì sao chép biểu của
	// bản đang dùng sang — nếu không, bản mới sẽ không có bậc thuế nào và
	// mọi người im lặng được miễn thuế.
	brackets := in.Brackets
	if len(brackets) == 0 {
		brackets = current.Brackets
	}
	if err := u.settings.ReplaceBrackets(ctx, id, brackets); err != nil {
		return nil, apperror.Internal(err)
	}

	u.logAccess(ctx, actor, domainpay.AuditUpdateSettings, "payroll_settings",
		&id, map[string]any{"effective_from": today.Format("2006-01-02")})

	return u.GetSettings(ctx)
}

// validateBrackets kiểm tra biểu thuế liền mạch và tăng dần.
//
// Biểu thuế có lỗ hổng là lỗi im lặng nguy hiểm nhất của cả module: thu
// nhập rơi vào khoảng không bậc nào phủ sẽ được tính thuế 0 đồng, và không
// có gì báo lỗi. Phải bắt ngay lúc nhập.
func validateBrackets(brackets []domainpay.TaxBracket) error {
	if len(brackets) == 0 {
		return nil // giữ nguyên biểu cũ
	}

	if brackets[0].From != 0 {
		return apperror.Invalid("Bậc thuế đầu tiên phải bắt đầu từ 0", nil)
	}

	for i, b := range brackets {
		if b.Rate < 0 || b.Rate > 1 {
			return apperror.Invalid("Tỷ lệ thuế phải từ 0 tới 1", nil)
		}
		if b.To != nil && *b.To <= b.From {
			return apperror.Invalid("Mốc trên của bậc thuế phải lớn hơn mốc dưới", nil)
		}

		last := i == len(brackets)-1
		if last {
			continue
		}

		if b.To == nil {
			return apperror.Invalid(
				"Chỉ bậc thuế CUỐI CÙNG mới được để trống mốc trên", nil)
		}
		// Bậc sau phải bắt đầu đúng chỗ bậc trước kết thúc.
		if brackets[i+1].From != *b.To {
			return apperror.Invalid(
				"Biểu thuế bị hở: bậc sau phải bắt đầu đúng tại mốc kết thúc của bậc trước", nil)
		}
	}
	return nil
}
