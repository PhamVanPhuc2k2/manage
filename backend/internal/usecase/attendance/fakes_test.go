package attendance

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
)

// Bộ giả lập cho toàn bộ cổng của module chấm công.
//
// Viết tay chứ không sinh bằng mockgen: các bài kiểm thử ở đây chỉ cần trả về
// dữ liệu dựng sẵn, và một tệp giả lập đọc được thì dễ suy luận hơn nhiều so
// với mã sinh tự động cùng bộ API kỳ vọng của nó.
//
// Mỗi kiểu hiện thực ĐẦY ĐỦ interface (Go không cho hiện thực một phần) nhưng
// chỉ những method có hàm gán vào mới làm gì; phần còn lại trả giá trị zero.
// Nhờ vậy mỗi bài kiểm thử chỉ phải khai báo đúng thứ nó quan tâm.

type fakeSchedules struct {
	applicable func(employeeID uuid.UUID, on time.Time) []*domainatt.Schedule
}

func (f *fakeSchedules) Create(context.Context, *domainatt.Schedule) error { return nil }
func (f *fakeSchedules) Update(context.Context, *domainatt.Schedule) error { return nil }
func (f *fakeSchedules) Delete(context.Context, uuid.UUID) error           { return nil }

func (f *fakeSchedules) GetByID(context.Context, uuid.UUID) (*domainatt.Schedule, error) {
	return nil, nil
}

func (f *fakeSchedules) List(context.Context, uuid.UUID) ([]*domainatt.Schedule, error) {
	return nil, nil
}

func (f *fakeSchedules) Applicable(
	_ context.Context,
	employeeID uuid.UUID,
	on time.Time,
) ([]*domainatt.Schedule, error) {
	if f.applicable == nil {
		return nil, nil
	}
	return f.applicable(employeeID, on), nil
}

type fakeHolidays struct {
	between func(from, to time.Time) []*domainatt.Holiday
}

func (f *fakeHolidays) Create(context.Context, *domainatt.Holiday) error { return nil }
func (f *fakeHolidays) Delete(context.Context, uuid.UUID) error          { return nil }

func (f *fakeHolidays) ListBetween(
	_ context.Context,
	_ uuid.UUID,
	from, to time.Time,
) ([]*domainatt.Holiday, error) {
	if f.between == nil {
		return nil, nil
	}
	return f.between(from, to), nil
}

type fakeSessions struct {
	created  []*domainatt.Session
	extended int
	// extendOK quyết định ExtendLatest có nới dài được phiên cũ hay không.
	// Đây chính là nhánh "gộp phiên" mà bài kiểm thử gộp phiên cần điều khiển.
	extendOK bool

	aggregate func(employeeID uuid.UUID, day time.Time) (int, int, *time.Time, *time.Time)
	withSess  []uuid.UUID
}

func (f *fakeSessions) Create(_ context.Context, s *domainatt.Session) error {
	f.created = append(f.created, s)
	return nil
}

func (f *fakeSessions) ExtendLatest(
	context.Context, uuid.UUID, time.Time, int, time.Duration,
) (bool, error) {
	if f.extendOK {
		f.extended++
	}
	return f.extendOK, nil
}

func (f *fakeSessions) ListByDay(
	context.Context, uuid.UUID, time.Time,
) ([]*domainatt.Session, error) {
	return nil, nil
}

func (f *fakeSessions) ListBetween(
	context.Context, uuid.UUID, time.Time, time.Time,
) ([]*domainatt.Session, error) {
	return nil, nil
}

func (f *fakeSessions) AggregateDay(
	_ context.Context,
	employeeID uuid.UUID,
	day time.Time,
) (int, int, *time.Time, *time.Time, error) {
	if f.aggregate == nil {
		return 0, 0, nil, nil, nil
	}
	online, active, first, last := f.aggregate(employeeID, day)
	return online, active, first, last, nil
}

func (f *fakeSessions) EmployeesWithSessions(
	context.Context, time.Time,
) ([]uuid.UUID, error) {
	return f.withSess, nil
}

type fakeDays struct {
	upserted []*domainatt.Day
	// upsertErr cho bài kiểm thử mô phỏng kỳ đã khoá.
	upsertErr error
}

func (f *fakeDays) Upsert(_ context.Context, d *domainatt.Day) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	// Lưu BẢN SAO: usecase dùng lại cùng một con trỏ cho nhiều ngày, nên giữ
	// con trỏ sẽ khiến mọi phần tử trong danh sách cùng trỏ vào giá trị cuối.
	copied := *d
	f.upserted = append(f.upserted, &copied)
	return nil
}

func (f *fakeDays) GetByDate(
	context.Context, uuid.UUID, time.Time,
) (*domainatt.Day, error) {
	return nil, nil
}

func (f *fakeDays) List(context.Context, domainatt.DayFilter) ([]*domainatt.Day, error) {
	return nil, nil
}

func (f *fakeDays) MonthSummary(
	context.Context, domainatt.DayFilter,
) ([]*domainatt.MonthSummary, error) {
	return nil, nil
}

func (f *fakeDays) SetLocked(context.Context, time.Time, time.Time, bool) (int64, error) {
	return 0, nil
}

type fakeAdjustments struct{}

func (fakeAdjustments) Create(context.Context, *domainatt.Adjustment) error { return nil }
func (fakeAdjustments) Update(context.Context, *domainatt.Adjustment) error { return nil }

func (fakeAdjustments) GetByID(context.Context, uuid.UUID) (*domainatt.Adjustment, error) {
	return nil, nil
}

func (fakeAdjustments) List(
	context.Context, []uuid.UUID, bool, *domainatt.ApprovalStatus,
) ([]*domainatt.Adjustment, error) {
	return nil, nil
}

type fakeLeaves struct {
	approvedOn map[uuid.UUID]*domainatt.LeaveRequest
}

func (f *fakeLeaves) Create(context.Context, *domainatt.LeaveRequest) error { return nil }
func (f *fakeLeaves) Update(context.Context, *domainatt.LeaveRequest) error { return nil }

func (f *fakeLeaves) GetByID(context.Context, uuid.UUID) (*domainatt.LeaveRequest, error) {
	return nil, nil
}

func (f *fakeLeaves) List(
	context.Context, domainatt.LeaveFilter,
) ([]*domainatt.LeaveRequest, int, error) {
	return nil, 0, nil
}

func (f *fakeLeaves) Overlapping(
	context.Context, uuid.UUID, time.Time, time.Time, *uuid.UUID,
) ([]*domainatt.LeaveRequest, error) {
	return nil, nil
}

func (f *fakeLeaves) ApprovedOn(
	context.Context, time.Time,
) (map[uuid.UUID]*domainatt.LeaveRequest, error) {
	return f.approvedOn, nil
}

type fakeBalances struct{}

func (fakeBalances) Get(context.Context, uuid.UUID, int) (*domainatt.Balance, error) {
	return nil, nil
}
func (fakeBalances) Upsert(context.Context, *domainatt.Balance) error       { return nil }
func (fakeBalances) AddUsed(context.Context, uuid.UUID, int, float64) error { return nil }

func (fakeBalances) List(
	context.Context, int, []uuid.UUID, bool,
) ([]*domainatt.Balance, error) {
	return nil, nil
}

type fakeEmployees struct {
	activeIDs []uuid.UUID
}

func (f *fakeEmployees) Exists(context.Context, uuid.UUID) (bool, error) { return true, nil }

func (f *fakeEmployees) DepartmentOf(context.Context, uuid.UUID) (*uuid.UUID, error) {
	return nil, nil
}

func (f *fakeEmployees) ListActiveIDs(
	context.Context, []uuid.UUID,
) ([]uuid.UUID, error) {
	return f.activeIDs, nil
}

type fakePresence struct {
	snapshot []domainatt.PresenceSnapshot
	status   map[uuid.UUID]string
}

func (f *fakePresence) SnapshotOnline(
	context.Context,
) ([]domainatt.PresenceSnapshot, error) {
	return f.snapshot, nil
}

func (f *fakePresence) StatusOf(
	context.Context, []uuid.UUID,
) (map[uuid.UUID]string, error) {
	return f.status, nil
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

// fixedClock để bài kiểm thử không phụ thuộc vào thời điểm chạy.
type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

// harness gom usecase và các bản giả lập lại, để bài kiểm thử vừa gọi được
// usecase vừa đọc được thứ nó đã ghi.
type harness struct {
	uc *Usecase

	schedules *fakeSchedules
	holidays  *fakeHolidays
	sessions  *fakeSessions
	days      *fakeDays
	leaves    *fakeLeaves
	employees *fakeEmployees
	presence  *fakePresence
	company   *fakeCompany
}

func newHarness(now time.Time) *harness {
	h := &harness{
		schedules: &fakeSchedules{},
		holidays:  &fakeHolidays{},
		sessions:  &fakeSessions{},
		days:      &fakeDays{},
		leaves:    &fakeLeaves{},
		employees: &fakeEmployees{},
		presence:  &fakePresence{},
		company:   &fakeCompany{id: uuid.New(), loc: time.UTC},
	}

	h.uc = NewUsecase(
		h.schedules, h.holidays, h.sessions, h.days,
		fakeAdjustments{}, h.leaves, fakeBalances{},
		h.employees, h.presence, h.company,
	)
	h.uc.SetClock(fixedClock{now: now})
	return h
}
