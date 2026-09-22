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

	byID    map[uuid.UUID]*domainatt.Schedule
	created []*domainatt.Schedule
	updated []*domainatt.Schedule
	deleted []uuid.UUID
}

func newFakeSchedules() *fakeSchedules {
	return &fakeSchedules{byID: map[uuid.UUID]*domainatt.Schedule{}}
}

func (f *fakeSchedules) add(s *domainatt.Schedule) *domainatt.Schedule {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	f.byID[s.ID] = s
	return s
}

func (f *fakeSchedules) Create(_ context.Context, s *domainatt.Schedule) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	f.created = append(f.created, s)
	f.byID[s.ID] = s
	return nil
}

func (f *fakeSchedules) Update(_ context.Context, s *domainatt.Schedule) error {
	if f.byID[s.ID] == nil {
		return domainatt.ErrNotFound
	}
	f.updated = append(f.updated, s)
	f.byID[s.ID] = s
	return nil
}

func (f *fakeSchedules) Delete(_ context.Context, id uuid.UUID) error {
	if f.byID[id] == nil {
		return domainatt.ErrNotFound
	}
	f.deleted = append(f.deleted, id)
	delete(f.byID, id)
	return nil
}

func (f *fakeSchedules) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainatt.Schedule, error) {
	if s := f.byID[id]; s != nil {
		copied := *s
		return &copied, nil
	}
	return nil, domainatt.ErrNotFound
}

func (f *fakeSchedules) List(
	context.Context, uuid.UUID,
) ([]*domainatt.Schedule, error) {
	out := make([]*domainatt.Schedule, 0, len(f.byID))
	for _, s := range f.byID {
		copied := *s
		out = append(out, &copied)
	}
	return out, nil
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

	created []*domainatt.Holiday
	deleted []uuid.UUID

	// known liệt kê id ngày lễ có thật; Delete trả lỗi với id lạ, vì usecase
	// dịch lỗi đó thành 404.
	known map[uuid.UUID]bool
}

func (f *fakeHolidays) Create(_ context.Context, h *domainatt.Holiday) error {
	if h.ID == uuid.Nil {
		h.ID = uuid.New()
	}
	f.created = append(f.created, h)
	return nil
}

func (f *fakeHolidays) Delete(_ context.Context, id uuid.UUID) error {
	if !f.known[id] {
		return domainatt.ErrNotFound
	}
	f.deleted = append(f.deleted, id)
	return nil
}

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

type dayKey struct {
	employeeID uuid.UUID
	day        string
}

type fakeDays struct {
	upserted []*domainatt.Day
	// upsertErr cho bài kiểm thử mô phỏng kỳ đã khoá.
	upsertErr error

	byDate  map[dayKey]*domainatt.Day
	list    []*domainatt.Day
	summary []*domainatt.MonthSummary

	lastFilter domainatt.DayFilter
	lockedRows int64
	lastLock   *bool
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

func (f *fakeDays) setDay(employeeID uuid.UUID, day time.Time, d *domainatt.Day) {
	if f.byDate == nil {
		f.byDate = map[dayKey]*domainatt.Day{}
	}
	f.byDate[dayKey{employeeID, day.Format("2006-01-02")}] = d
}

// GetByDate trả ErrNotFound khi chưa có bản ghi, giống repository thật.
//
// Trả (nil, nil) sẽ khiến mọi chỗ gọi theo mẫu `if d, err := ...; err == nil
// && d.IsLocked` nổ con trỏ nil — một kiểu hỏng mà bản giả lập tự tạo ra chứ
// không có thật trong hệ thống.
func (f *fakeDays) GetByDate(
	_ context.Context, employeeID uuid.UUID, day time.Time,
) (*domainatt.Day, error) {
	if d := f.byDate[dayKey{employeeID, day.Format("2006-01-02")}]; d != nil {
		copied := *d
		return &copied, nil
	}
	return nil, domainatt.ErrNotFound
}

func (f *fakeDays) List(
	_ context.Context, filter domainatt.DayFilter,
) ([]*domainatt.Day, error) {
	f.lastFilter = filter
	return f.list, nil
}

func (f *fakeDays) MonthSummary(
	_ context.Context, filter domainatt.DayFilter,
) ([]*domainatt.MonthSummary, error) {
	f.lastFilter = filter
	return f.summary, nil
}

func (f *fakeDays) SetLocked(
	_ context.Context, _, _ time.Time, locked bool,
) (int64, error) {
	f.lastLock = &locked
	return f.lockedRows, nil
}

type fakeAdjustments struct {
	byID map[uuid.UUID]*domainatt.Adjustment

	created []*domainatt.Adjustment
	updated []*domainatt.Adjustment

	// lastIDs/lastRestrict ghi lại hai tham số phạm vi của lần List gần
	// nhất. Phạm vi được áp bằng cách TRUYỀN XUỐNG repository, nên đây là
	// chỗ duy nhất nhìn thấy được nó.
	lastIDs      []uuid.UUID
	lastRestrict bool
}

func newFakeAdjustments() *fakeAdjustments {
	return &fakeAdjustments{byID: map[uuid.UUID]*domainatt.Adjustment{}}
}

func (f *fakeAdjustments) add(a *domainatt.Adjustment) *domainatt.Adjustment {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	f.byID[a.ID] = a
	return a
}

func (f *fakeAdjustments) Create(_ context.Context, a *domainatt.Adjustment) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	f.created = append(f.created, a)
	f.byID[a.ID] = a
	return nil
}

func (f *fakeAdjustments) Update(_ context.Context, a *domainatt.Adjustment) error {
	if f.byID[a.ID] == nil {
		return domainatt.ErrNotFound
	}
	f.updated = append(f.updated, a)
	f.byID[a.ID] = a
	return nil
}

func (f *fakeAdjustments) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainatt.Adjustment, error) {
	if a := f.byID[id]; a != nil {
		copied := *a
		return &copied, nil
	}
	return nil, domainatt.ErrNotFound
}

func (f *fakeAdjustments) List(
	_ context.Context, ids []uuid.UUID, restrict bool, status *domainatt.ApprovalStatus,
) ([]*domainatt.Adjustment, error) {
	f.lastIDs, f.lastRestrict = ids, restrict

	out := make([]*domainatt.Adjustment, 0, len(f.byID))
	for _, a := range f.byID {
		if status != nil && a.Status != *status {
			continue
		}
		copied := *a
		out = append(out, &copied)
	}
	return out, nil
}

type fakeLeaves struct {
	approvedOn map[uuid.UUID]*domainatt.LeaveRequest
	byID       map[uuid.UUID]*domainatt.LeaveRequest

	// overlapping là câu trả lời dựng sẵn cho phép kiểm tra trùng đơn. Bản
	// giả lập không tự dò trùng vì việc đó là của SQL; thứ cần kiểm thử là
	// usecase có gọi và có tôn trọng kết quả hay không.
	overlapping []*domainatt.LeaveRequest

	created []*domainatt.LeaveRequest
	updated []*domainatt.LeaveRequest

	lastFilter domainatt.LeaveFilter
}

func newFakeLeaves() *fakeLeaves {
	return &fakeLeaves{byID: map[uuid.UUID]*domainatt.LeaveRequest{}}
}

func (f *fakeLeaves) add(r *domainatt.LeaveRequest) *domainatt.LeaveRequest {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	f.byID[r.ID] = r
	return r
}

func (f *fakeLeaves) Create(_ context.Context, r *domainatt.LeaveRequest) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	f.created = append(f.created, r)
	f.byID[r.ID] = r
	return nil
}

func (f *fakeLeaves) Update(_ context.Context, r *domainatt.LeaveRequest) error {
	if f.byID[r.ID] == nil {
		return domainatt.ErrNotFound
	}
	f.updated = append(f.updated, r)
	f.byID[r.ID] = r
	return nil
}

func (f *fakeLeaves) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainatt.LeaveRequest, error) {
	if r := f.byID[id]; r != nil {
		copied := *r
		return &copied, nil
	}
	return nil, domainatt.ErrNotFound
}

func (f *fakeLeaves) List(
	_ context.Context, filter domainatt.LeaveFilter,
) ([]*domainatt.LeaveRequest, int, error) {
	f.lastFilter = filter

	out := make([]*domainatt.LeaveRequest, 0, len(f.byID))
	for _, r := range f.byID {
		copied := *r
		out = append(out, &copied)
	}
	return out, len(out), nil
}

func (f *fakeLeaves) Overlapping(
	context.Context, uuid.UUID, time.Time, time.Time, *uuid.UUID,
) ([]*domainatt.LeaveRequest, error) {
	return f.overlapping, nil
}

func (f *fakeLeaves) ApprovedOn(
	context.Context, time.Time,
) (map[uuid.UUID]*domainatt.LeaveRequest, error) {
	return f.approvedOn, nil
}

type balanceKey struct {
	employeeID uuid.UUID
	year       int
}

type fakeBalances struct {
	byKey map[balanceKey]*domainatt.Balance

	// used cộng dồn mọi lời gọi AddUsed. Giữ riêng thay vì cộng thẳng vào
	// Balance để phân biệt được "đã gọi trừ quỹ" với "quỹ tình cờ đúng".
	used     map[balanceKey]float64
	upserted []*domainatt.Balance
}

func newFakeBalances() *fakeBalances {
	return &fakeBalances{
		byKey: map[balanceKey]*domainatt.Balance{},
		used:  map[balanceKey]float64{},
	}
}

func (f *fakeBalances) set(employeeID uuid.UUID, year int, b *domainatt.Balance) {
	f.byKey[balanceKey{employeeID, year}] = b
}

func (f *fakeBalances) Get(
	_ context.Context, employeeID uuid.UUID, year int,
) (*domainatt.Balance, error) {
	if b := f.byKey[balanceKey{employeeID, year}]; b != nil {
		copied := *b
		return &copied, nil
	}
	return nil, domainatt.ErrNotFound
}

func (f *fakeBalances) Upsert(_ context.Context, b *domainatt.Balance) error {
	f.upserted = append(f.upserted, b)
	f.byKey[balanceKey{b.EmployeeID, b.Year}] = b
	return nil
}

func (f *fakeBalances) AddUsed(
	_ context.Context, employeeID uuid.UUID, year int, days float64,
) error {
	f.used[balanceKey{employeeID, year}] += days
	return nil
}

func (f *fakeBalances) List(
	context.Context, int, []uuid.UUID, bool,
) ([]*domainatt.Balance, error) {
	out := make([]*domainatt.Balance, 0, len(f.byKey))
	for _, b := range f.byKey {
		copied := *b
		out = append(out, &copied)
	}
	return out, nil
}

type fakeEmployees struct {
	activeIDs []uuid.UUID

	// missing liệt kê những id mà Exists trả về false, để kiểm thử nhánh
	// "nhân viên không tồn tại hoặc đã nghỉ việc".
	missing map[uuid.UUID]bool
}

func (f *fakeEmployees) Exists(_ context.Context, id uuid.UUID) (bool, error) {
	return !f.missing[id], nil
}

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

	schedules   *fakeSchedules
	holidays    *fakeHolidays
	sessions    *fakeSessions
	days        *fakeDays
	adjustments *fakeAdjustments
	leaves      *fakeLeaves
	balances    *fakeBalances
	employees   *fakeEmployees
	presence    *fakePresence
	company     *fakeCompany
}

func newHarness(now time.Time) *harness {
	h := &harness{
		schedules:   newFakeSchedules(),
		holidays:    &fakeHolidays{known: map[uuid.UUID]bool{}},
		sessions:    &fakeSessions{},
		days:        &fakeDays{},
		adjustments: newFakeAdjustments(),
		leaves:      newFakeLeaves(),
		balances:    newFakeBalances(),
		employees:   &fakeEmployees{missing: map[uuid.UUID]bool{}},
		presence:    &fakePresence{},
		company:     &fakeCompany{id: uuid.New(), loc: time.UTC},
	}

	h.uc = NewUsecase(
		h.schedules, h.holidays, h.sessions, h.days,
		h.adjustments, h.leaves, h.balances,
		h.employees, h.presence, h.company,
	)
	h.uc.SetClock(fixedClock{now: now})
	return h
}
