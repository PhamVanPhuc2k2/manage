package attendance

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
)

func officeHours() *domainatt.Schedule {
	return &domainatt.Schedule{
		WorkStart:    "08:00",
		WorkEnd:      "17:30",
		BreakMinutes: 90,
		GraceMinutes: 15,
		Workdays:     []int16{1, 2, 3, 4, 5},
	}
}

func at(day time.Time, hour, min int) *time.Time {
	t := time.Date(day.Year(), day.Month(), day.Day(), hour, min, 0, 0, time.UTC)
	return &t
}

// =========================================================================
// QUÉT PRESENCE
// =========================================================================

// TestCollectPresenceCountsActiveSeparately khoá lại phân biệt cốt lõi của
// Phase 3: "mở tab" khác "đang làm việc".
//
// Gộp hai thứ này lại thì máy để đó qua đêm vẫn được tính là làm việc đủ ca,
// và toàn bộ dữ liệu chấm công mất ý nghĩa.
func TestCollectPresenceCountsActiveSeparately(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	h := newHarness(now)

	activeEmp, idleEmp := uuid.New(), uuid.New()
	h.presence.snapshot = []domainatt.PresenceSnapshot{
		{EmployeeID: activeEmp, IsActive: true, LastSeenAt: now},
		{EmployeeID: idleEmp, IsActive: false, LastSeenAt: now},
	}

	res, err := h.uc.CollectPresence(context.Background())
	if err != nil {
		t.Fatalf("CollectPresence lỗi: %v", err)
	}

	if res.Online != 2 {
		t.Errorf("Online = %d, muốn 2", res.Online)
	}
	if res.Active != 1 {
		t.Errorf("Active = %d, muốn 1 (chỉ người đang hoạt động)", res.Active)
	}
	if res.Created != 2 {
		t.Errorf("Created = %d, muốn 2", res.Created)
	}

	// Người chỉ mở tab phải có phiên với 0 phút hoạt động.
	for _, s := range h.sessions.created {
		switch s.EmployeeID {
		case activeEmp:
			if s.ActiveMinutes != int(CollectInterval.Minutes()) {
				t.Errorf("người đang hoạt động: ActiveMinutes = %d, muốn %d",
					s.ActiveMinutes, int(CollectInterval.Minutes()))
			}
		case idleEmp:
			if s.ActiveMinutes != 0 {
				t.Errorf("người chỉ mở tab: ActiveMinutes = %d, muốn 0", s.ActiveMinutes)
			}
		}
	}
}

// TestCollectPresenceSessionStartsOneIntervalBack: lấy `now` làm điểm bắt đầu
// sẽ tạo ra phiên dài 0 phút, và phút làm việc đầu tiên của mỗi người biến
// mất khỏi bảng công mỗi ngày.
func TestCollectPresenceSessionStartsOneIntervalBack(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	h := newHarness(now)

	h.presence.snapshot = []domainatt.PresenceSnapshot{
		{EmployeeID: uuid.New(), IsActive: true, LastSeenAt: now},
	}

	if _, err := h.uc.CollectPresence(context.Background()); err != nil {
		t.Fatalf("CollectPresence lỗi: %v", err)
	}
	if len(h.sessions.created) != 1 {
		t.Fatalf("muốn 1 phiên, được %d", len(h.sessions.created))
	}

	s := h.sessions.created[0]
	if got := s.Minutes(); got != int(CollectInterval.Minutes()) {
		t.Errorf("độ dài phiên mới = %d phút, muốn %d",
			got, int(CollectInterval.Minutes()))
	}
	if !s.EndedAt.Equal(now) {
		t.Errorf("EndedAt = %v, muốn %v", s.EndedAt, now)
	}
}

// TestCollectPresenceExtendsInsteadOfCreating: khi còn phiên nối được thì
// nới dài, KHÔNG tạo phiên mới. Tạo mới mỗi lần quét sẽ sinh ra 120 phiên
// một giờ cho mỗi người, và bảng công thành một rừng phiên một phút.
func TestCollectPresenceExtendsInsteadOfCreating(t *testing.T) {
	now := time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC)
	h := newHarness(now)
	h.sessions.extendOK = true

	h.presence.snapshot = []domainatt.PresenceSnapshot{
		{EmployeeID: uuid.New(), IsActive: true, LastSeenAt: now},
	}

	res, err := h.uc.CollectPresence(context.Background())
	if err != nil {
		t.Fatalf("CollectPresence lỗi: %v", err)
	}
	if res.Extended != 1 || res.Created != 0 {
		t.Errorf("Extended=%d Created=%d, muốn Extended=1 Created=0",
			res.Extended, res.Created)
	}
	if len(h.sessions.created) != 0 {
		t.Error("không được tạo phiên mới khi đã nới dài được phiên cũ")
	}
}

func TestCollectPresenceNoOneOnline(t *testing.T) {
	h := newHarness(time.Date(2026, 9, 21, 9, 0, 0, 0, time.UTC))

	res, err := h.uc.CollectPresence(context.Background())
	if err != nil {
		t.Fatalf("CollectPresence lỗi: %v", err)
	}
	if res.Online != 0 || len(h.sessions.created) != 0 {
		t.Error("không ai online thì không được ghi gì")
	}
}

// =========================================================================
// TỔNG HỢP NGÀY
// =========================================================================

// TestRollupMarksAbsentEmployees là lý do RollupDay duyệt MỌI nhân viên chứ
// không chỉ những người có phiên.
//
// Người vắng mặt không có phiên nào, nên nếu chỉ duyệt bảng phiên thì họ đơn
// giản không xuất hiện — và "không có dòng" rất khác với "có dòng ghi vắng
// mặt" khi tính lương.
func TestRollupMarksAbsentEmployees(t *testing.T) {
	day := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC) // thứ hai
	h := newHarness(day)

	present, absent := uuid.New(), uuid.New()
	h.employees.activeIDs = []uuid.UUID{present, absent}
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{officeHours()}
	}
	h.sessions.aggregate = func(id uuid.UUID, d time.Time) (int, int, *time.Time, *time.Time) {
		if id == present {
			return 480, 400, at(d, 8, 0), at(d, 17, 30)
		}
		return 0, 0, nil, nil
	}

	res, err := h.uc.RollupDay(context.Background(), day)
	if err != nil {
		t.Fatalf("RollupDay lỗi: %v", err)
	}

	if res.Processed != 2 {
		t.Errorf("Processed = %d, muốn 2", res.Processed)
	}
	if res.Present != 1 || res.Absent != 1 {
		t.Errorf("Present=%d Absent=%d, muốn 1 và 1", res.Present, res.Absent)
	}

	// Người vắng phải có dòng bảng công với thiếu giờ bằng cả ca.
	for _, d := range h.days.upserted {
		if d.EmployeeID == absent {
			if d.Status != domainatt.DayAbsent {
				t.Errorf("trạng thái người vắng = %q, muốn %q", d.Status, domainatt.DayAbsent)
			}
			if d.ShortfallMinutes != officeHours().ExpectedMinutes() {
				t.Errorf("thiếu giờ của người vắng = %d, muốn %d",
					d.ShortfallMinutes, officeHours().ExpectedMinutes())
			}
		}
	}
}

// TestRollupStatusPriority khoá lại thứ tự xét trạng thái.
//
// Ngày lễ và cuối tuần thắng tất cả: không ai phải đi làm thì không thể tính
// là vắng. Đảo thứ tự này nghĩa là cả công ty bị ghi vắng mặt vào ngày Tết.
func TestRollupStatusPriority(t *testing.T) {
	emp := uuid.New()

	cases := []struct {
		name     string
		day      time.Time
		holiday  bool
		onLeave  bool
		online   int
		expected domainatt.DayStatus
	}{
		{
			name:     "ngày lễ thắng cả đơn nghỉ và vắng mặt",
			day:      time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC),
			holiday:  true,
			onLeave:  true,
			expected: domainatt.DayHoliday,
		},
		{
			name:     "cuối tuần không tính là vắng",
			day:      time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC), // thứ bảy
			expected: domainatt.DayWeekend,
		},
		{
			name:     "có đơn nghỉ được duyệt",
			day:      time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC),
			onLeave:  true,
			expected: domainatt.DayLeave,
		},
		{
			name:     "có phiên làm việc",
			day:      time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC),
			online:   480,
			expected: domainatt.DayPresent,
		},
		{
			name:     "không có gì cả",
			day:      time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC),
			expected: domainatt.DayAbsent,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(c.day)
			h.employees.activeIDs = []uuid.UUID{emp}
			h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
				return []*domainatt.Schedule{officeHours()}
			}

			if c.holiday {
				h.holidays.between = func(from, to time.Time) []*domainatt.Holiday {
					return []*domainatt.Holiday{{Date: from, Name: "Ngày lễ"}}
				}
			}
			if c.onLeave {
				h.leaves.approvedOn = map[uuid.UUID]*domainatt.LeaveRequest{
					emp: {EmployeeID: emp},
				}
			}
			online := c.online
			h.sessions.aggregate = func(uuid.UUID, time.Time) (int, int, *time.Time, *time.Time) {
				if online == 0 {
					return 0, 0, nil, nil
				}
				return online, online, at(c.day, 8, 0), at(c.day, 17, 30)
			}

			if _, err := h.uc.RollupDay(context.Background(), c.day); err != nil {
				t.Fatalf("RollupDay lỗi: %v", err)
			}
			if len(h.days.upserted) != 1 {
				t.Fatalf("muốn 1 dòng bảng công, được %d", len(h.days.upserted))
			}
			if got := h.days.upserted[0].Status; got != c.expected {
				t.Errorf("trạng thái = %q, muốn %q", got, c.expected)
			}
		})
	}
}

// TestRollupSkipsLockedPeriod: kỳ đã khoá thì bỏ qua trong im lặng — đó
// chính là tác dụng của việc khoá kỳ, không phải một lỗi cần báo.
func TestRollupSkipsLockedPeriod(t *testing.T) {
	day := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	h := newHarness(day)

	h.employees.activeIDs = []uuid.UUID{uuid.New()}
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{officeHours()}
	}
	h.days.upsertErr = domainatt.ErrLocked

	res, err := h.uc.RollupDay(context.Background(), day)
	if err != nil {
		t.Fatalf("kỳ khoá không được trả lỗi ra ngoài: %v", err)
	}
	if res.Locked != 1 {
		t.Errorf("Locked = %d, muốn 1", res.Locked)
	}
	if res.Processed != 0 {
		t.Errorf("Processed = %d, muốn 0 (không ghi được thì không tính là đã xử lý)",
			res.Processed)
	}
}

// =========================================================================
// ĐI MUỘN, VỀ SỚM, THIẾU GIỜ
// =========================================================================

func TestApplyLateness(t *testing.T) {
	day := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	s := officeHours() // 08:00–17:30, nghỉ 90 phút, ân hạn 15 phút

	cases := []struct {
		name          string
		online        int
		first, last   *time.Time
		wantLate      int
		wantEarly     int
		wantShortfall int
	}{
		{
			name:   "đúng giờ, đủ ca",
			online: 480,
			first:  at(day, 8, 0),
			last:   at(day, 17, 30),
		},
		{
			// Ân hạn 15 phút: kẹt xe vài phút không phải vi phạm. Thiếu nó
			// thì bảng công đầy những con số 2 phút, 3 phút — không ai đọc nữa.
			name:   "muộn 10 phút vẫn trong ân hạn",
			online: 480,
			first:  at(day, 8, 10),
			last:   at(day, 17, 30),
		},
		{
			name:     "muộn 30 phút: trừ ân hạn còn 15",
			online:   480,
			first:    at(day, 8, 30),
			last:     at(day, 17, 30),
			wantLate: 15,
		},
		{
			name:      "về sớm 30 phút",
			online:    480,
			first:     at(day, 8, 0),
			last:      at(day, 17, 0),
			wantEarly: 30,
		},
		{
			name:          "thiếu giờ",
			online:        300,
			first:         at(day, 8, 0),
			last:          at(day, 17, 30),
			wantShortfall: 180,
		},
		{
			// Đến sớm KHÔNG được thành "đi muộn số âm".
			name:   "đến sớm",
			online: 480,
			first:  at(day, 7, 30),
			last:   at(day, 17, 30),
		},
		{
			// Về muộn KHÔNG được thành "về sớm số âm".
			name:   "về muộn",
			online: 540,
			first:  at(day, 8, 0),
			last:   at(day, 18, 30),
		},
	}

	h := newHarness(day)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &domainatt.Day{
				OnlineMinutes: c.online,
				FirstSeenAt:   c.first,
				LastSeenAt:    c.last,
			}
			h.uc.applyLateness(d, s, time.UTC)

			if d.LateMinutes != c.wantLate {
				t.Errorf("LateMinutes = %d, muốn %d", d.LateMinutes, c.wantLate)
			}
			if d.EarlyLeaveMinutes != c.wantEarly {
				t.Errorf("EarlyLeaveMinutes = %d, muốn %d",
					d.EarlyLeaveMinutes, c.wantEarly)
			}
			if d.ShortfallMinutes != c.wantShortfall {
				t.Errorf("ShortfallMinutes = %d, muốn %d",
					d.ShortfallMinutes, c.wantShortfall)
			}
		})
	}
}

func TestApplyLatenessNilScheduleIsSafe(t *testing.T) {
	// Chưa cấu hình khung giờ nào là trạng thái hợp lệ của hệ thống mới dựng.
	// Nó phải không tính gì cả, không được panic.
	h := newHarness(time.Now())
	d := &domainatt.Day{OnlineMinutes: 100}

	h.uc.applyLateness(d, nil, time.UTC)

	if d.LateMinutes != 0 || d.ShortfallMinutes != 0 {
		t.Error("không có khung giờ thì không được tính đi muộn hay thiếu giờ")
	}
}

// =========================================================================
// CHỌN KHUNG GIỜ
// =========================================================================

// TestResolveScheduleHonoursPriority: cá nhân > phòng ban > công ty; cùng mức
// thì lấy cái có hiệu lực muộn nhất.
func TestResolveScheduleHonoursPriority(t *testing.T) {
	day := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	id := uuid.New()

	company := officeHours()
	company.Name = "công ty"
	company.EffectiveFrom = day.AddDate(-1, 0, 0)

	dept := officeHours()
	dept.Name = "phòng ban"
	dept.DepartmentID = &id
	dept.EffectiveFrom = day.AddDate(-1, 0, 0)

	emp := officeHours()
	emp.Name = "cá nhân"
	emp.EmployeeID = &id
	emp.EffectiveFrom = day.AddDate(-1, 0, 0)

	h := newHarness(day)

	// Cố ý xếp mức hẹp nhất ở GIỮA để bài kiểm thử không đạt chỉ vì thuật
	// toán lấy phần tử đầu hoặc phần tử cuối.
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{company, emp, dept}
	}

	got, err := h.uc.resolveSchedule(context.Background(), id, day)
	if err != nil {
		t.Fatalf("resolveSchedule lỗi: %v", err)
	}
	if got.Name != "cá nhân" {
		t.Errorf("chọn khung giờ %q, muốn \"cá nhân\"", got.Name)
	}
}

func TestResolveScheduleLatestWinsAtSameLevel(t *testing.T) {
	day := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)

	old := officeHours()
	old.Name = "cũ"
	old.EffectiveFrom = day.AddDate(-2, 0, 0)

	recent := officeHours()
	recent.Name = "mới"
	recent.EffectiveFrom = day.AddDate(0, -1, 0)

	h := newHarness(day)
	h.schedules.applicable = func(uuid.UUID, time.Time) []*domainatt.Schedule {
		return []*domainatt.Schedule{recent, old}
	}

	got, err := h.uc.resolveSchedule(context.Background(), uuid.New(), day)
	if err != nil {
		t.Fatalf("resolveSchedule lỗi: %v", err)
	}
	if got.Name != "mới" {
		t.Errorf("chọn khung giờ %q, muốn \"mới\"", got.Name)
	}
}

func TestResolveScheduleNoneConfigured(t *testing.T) {
	h := newHarness(time.Now())

	got, err := h.uc.resolveSchedule(context.Background(), uuid.New(), time.Now())
	if err != nil {
		t.Fatalf("resolveSchedule lỗi: %v", err)
	}
	if got != nil {
		t.Error("chưa cấu hình khung giờ nào thì phải trả nil, không phải giá trị mặc định ngầm")
	}
}
