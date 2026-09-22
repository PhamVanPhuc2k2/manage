package payroll

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
)

// Bộ kiểm thử cấu hình lương và tham số tính lương.
//
// Cả hai đều theo một nguyên tắc: GHI BẢN MỚI, KHÔNG SỬA ĐÈ. Sửa đè phá
// hỏng khả năng tính lại một kỳ cũ và cho ra đúng con số cũ — câu hỏi luôn
// xuất hiện khi có tranh chấp lao động hoặc khi cơ quan thuế hỏi lại.

func validStructureInput(employeeID uuid.UUID) StructureInput {
	return StructureInput{
		EmployeeID:    employeeID,
		BaseSalary:    money(20_000_000),
		Dependents:    1,
		EffectiveFrom: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
}

// =========================================================================
// ĐỌC CẤU HÌNH LƯƠNG
// =========================================================================

// TestGetOwnStructureIsAlwaysAllowed: người ta có quyền biết lương của
// chính mình, không cần quyền gì thêm.
func TestGetOwnStructureIsAlwaysAllowed(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	h.structures.effective[empID] = &domainpay.Structure{
		ID: uuid.New(), EmployeeID: empID, BaseSalary: money(20_000_000),
	}

	got, err := h.uc.GetStructure(
		context.Background(), payActor(empID), empID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.BaseSalary != money(20_000_000) {
		t.Errorf("lương cơ bản = %d, muốn 20000000", got.BaseSalary)
	}
}

func TestGetOtherStructureNeedsPermission(t *testing.T) {
	other := uuid.New()
	h := newHarness(nowAfterAugust())
	h.structures.effective[other] = &domainpay.Structure{
		ID: uuid.New(), EmployeeID: other, BaseSalary: money(20_000_000),
	}

	_, err := h.uc.GetStructure(
		context.Background(), payActor(uuid.New()), other)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestGetStructureRequiresActor(t *testing.T) {
	h := newHarness(nowAfterAugust())

	_, err := h.uc.GetStructure(context.Background(), nil, uuid.New())
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

func TestGetStructureIsAudited(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	h.structures.effective[empID] = &domainpay.Structure{
		ID: uuid.New(), EmployeeID: empID,
	}

	if _, err := h.uc.GetStructure(
		context.Background(), payActor(empID), empID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.audit.actions(), domainpay.AuditViewSalary) {
		t.Errorf("nhật ký = %v, phải có %q",
			h.audit.actions(), domainpay.AuditViewSalary)
	}
}

func TestStructureHistoryNeedsPermissionForOthers(t *testing.T) {
	other := uuid.New()
	h := newHarness(nowAfterAugust())

	_, err := h.uc.StructureHistory(
		context.Background(), payActor(uuid.New()), other)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestStructureHistoryWithPermission(t *testing.T) {
	other := uuid.New()
	h := newHarness(nowAfterAugust())
	h.structures.history[other] = []*domainpay.Structure{
		{ID: uuid.New(), EmployeeID: other, BaseSalary: money(25_000_000)},
		{ID: uuid.New(), EmployeeID: other, BaseSalary: money(20_000_000)},
	}

	got, err := h.uc.StructureHistory(context.Background(),
		payActor(uuid.New(), domainauth.PermSalaryRead), other)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("số bản ghi = %d, muốn 2", len(got))
	}
}

// =========================================================================
// GHI CẤU HÌNH LƯƠNG
// =========================================================================

func TestSetStructureRejectsBadInput(t *testing.T) {
	empID := uuid.New()
	negative := money(-1)

	cases := []struct {
		name   string
		mutate func(*StructureInput)
	}{
		{"lương âm", func(in *StructureInput) { in.BaseSalary = money(-1) }},
		{"lương bảo hiểm âm", func(in *StructureInput) { in.InsuranceSalary = &negative }},
		{"người phụ thuộc âm", func(in *StructureInput) { in.Dependents = -1 }},
		{"quá 20 người phụ thuộc", func(in *StructureInput) { in.Dependents = 21 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(nowAfterAugust())
			in := validStructureInput(empID)
			tc.mutate(&in)

			_, err := h.uc.SetStructure(
				context.Background(), payActor(uuid.New()), in)

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(h.structures.created) != 0 {
				t.Error("đã ghi dù đầu vào không hợp lệ")
			}
		})
	}
}

func TestSetStructureRejectsUnknownEmployee(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	h.employees.missing[empID] = true

	_, err := h.uc.SetStructure(context.Background(),
		payActor(uuid.New()), validStructureInput(empID))

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestSetStructureValidatesComponents(t *testing.T) {
	empID := uuid.New()

	cases := []struct {
		name string
		comp *domainpay.Component
	}{
		{"loại lạ", &domainpay.Component{Kind: "thuong-tet", Code: "PC", Name: "Phụ cấp"}},
		{"thiếu mã", &domainpay.Component{Kind: domainpay.KindAllowance, Code: " ", Name: "Phụ cấp"}},
		{"thiếu tên", &domainpay.Component{Kind: domainpay.KindAllowance, Code: "PC", Name: ""}},
		{"số tiền âm", &domainpay.Component{
			Kind: domainpay.KindAllowance, Code: "PC", Name: "Phụ cấp", Amount: money(-1),
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(nowAfterAugust())
			in := validStructureInput(empID)
			in.Components = []*domainpay.Component{tc.comp}

			_, err := h.uc.SetStructure(
				context.Background(), payActor(uuid.New()), in)

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(h.structures.created) != 0 {
				t.Error("đã ghi dù thành phần lương không hợp lệ")
			}
		})
	}
}

// TestSetStructureClosesOldBeforeCreatingNew.
//
// Thứ tự này quan trọng. Hai dòng cùng để mở sẽ khiến EffectiveOn trả kết
// quả phụ thuộc thứ tự sắp xếp — tức là không xác định — và lương của người
// đó nhảy qua lại giữa hai mức mà không ai hiểu vì sao.
func TestSetStructureClosesOldBeforeCreatingNew(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())

	if _, err := h.uc.SetStructure(context.Background(),
		payActor(uuid.New()), validStructureInput(empID),
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	want := []string{"close", "create"}
	if !slices.Equal(h.structures.order, want) {
		t.Errorf("thứ tự thao tác = %v, muốn %v", h.structures.order, want)
	}
}

// TestSetStructureInheritsBankDetails.
//
// Ghi nhận tăng lương thường chỉ gửi mức lương mới. Không kế thừa thì mỗi
// lần tăng lương là một lần xoá số tài khoản, và kế toán không chuyển khoản
// được cho người vừa được tăng lương — một lỗi vận hành nghiêm trọng mà
// không có thông báo nào.
func TestSetStructureInheritsBankDetails(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	h.structures.effective[empID] = &domainpay.Structure{
		ID: uuid.New(), EmployeeID: empID,
		BaseSalary:  money(18_000_000),
		BankAccount: "0123456789",
		BankName:    "Vietcombank",
	}

	if _, err := h.uc.SetStructure(context.Background(),
		payActor(uuid.New()), validStructureInput(empID),
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	got := h.structures.created[0]
	if got.BankAccount != "0123456789" || got.BankName != "Vietcombank" {
		t.Errorf("thông tin ngân hàng = %q / %q, muốn kế thừa từ bản cũ",
			got.BankAccount, got.BankName)
	}
}

// TestSetStructureOverridesBankDetailsWhenGiven: kế thừa chỉ áp dụng khi
// bản mới để trống. Đổi ngân hàng phải ăn.
func TestSetStructureOverridesBankDetailsWhenGiven(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())
	h.structures.effective[empID] = &domainpay.Structure{
		ID: uuid.New(), EmployeeID: empID,
		BankAccount: "0123456789", BankName: "Vietcombank",
	}

	in := validStructureInput(empID)
	in.BankAccount = "9876543210"
	in.BankName = "Techcombank"

	if _, err := h.uc.SetStructure(
		context.Background(), payActor(uuid.New()), in,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	got := h.structures.created[0]
	if got.BankAccount != "9876543210" || got.BankName != "Techcombank" {
		t.Errorf("thông tin ngân hàng = %q / %q, muốn dùng giá trị mới",
			got.BankAccount, got.BankName)
	}
}

func TestSetStructureDefaultsEffectiveFromToToday(t *testing.T) {
	now := nowAfterAugust()
	empID := uuid.New()
	h := newHarness(now)

	in := validStructureInput(empID)
	in.EffectiveFrom = time.Time{}

	if _, err := h.uc.SetStructure(
		context.Background(), payActor(uuid.New()), in,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !h.structures.created[0].EffectiveFrom.Equal(now) {
		t.Errorf("ngày hiệu lực = %v, muốn %v",
			h.structures.created[0].EffectiveFrom, now)
	}
}

func TestSetStructureStoresComponents(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())

	in := validStructureInput(empID)
	in.Components = []*domainpay.Component{
		{Kind: domainpay.KindAllowance, Code: "XANG", Name: "Phụ cấp xăng xe",
			Amount: money(1_000_000)},
	}

	if _, err := h.uc.SetStructure(
		context.Background(), payActor(uuid.New()), in,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	structureID := h.structures.created[0].ID
	if len(h.structures.components[structureID]) != 1 {
		t.Errorf("thành phần lương đã ghi = %v, muốn 1 dòng",
			h.structures.components[structureID])
	}
}

func TestSetStructureIsAudited(t *testing.T) {
	empID := uuid.New()
	h := newHarness(nowAfterAugust())

	if _, err := h.uc.SetStructure(context.Background(),
		payActor(uuid.New()), validStructureInput(empID),
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.audit.actions(), domainpay.AuditUpdateSalary) {
		t.Errorf("nhật ký = %v, phải có %q",
			h.audit.actions(), domainpay.AuditUpdateSalary)
	}
}

// =========================================================================
// THAM SỐ TÍNH LƯƠNG
// =========================================================================

func validSettingsInput() SettingsInput {
	s := defaultSettings()
	return SettingsInput{
		PersonalDeduction:        s.PersonalDeduction,
		DependentDeduction:       s.DependentDeduction,
		SocialRate:               s.SocialRate,
		HealthRate:               s.HealthRate,
		UnemploymentRate:         s.UnemploymentRate,
		EmployerSocialRate:       s.EmployerSocialRate,
		EmployerHealthRate:       s.EmployerHealthRate,
		EmployerUnemploymentRate: s.EmployerUnemploymentRate,
		SocialCap:                s.SocialCap,
		UnemploymentCap:          s.UnemploymentCap,
		StandardWorkdays:         s.StandardWorkdays,
		Brackets:                 s.Brackets,
	}
}

func TestUpdateSettingsRejectsBadInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*SettingsInput)
	}{
		{"giảm trừ bản thân âm", func(in *SettingsInput) { in.PersonalDeduction = money(-1) }},
		{"giảm trừ phụ thuộc âm", func(in *SettingsInput) { in.DependentDeduction = money(-1) }},
		{"ngày công chuẩn bằng 0", func(in *SettingsInput) { in.StandardWorkdays = 0 }},
		{"ngày công chuẩn quá 31", func(in *SettingsInput) { in.StandardWorkdays = 32 }},
		{"tỷ lệ bảo hiểm âm", func(in *SettingsInput) { in.SocialRate = -0.01 }},
		{"tỷ lệ bảo hiểm quá 1", func(in *SettingsInput) { in.HealthRate = 1.5 }},
		{"tỷ lệ công ty đóng quá 1", func(in *SettingsInput) { in.EmployerSocialRate = 2 }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(nowAfterAugust())
			in := validSettingsInput()
			tc.mutate(&in)

			_, err := h.uc.UpdateSettings(
				context.Background(), payActor(uuid.New()), in)

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(h.settings.versions) != 0 {
				t.Error("đã ghi bản mới dù đầu vào không hợp lệ")
			}
		})
	}
}

// TestValidateBrackets là phép thử quan trọng nhất của phần tham số.
//
// Biểu thuế có lỗ hổng là lỗi im lặng nguy hiểm nhất của cả module: thu
// nhập rơi vào khoảng không bậc nào phủ sẽ được tính thuế 0 đồng, và không
// có gì báo lỗi. Sai sót kiểu này chỉ lộ ra khi cơ quan thuế đối chiếu.
func TestValidateBrackets(t *testing.T) {
	top := func(v int64) *domainpay.Money { return toPtr(money(v)) }

	cases := []struct {
		name     string
		brackets []domainpay.TaxBracket
		want     int
	}{
		{
			"biểu hợp lệ hai bậc",
			[]domainpay.TaxBracket{
				{From: money(0), To: top(5_000_000), Rate: 0.05},
				{From: money(5_000_000), To: nil, Rate: 0.10},
			},
			http.StatusOK,
		},
		{
			"để trống thì giữ biểu cũ",
			nil,
			http.StatusOK,
		},
		{
			"bậc đầu không bắt đầu từ 0",
			[]domainpay.TaxBracket{
				{From: money(1_000_000), To: nil, Rate: 0.05},
			},
			http.StatusBadRequest,
		},
		{
			"biểu bị hở",
			[]domainpay.TaxBracket{
				{From: money(0), To: top(5_000_000), Rate: 0.05},
				{From: money(6_000_000), To: nil, Rate: 0.10},
			},
			http.StatusBadRequest,
		},
		{
			"biểu chồng lấn",
			[]domainpay.TaxBracket{
				{From: money(0), To: top(5_000_000), Rate: 0.05},
				{From: money(4_000_000), To: nil, Rate: 0.10},
			},
			http.StatusBadRequest,
		},
		{
			"bậc giữa để trống mốc trên",
			[]domainpay.TaxBracket{
				{From: money(0), To: nil, Rate: 0.05},
				{From: money(5_000_000), To: nil, Rate: 0.10},
			},
			http.StatusBadRequest,
		},
		{
			"mốc trên nhỏ hơn mốc dưới",
			[]domainpay.TaxBracket{
				{From: money(0), To: top(5_000_000), Rate: 0.05},
				{From: money(5_000_000), To: top(4_000_000), Rate: 0.10},
			},
			http.StatusBadRequest,
		},
		{
			"tỷ lệ thuế quá 1",
			[]domainpay.TaxBracket{
				{From: money(0), To: nil, Rate: 1.5},
			},
			http.StatusBadRequest,
		},
		{
			"tỷ lệ thuế âm",
			[]domainpay.TaxBracket{
				{From: money(0), To: nil, Rate: -0.05},
			},
			http.StatusBadRequest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(nowAfterAugust())
			in := validSettingsInput()
			in.Brackets = tc.brackets

			_, err := h.uc.UpdateSettings(
				context.Background(), payActor(uuid.New()), in)

			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d (%v)", got, tc.want, err)
			}
		})
	}
}

// TestUpdateSettingsWritesNewVersion.
//
// Ghi BẢN MỚI có hiệu lực từ hôm nay, không sửa đè. Nhờ vậy kỳ lương của
// những tháng trước vẫn thấy tham số cũ khi tính lại. Sửa đè sẽ khiến việc
// tính lại tháng Mười Hai dùng mức giảm trừ vừa đổi hồi tháng Một — sai, và
// không có gì báo.
func TestUpdateSettingsWritesNewVersion(t *testing.T) {
	now := nowAfterAugust()
	h := newHarness(now)

	in := validSettingsInput()
	in.PersonalDeduction = money(15_500_000)

	if _, err := h.uc.UpdateSettings(
		context.Background(), payActor(uuid.New()), in,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.settings.versions) != 1 {
		t.Fatalf("số bản đã ghi = %d, muốn 1", len(h.settings.versions))
	}

	v := h.settings.versions[0]
	if v.PersonalDeduction != money(15_500_000) {
		t.Errorf("giảm trừ = %d, muốn 15500000", v.PersonalDeduction)
	}
	if v.EffectiveFrom.Format("2006-01-02") != now.Format("2006-01-02") {
		t.Errorf("ngày hiệu lực = %s, muốn %s",
			v.EffectiveFrom.Format("2006-01-02"), now.Format("2006-01-02"))
	}
}

// TestUpdateSettingsCarriesBracketsForward.
//
// Không truyền biểu mới thì sao chép biểu của bản đang dùng sang. Nếu
// không, bản mới sẽ không có bậc thuế nào và mọi người im lặng được miễn
// thuế — một sai sót không ai phát hiện cho tới kỳ quyết toán.
func TestUpdateSettingsCarriesBracketsForward(t *testing.T) {
	h := newHarness(nowAfterAugust())

	in := validSettingsInput()
	in.Brackets = nil

	if _, err := h.uc.UpdateSettings(
		context.Background(), payActor(uuid.New()), in,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	id := h.settings.versions[0].ID
	if len(h.settings.brackets[id]) != 7 {
		t.Errorf("số bậc thuế của bản mới = %d, muốn 7 (sao chép từ bản cũ)",
			len(h.settings.brackets[id]))
	}
}

func TestUpdateSettingsIsAudited(t *testing.T) {
	h := newHarness(nowAfterAugust())

	if _, err := h.uc.UpdateSettings(
		context.Background(), payActor(uuid.New()), validSettingsInput(),
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.audit.actions(), domainpay.AuditUpdateSettings) {
		t.Errorf("nhật ký = %v, phải có %q",
			h.audit.actions(), domainpay.AuditUpdateSettings)
	}
}

func TestGetSettingsWithoutConfigIsUnprocessable(t *testing.T) {
	h := newHarness(nowAfterAugust())
	h.settings.current = nil

	_, err := h.uc.GetSettings(context.Background())
	if got := statusOf(err); got != http.StatusUnprocessableEntity {
		t.Errorf("mã lỗi = %d, muốn 422", got)
	}
}
