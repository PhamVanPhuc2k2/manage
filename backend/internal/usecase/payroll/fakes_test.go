package payroll

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
)

// Bản giả lập cho module lương.
//
// Mỗi bản giữ dữ liệu thật trong bộ nhớ và ghi lại những gì usecase yêu cầu
// nó làm. Với module này, việc GHI LẠI quan trọng ngang việc trả dữ liệu:
// phần lớn luật cần kiểm thử ở đây là về THỨ TỰ và về TÁC DỤNG PHỤ — đóng
// khoảng hiệu lực trước khi thêm bản mới, ghi nhật ký mỗi lần xem, trả
// trạng thái về nháp khi đẩy việc hỏng.

var errFake = errors.New("bản giả lập: không có dữ liệu")

// =========================================================================
// THAM SỐ TÍNH LƯƠNG
// =========================================================================

type fakeSettings struct {
	current *domainpay.Settings

	// versions ghi lại mọi bản đã lưu. Luật cốt lõi của tham số lương là
	// KHÔNG ghi đè bản cũ, nên danh sách này chính là thứ cần kiểm chứng.
	versions []*domainpay.Settings
	brackets map[uuid.UUID][]domainpay.TaxBracket

	// lastAt giữ thời điểm của lần Current gần nhất. Tính lại kỳ tháng
	// trước phải dùng tham số của tháng trước, và cách duy nhất thấy được
	// điều đó là nhìn thời điểm usecase hỏi.
	lastAt time.Time
}

func newFakeSettings(s *domainpay.Settings) *fakeSettings {
	return &fakeSettings{
		current:  s,
		brackets: map[uuid.UUID][]domainpay.TaxBracket{},
	}
}

func (f *fakeSettings) Current(
	_ context.Context, _ uuid.UUID, at time.Time,
) (*domainpay.Settings, error) {
	f.lastAt = at
	if f.current == nil {
		return nil, errFake
	}
	copied := *f.current
	return &copied, nil
}

func (f *fakeSettings) SaveVersion(
	_ context.Context, s *domainpay.Settings,
) (uuid.UUID, error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	f.versions = append(f.versions, s)
	return s.ID, nil
}

func (f *fakeSettings) ReplaceBrackets(
	_ context.Context, settingsID uuid.UUID, brackets []domainpay.TaxBracket,
) error {
	f.brackets[settingsID] = brackets
	return nil
}

// =========================================================================
// CẤU HÌNH LƯƠNG
// =========================================================================

type fakeStructures struct {
	byID      map[uuid.UUID]*domainpay.Structure
	effective map[uuid.UUID]*domainpay.Structure
	history   map[uuid.UUID][]*domainpay.Structure

	created    []*domainpay.Structure
	closed     []uuid.UUID
	components map[uuid.UUID][]*domainpay.Component

	// order ghi lại thứ tự các thao tác ghi, để kiểm chứng CloseOpenEnded
	// chạy TRƯỚC Create. Hai dòng cùng để mở sẽ khiến EffectiveOn trả kết
	// quả phụ thuộc thứ tự sắp xếp, tức là không xác định.
	order []string
}

func newFakeStructures() *fakeStructures {
	return &fakeStructures{
		byID:       map[uuid.UUID]*domainpay.Structure{},
		effective:  map[uuid.UUID]*domainpay.Structure{},
		history:    map[uuid.UUID][]*domainpay.Structure{},
		components: map[uuid.UUID][]*domainpay.Component{},
	}
}

func (f *fakeStructures) Create(_ context.Context, s *domainpay.Structure) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	f.order = append(f.order, "create")
	f.created = append(f.created, s)
	f.byID[s.ID] = s
	return nil
}

func (f *fakeStructures) Update(_ context.Context, s *domainpay.Structure) error {
	if f.byID[s.ID] == nil {
		return errFake
	}
	f.byID[s.ID] = s
	return nil
}

func (f *fakeStructures) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainpay.Structure, error) {
	if s := f.byID[id]; s != nil {
		copied := *s
		return &copied, nil
	}
	return nil, errFake
}

func (f *fakeStructures) EffectiveOn(
	_ context.Context, employeeID uuid.UUID, _ time.Time,
) (*domainpay.Structure, error) {
	if s := f.effective[employeeID]; s != nil {
		copied := *s
		return &copied, nil
	}
	return nil, errFake
}

func (f *fakeStructures) EffectiveOnMany(
	_ context.Context, employeeIDs []uuid.UUID, _ time.Time,
) (map[uuid.UUID]*domainpay.Structure, error) {
	out := map[uuid.UUID]*domainpay.Structure{}
	for _, id := range employeeIDs {
		if s := f.effective[id]; s != nil {
			out[id] = s
		}
	}
	return out, nil
}

func (f *fakeStructures) History(
	_ context.Context, employeeID uuid.UUID,
) ([]*domainpay.Structure, error) {
	return f.history[employeeID], nil
}

func (f *fakeStructures) CloseOpenEnded(
	_ context.Context, employeeID uuid.UUID, _ time.Time,
) error {
	f.order = append(f.order, "close")
	f.closed = append(f.closed, employeeID)
	return nil
}

func (f *fakeStructures) ReplaceComponents(
	_ context.Context, structureID uuid.UUID, components []*domainpay.Component,
) error {
	f.components[structureID] = components
	return nil
}

// =========================================================================
// KỲ LƯƠNG
// =========================================================================

type fakePeriods struct {
	byID      map[uuid.UUID]*domainpay.Period
	forMonth  map[string]bool
	updated   []*domainpay.Period
	created   []*domainpay.Period
	totalsFor []uuid.UUID
}

func newFakePeriods() *fakePeriods {
	return &fakePeriods{
		byID:     map[uuid.UUID]*domainpay.Period{},
		forMonth: map[string]bool{},
	}
}

func (f *fakePeriods) add(p *domainpay.Period) *domainpay.Period {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.byID[p.ID] = p
	return p
}

func (f *fakePeriods) Create(_ context.Context, p *domainpay.Period) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.created = append(f.created, p)
	f.byID[p.ID] = p
	return nil
}

func (f *fakePeriods) Update(_ context.Context, p *domainpay.Period) error {
	if f.byID[p.ID] == nil {
		return errFake
	}
	// Lưu bản sao: usecase sửa cùng một con trỏ nhiều lần trong một lời
	// gọi, nên giữ con trỏ sẽ khiến mọi phần tử cùng trỏ vào giá trị cuối.
	copied := *p
	f.updated = append(f.updated, &copied)
	f.byID[p.ID] = p
	return nil
}

func (f *fakePeriods) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainpay.Period, error) {
	if p := f.byID[id]; p != nil {
		copied := *p
		return &copied, nil
	}
	return nil, errFake
}

func (f *fakePeriods) List(
	context.Context, uuid.UUID, int,
) ([]*domainpay.Period, error) {
	out := make([]*domainpay.Period, 0, len(f.byID))
	for _, p := range f.byID {
		copied := *p
		out = append(out, &copied)
	}
	return out, nil
}

func (f *fakePeriods) ExistsForMonth(
	_ context.Context, _ uuid.UUID, year, month int,
) (bool, error) {
	return f.forMonth[monthKey(year, month)], nil
}

func (f *fakePeriods) UpdateTotals(_ context.Context, periodID uuid.UUID) error {
	f.totalsFor = append(f.totalsFor, periodID)
	return nil
}

func monthKey(year, month int) string {
	return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC).
		Format("2006-01")
}

// =========================================================================
// PHIẾU LƯƠNG
// =========================================================================

type fakePayslips struct {
	byID map[uuid.UUID]*domainpay.Payslip

	replaced   [][]*domainpay.Payslip
	updated    []*domainpay.Payslip
	lastFilter domainpay.PayslipFilter

	costByDept  []*domainpay.CostRow
	costByMonth []*domainpay.CostRow

	updateErr error
}

func newFakePayslips() *fakePayslips {
	return &fakePayslips{byID: map[uuid.UUID]*domainpay.Payslip{}}
}

func (f *fakePayslips) add(s *domainpay.Payslip) *domainpay.Payslip {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	f.byID[s.ID] = s
	return s
}

func (f *fakePayslips) ReplaceForPeriod(
	_ context.Context, _ uuid.UUID, slips []*domainpay.Payslip,
) error {
	f.replaced = append(f.replaced, slips)
	return nil
}

func (f *fakePayslips) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainpay.Payslip, error) {
	if s := f.byID[id]; s != nil {
		copied := *s
		return &copied, nil
	}
	return nil, errFake
}

func (f *fakePayslips) GetForEmployee(
	_ context.Context, _, employeeID uuid.UUID,
) (*domainpay.Payslip, error) {
	for _, s := range f.byID {
		if s.EmployeeID == employeeID {
			copied := *s
			return &copied, nil
		}
	}
	return nil, errFake
}

func (f *fakePayslips) List(
	_ context.Context, filter domainpay.PayslipFilter,
) ([]*domainpay.Payslip, int, error) {
	f.lastFilter = filter

	out := make([]*domainpay.Payslip, 0, len(f.byID))
	for _, s := range f.byID {
		copied := *s
		out = append(out, &copied)
	}
	return out, len(out), nil
}

func (f *fakePayslips) Update(_ context.Context, s *domainpay.Payslip) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	copied := *s
	f.updated = append(f.updated, &copied)
	f.byID[s.ID] = s
	return nil
}

func (f *fakePayslips) ListForEmployee(
	_ context.Context, employeeID uuid.UUID, _ int,
) ([]*domainpay.Payslip, error) {
	var out []*domainpay.Payslip
	for _, s := range f.byID {
		if s.EmployeeID == employeeID {
			copied := *s
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (f *fakePayslips) CostByDepartment(
	context.Context, uuid.UUID,
) ([]*domainpay.CostRow, error) {
	return f.costByDept, nil
}

func (f *fakePayslips) CostByMonth(
	context.Context, uuid.UUID, int,
) ([]*domainpay.CostRow, error) {
	return f.costByMonth, nil
}

// =========================================================================
// NHẬT KÝ TRUY CẬP
// =========================================================================

type fakeAudit struct {
	entries []*domainpay.AuditEntry
	logErr  error
}

func (f *fakeAudit) Log(_ context.Context, e *domainpay.AuditEntry) error {
	if f.logErr != nil {
		return f.logErr
	}
	f.entries = append(f.entries, e)
	return nil
}

func (f *fakeAudit) List(
	_ context.Context, resource string, _ *uuid.UUID, _ int,
) ([]*domainpay.AuditEntry, error) {
	var out []*domainpay.AuditEntry
	for _, e := range f.entries {
		if resource == "" || e.Resource == resource {
			out = append(out, e)
		}
	}
	return out, nil
}

// actions trả về danh sách hành động đã ghi, để phép thử đọc được gọn.
func (f *fakeAudit) actions() []string {
	out := make([]string, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e.Action)
	}
	return out
}

// =========================================================================
// CÁC CỔNG SANG MODULE KHÁC
// =========================================================================

type fakeAttendance struct {
	workdays map[uuid.UUID]domainpay.Workdays
}

func (f *fakeAttendance) WorkdaysInPeriod(
	_ context.Context, employeeIDs []uuid.UUID, _, _ time.Time,
) (map[uuid.UUID]domainpay.Workdays, error) {
	out := map[uuid.UUID]domainpay.Workdays{}
	for _, id := range employeeIDs {
		if w, ok := f.workdays[id]; ok {
			out[id] = w
		}
	}
	return out, nil
}

type fakeEmployees struct {
	activeIDs []uuid.UUID
	missing   map[uuid.UUID]bool
}

func (f *fakeEmployees) ListActiveIDs(
	context.Context, []uuid.UUID,
) ([]uuid.UUID, error) {
	return f.activeIDs, nil
}

func (f *fakeEmployees) Exists(_ context.Context, id uuid.UUID) (bool, error) {
	return !f.missing[id], nil
}

type fakeCompany struct {
	id  uuid.UUID
	loc *time.Location
}

func (f *fakeCompany) CurrentCompanyID(context.Context) (uuid.UUID, error) {
	return f.id, nil
}

func (f *fakeCompany) CurrentTimezone(context.Context) (*time.Location, error) {
	if f.loc == nil {
		return time.UTC, nil
	}
	return f.loc, nil
}

type fakeJobs struct {
	published []uuid.UUID
	err       error
}

func (f *fakeJobs) PublishCalculate(
	_ context.Context, periodID uuid.UUID, _ string,
) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, periodID)
	return nil
}

type fakeMailer struct {
	sent []string // email
	err  error
}

func (f *fakeMailer) SendPayslip(
	_ context.Context, email, _, _, _ string,
) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, email)
	return nil
}

// =========================================================================
// HARNESS
// =========================================================================

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type harness struct {
	uc *Usecase

	settings   *fakeSettings
	structures *fakeStructures
	periods    *fakePeriods
	payslips   *fakePayslips
	audit      *fakeAudit
	attendance *fakeAttendance
	employees  *fakeEmployees
	company    *fakeCompany
	jobs       *fakeJobs
	mailer     *fakeMailer
}

// newHarness dựng usecase với đồng hồ cố định.
//
// Nhiều luật ở đây phụ thuộc "hôm nay": kỳ lương chỉ tạo được cho tháng ĐÃ
// KẾT THÚC, và tham số mới có hiệu lực TỪ HÔM NAY. Dùng đồng hồ thật thì
// bộ kiểm thử sẽ hỏng vào một ngày nào đó mà không ai biết vì sao.
func newHarness(now time.Time) *harness {
	h := &harness{
		settings:   newFakeSettings(defaultSettings()),
		structures: newFakeStructures(),
		periods:    newFakePeriods(),
		payslips:   newFakePayslips(),
		audit:      &fakeAudit{},
		attendance: &fakeAttendance{workdays: map[uuid.UUID]domainpay.Workdays{}},
		employees:  &fakeEmployees{missing: map[uuid.UUID]bool{}},
		company:    &fakeCompany{id: uuid.New(), loc: time.UTC},
		jobs:       &fakeJobs{},
		mailer:     &fakeMailer{},
	}

	h.uc = NewUsecase(
		h.settings, h.structures, h.periods, h.payslips, h.audit,
		h.attendance, h.employees, h.company, h.jobs, h.mailer,
	)
	h.uc.SetClock(fixedClock{now: now})
	return h
}

// payActor dựng người dùng với danh sách quyền cho trước.
func payActor(employeeID uuid.UUID, perms ...string) *domainauth.Actor {
	p := map[string]struct{}{}
	for _, perm := range perms {
		p[perm] = struct{}{}
	}
	return &domainauth.Actor{
		UserID:      uuid.New(),
		EmployeeID:  employeeID,
		Permissions: p,
		Scope:       domainauth.ScopeAll,
	}
}

// Ràng buộc kiểu: mọi bản giả lập phải khớp cổng ở tầng domain, nếu không
// chúng chỉ kiểm thử chính mình.
var (
	_ domainpay.SettingsRepository  = (*fakeSettings)(nil)
	_ domainpay.StructureRepository = (*fakeStructures)(nil)
	_ domainpay.PeriodRepository    = (*fakePeriods)(nil)
	_ domainpay.PayslipRepository   = (*fakePayslips)(nil)
	_ domainpay.AuditRepository     = (*fakeAudit)(nil)
	_ domainpay.AttendanceLookup    = (*fakeAttendance)(nil)
	_ domainpay.EmployeeLookup      = (*fakeEmployees)(nil)
	_ domainpay.CompanyLookup       = (*fakeCompany)(nil)
	_ domainpay.JobPublisher        = (*fakeJobs)(nil)
	_ domainpay.PayslipMailer       = (*fakeMailer)(nil)
)
