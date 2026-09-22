package attendance

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
)

// Bộ kiểm thử phần quản trị chấm công: yêu cầu điều chỉnh, khoá kỳ công,
// khung giờ làm việc và ngày lễ.
//
// Điểm chung của cả bốn: chúng sửa được con số dùng để tính lương. Mỗi luật
// ở đây tồn tại để một sai lệch không đi thẳng vào phiếu lương của ai đó.

func validScheduleInput() ScheduleInput {
	return ScheduleInput{
		Name:         "Hành chính",
		WorkStart:    "08:00",
		WorkEnd:      "17:30",
		Workdays:     []int16{1, 2, 3, 4, 5},
		BreakMinutes: 90,
		GraceMinutes: 15,
	}
}

// =========================================================================
// YÊU CẦU ĐIỀU CHỈNH CÔNG
// =========================================================================

func TestCreateAdjustmentRejectsBadInput(t *testing.T) {
	monday := mondayOf()
	start := monday.Add(9 * time.Hour)

	cases := []struct {
		name   string
		start  time.Time
		end    time.Time
		reason string
		want   int
	}{
		{"hợp lệ", start, start.Add(2 * time.Hour), "máy hỏng", http.StatusOK},
		{"kết thúc trước bắt đầu", start, start.Add(-time.Hour), "máy hỏng", http.StatusBadRequest},
		{"kết thúc trùng bắt đầu", start, start, "máy hỏng", http.StatusBadRequest},
		{"quá 24 giờ", start, start.Add(25 * time.Hour), "máy hỏng", http.StatusBadRequest},
		{"không nêu lý do", start, start.Add(2 * time.Hour), "   ", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(monday)

			_, err := h.uc.CreateAdjustment(context.Background(),
				leaveActor(uuid.New()), tc.start, tc.end, tc.reason)

			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d (%v)", got, tc.want, err)
			}
		})
	}
}

func TestCreateAdjustmentRequiresActor(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)

	_, err := h.uc.CreateAdjustment(context.Background(), nil,
		monday.Add(9*time.Hour), monday.Add(11*time.Hour), "máy hỏng")

	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

// TestCreateAdjustmentRejectsLockedDay: kỳ công đã khoá là để chốt lương.
// Nhận thêm yêu cầu cho ngày đó là hứa một điều không thể thực hiện — duyệt
// xong cũng không đổi được bảng công nữa.
func TestCreateAdjustmentRejectsLockedDay(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	h.days.setDay(empID, monday, &domainatt.Day{
		EmployeeID: empID, WorkDate: monday, IsLocked: true,
	})

	_, err := h.uc.CreateAdjustment(context.Background(), leaveActor(empID),
		monday.Add(9*time.Hour), monday.Add(11*time.Hour), "máy hỏng")

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.adjustments.created) != 0 {
		t.Error("đã tạo yêu cầu cho ngày đã khoá")
	}
}

func TestCreateAdjustmentTrimsReason(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)

	if _, err := h.uc.CreateAdjustment(context.Background(),
		leaveActor(uuid.New()),
		monday.Add(9*time.Hour), monday.Add(11*time.Hour), "  máy hỏng  ",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.adjustments.created[0].Reason; got != "máy hỏng" {
		t.Errorf("lý do = %q, khoảng trắng chưa được cắt", got)
	}
}

// TestApproveAdjustmentWritesSessionNotTotals.
//
// Duyệt thì GHI THÊM một phiên có nguồn 'adjustment' rồi tổng hợp lại, chứ
// không sửa thẳng con số tổng. Bảng công phải luôn truy ngược được về các
// phiên tạo ra nó — khi có tranh cãi về lương, "hệ thống tính ra thế" không
// phải là một câu trả lời.
func TestApproveAdjustmentWritesSessionNotTotals(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	a := h.adjustments.add(&domainatt.Adjustment{
		EmployeeID:     empID,
		WorkDate:       monday,
		RequestedStart: monday.Add(9 * time.Hour),
		RequestedEnd:   monday.Add(11 * time.Hour),
		Reason:         "máy hỏng",
		Status:         domainatt.StatusPending,
	})

	if _, err := h.uc.DecideAdjustment(context.Background(),
		leaveActor(uuid.New(), domainauth.PermAttendanceReadAll),
		a.ID, true, "đã xác minh",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.adjustments.byID[a.ID].Status; got != domainatt.StatusApproved {
		t.Errorf("trạng thái = %q, muốn %q", got, domainatt.StatusApproved)
	}
	if len(h.sessions.created) != 1 {
		t.Fatalf("số phiên đã ghi = %d, muốn 1", len(h.sessions.created))
	}

	s := h.sessions.created[0]
	if s.Source != domainatt.SourceAdjustment {
		t.Errorf("nguồn phiên = %q, muốn %q", s.Source, domainatt.SourceAdjustment)
	}
	if s.ActiveMinutes != 120 {
		t.Errorf("số phút = %d, muốn 120", s.ActiveMinutes)
	}
}

func TestRejectAdjustmentWritesNoSession(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	a := h.adjustments.add(&domainatt.Adjustment{
		EmployeeID:     uuid.New(),
		WorkDate:       monday,
		RequestedStart: monday.Add(9 * time.Hour),
		RequestedEnd:   monday.Add(11 * time.Hour),
		Status:         domainatt.StatusPending,
	})

	if _, err := h.uc.DecideAdjustment(context.Background(),
		leaveActor(uuid.New(), domainauth.PermAttendanceReadAll),
		a.ID, false, "không có bằng chứng",
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.adjustments.byID[a.ID].Status; got != domainatt.StatusRejected {
		t.Errorf("trạng thái = %q, muốn %q", got, domainatt.StatusRejected)
	}
	if len(h.sessions.created) != 0 {
		t.Error("từ chối mà vẫn ghi phiên — giờ công được cộng cho một yêu cầu bị bác")
	}
}

func TestCannotApproveOwnAdjustment(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	a := h.adjustments.add(&domainatt.Adjustment{
		EmployeeID:     empID,
		WorkDate:       monday,
		RequestedStart: monday.Add(9 * time.Hour),
		RequestedEnd:   monday.Add(11 * time.Hour),
		Status:         domainatt.StatusPending,
	})

	_, err := h.uc.DecideAdjustment(context.Background(),
		leaveActor(empID, domainauth.PermAttendanceReadAll), a.ID, true, "")

	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
	if len(h.sessions.created) != 0 {
		t.Error("đã tự cộng giờ công cho chính mình")
	}
}

func TestCannotDecideAdjustmentTwice(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	a := h.adjustments.add(&domainatt.Adjustment{
		EmployeeID: uuid.New(), WorkDate: monday,
		RequestedStart: monday.Add(9 * time.Hour),
		RequestedEnd:   monday.Add(11 * time.Hour),
		Status:         domainatt.StatusApproved,
	})

	_, err := h.uc.DecideAdjustment(context.Background(),
		leaveActor(uuid.New(), domainauth.PermAttendanceReadAll), a.ID, true, "")

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.sessions.created) != 0 {
		t.Error("duyệt lần hai đã cộng giờ công thêm một lần nữa")
	}
}

func TestDecideMissingAdjustmentReturns404(t *testing.T) {
	h := newHarness(mondayOf())

	_, err := h.uc.DecideAdjustment(context.Background(),
		leaveActor(uuid.New(), domainauth.PermAttendanceReadAll),
		uuid.New(), true, "")

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestListAdjustmentsAppliesScope: phạm vi được áp bằng cách truyền xuống
// repository, nên đây là chỗ duy nhất kiểm chứng được.
func TestListAdjustmentsAppliesScope(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())

	if _, err := h.uc.ListAdjustments(
		context.Background(), leaveActor(empID), nil,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !h.adjustments.lastRestrict {
		t.Error("người không có quyền xem công người khác phải bị giới hạn phạm vi")
	}
	if len(h.adjustments.lastIDs) != 1 || h.adjustments.lastIDs[0] != empID {
		t.Errorf("phạm vi = %v, muốn [%v]", h.adjustments.lastIDs, empID)
	}
}

func TestListAdjustmentsFiltersByStatus(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	h.adjustments.add(&domainatt.Adjustment{
		EmployeeID: uuid.New(), WorkDate: monday, Status: domainatt.StatusPending,
	})
	h.adjustments.add(&domainatt.Adjustment{
		EmployeeID: uuid.New(), WorkDate: monday, Status: domainatt.StatusApproved,
	})

	pending := domainatt.StatusPending
	got, err := h.uc.ListAdjustments(context.Background(),
		leaveActor(uuid.New(), domainauth.PermAttendanceReadAll), &pending)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("số yêu cầu = %d, muốn 1", len(got))
	}
}

// =========================================================================
// KHOÁ KỲ CÔNG
// =========================================================================

func TestLockPeriod(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	h.days.lockedRows = 42

	n, err := h.uc.LockPeriod(
		context.Background(), monday, monday.AddDate(0, 0, 30), true)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if n != 42 {
		t.Errorf("số dòng bị khoá = %d, muốn 42", n)
	}
	if h.days.lastLock == nil || !*h.days.lastLock {
		t.Error("cờ khoá chưa được truyền xuống")
	}
}

func TestLockPeriodRejectsBadRange(t *testing.T) {
	monday := mondayOf()

	cases := []struct {
		name string
		from time.Time
		to   time.Time
	}{
		{"kết thúc trước bắt đầu", monday, monday.AddDate(0, 0, -1)},
		{"khoảng quá dài", monday, monday.AddDate(0, 0, 401)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(monday)

			_, err := h.uc.LockPeriod(context.Background(), tc.from, tc.to, true)
			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if h.days.lastLock != nil {
				t.Error("đã gọi khoá dù khoảng ngày không hợp lệ")
			}
		})
	}
}

// =========================================================================
// KHUNG GIỜ LÀM VIỆC
// =========================================================================

func TestValidateScheduleInput(t *testing.T) {
	deptID, empID := uuid.New(), uuid.New()

	cases := []struct {
		name   string
		mutate func(*ScheduleInput)
		want   int
	}{
		{"hợp lệ", func(*ScheduleInput) {}, http.StatusOK},
		{"thiếu tên", func(in *ScheduleInput) { in.Name = "  " }, http.StatusBadRequest},
		{
			"vừa phòng ban vừa cá nhân",
			func(in *ScheduleInput) { in.DepartmentID, in.EmployeeID = &deptID, &empID },
			http.StatusBadRequest,
		},
		{"nghỉ trưa âm", func(in *ScheduleInput) { in.BreakMinutes = -1 }, http.StatusBadRequest},
		{"nghỉ trưa quá 8 tiếng", func(in *ScheduleInput) { in.BreakMinutes = 8*60 + 1 }, http.StatusBadRequest},
		{"ân hạn âm", func(in *ScheduleInput) { in.GraceMinutes = -1 }, http.StatusBadRequest},
		{"ân hạn quá 2 tiếng", func(in *ScheduleInput) { in.GraceMinutes = 121 }, http.StatusBadRequest},
		{"ngày làm việc là 0", func(in *ScheduleInput) { in.Workdays = []int16{0} }, http.StatusBadRequest},
		{"ngày làm việc là 8", func(in *ScheduleInput) { in.Workdays = []int16{8} }, http.StatusBadRequest},
		{"giờ bắt đầu sai dạng", func(in *ScheduleInput) { in.WorkStart = "tám giờ" }, http.StatusBadRequest},
		{"kết thúc trước bắt đầu", func(in *ScheduleInput) { in.WorkEnd = "07:00" }, http.StatusBadRequest},
		{
			"chỉ áp cho phòng ban",
			func(in *ScheduleInput) { in.DepartmentID = &deptID },
			http.StatusOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(mondayOf())
			in := validScheduleInput()
			tc.mutate(&in)

			_, err := h.uc.CreateSchedule(context.Background(), in)
			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d (%v)", got, tc.want, err)
			}
		})
	}
}

// TestCreateScheduleDefaults: không khai ngày làm việc thì mặc định thứ hai
// tới thứ sáu, và không khai ngày hiệu lực thì tính từ hôm nay.
//
// Để trống hai trường này mà không có mặc định sẽ tạo ra một khung giờ không
// có ngày làm việc nào — mọi ngày thành cuối tuần và bảng công rỗng.
func TestCreateScheduleDefaults(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)

	in := validScheduleInput()
	in.Workdays = nil

	if _, err := h.uc.CreateSchedule(context.Background(), in); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	s := h.schedules.created[0]
	if len(s.Workdays) != 5 {
		t.Errorf("ngày làm việc = %v, muốn thứ hai tới thứ sáu", s.Workdays)
	}
	if !s.EffectiveFrom.Equal(monday) {
		t.Errorf("ngày hiệu lực = %v, muốn %v", s.EffectiveFrom, monday)
	}
}

func TestUpdateSchedule(t *testing.T) {
	h := newHarness(mondayOf())
	s := h.schedules.add(&domainatt.Schedule{
		Name: "Cũ", WorkStart: "08:00", WorkEnd: "17:00",
		Workdays: []int16{1, 2, 3, 4, 5},
	})

	in := validScheduleInput()
	in.Name = "Mới"
	in.GraceMinutes = 30

	got, err := h.uc.UpdateSchedule(context.Background(), s.ID, in)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Name != "Mới" || got.GraceMinutes != 30 {
		t.Errorf("chưa cập nhật: %q / %d phút ân hạn", got.Name, got.GraceMinutes)
	}
}

// TestUpdateScheduleKeepsWorkdaysWhenOmitted: danh sách rỗng nghĩa là "không
// đổi", không phải "không có ngày làm việc nào".
func TestUpdateScheduleKeepsWorkdaysWhenOmitted(t *testing.T) {
	h := newHarness(mondayOf())
	s := h.schedules.add(&domainatt.Schedule{
		Name: "Ca đêm", WorkStart: "08:00", WorkEnd: "17:00",
		Workdays: []int16{6, 7},
	})

	in := validScheduleInput()
	in.Workdays = nil

	got, err := h.uc.UpdateSchedule(context.Background(), s.ID, in)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got.Workdays) != 2 || got.Workdays[0] != 6 {
		t.Errorf("ngày làm việc = %v, muốn giữ nguyên [6 7]", got.Workdays)
	}
}

func TestUpdateMissingScheduleReturns404(t *testing.T) {
	h := newHarness(mondayOf())

	_, err := h.uc.UpdateSchedule(
		context.Background(), uuid.New(), validScheduleInput())

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestCannotDeleteCompanySchedule.
//
// Khung giờ công ty là mốc cuối cùng để tính đi muộn và thiếu giờ. Xoá đi
// thì mọi phép tính im lặng trả về 0 và bảng công trông vẫn bình thường
// trong khi đã mất hết ý nghĩa — không có lỗi nào để ai đó nhận ra.
func TestCannotDeleteCompanySchedule(t *testing.T) {
	h := newHarness(mondayOf())
	s := h.schedules.add(&domainatt.Schedule{
		Name: "Toàn công ty", WorkStart: "08:00", WorkEnd: "17:30",
	})

	err := h.uc.DeleteSchedule(context.Background(), s.ID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.schedules.deleted) != 0 {
		t.Error("khung giờ công ty đã bị xoá")
	}
}

func TestDeleteDepartmentSchedule(t *testing.T) {
	deptID := uuid.New()
	h := newHarness(mondayOf())
	s := h.schedules.add(&domainatt.Schedule{
		Name: "Phòng kỹ thuật", DepartmentID: &deptID,
		WorkStart: "09:00", WorkEnd: "18:00",
	})

	if err := h.uc.DeleteSchedule(context.Background(), s.ID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(h.schedules.deleted) != 1 {
		t.Error("khung giờ phòng ban chưa bị xoá")
	}
}

func TestDeleteMissingScheduleReturns404(t *testing.T) {
	h := newHarness(mondayOf())

	err := h.uc.DeleteSchedule(context.Background(), uuid.New())
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestListSchedules(t *testing.T) {
	h := newHarness(mondayOf())
	h.schedules.add(&domainatt.Schedule{Name: "Hành chính"})

	got, err := h.uc.ListSchedules(context.Background())
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("số khung giờ = %d, muốn 1", len(got))
	}
}

// =========================================================================
// NGÀY LỄ
// =========================================================================

// TestCreateHolidayTriggersRollup.
//
// Thêm ngày lễ phải tổng hợp lại ngày đó ngay: những người đã bị đánh dấu
// VẮNG MẶT vì hệ thống chưa biết đây là ngày lễ phải được sửa lại. Không làm
// thì họ mang dấu vắng mặt cho tới lúc chốt lương.
func TestCreateHolidayTriggersRollup(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)
	h.employees.activeIDs = []uuid.UUID{uuid.New()}

	got, err := h.uc.CreateHoliday(
		context.Background(), monday, "  Quốc khánh  ", true)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.Name != "Quốc khánh" {
		t.Errorf("tên = %q, khoảng trắng chưa được cắt", got.Name)
	}
	if len(h.days.upserted) == 0 {
		t.Error("chưa tổng hợp lại ngày vừa khai là ngày lễ")
	}
}

func TestCreateHolidayRejectsBlankName(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)

	_, err := h.uc.CreateHoliday(context.Background(), monday, "   ", true)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.holidays.created) != 0 {
		t.Error("đã tạo ngày lễ không tên")
	}
}

func TestDeleteMissingHolidayReturns404(t *testing.T) {
	h := newHarness(mondayOf())

	err := h.uc.DeleteHoliday(context.Background(), uuid.New())
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestDeleteHoliday(t *testing.T) {
	id := uuid.New()
	h := newHarness(mondayOf())
	h.holidays.known[id] = true

	if err := h.uc.DeleteHoliday(context.Background(), id); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(h.holidays.deleted) != 1 {
		t.Error("ngày lễ chưa bị xoá")
	}
}

// TestListHolidaysCoversWholeYear: khoảng truy vấn phải là 01-01 tới 31-12,
// không phải một phần năm.
func TestListHolidaysCoversWholeYear(t *testing.T) {
	var gotFrom, gotTo time.Time

	h := newHarness(mondayOf())
	h.holidays.between = func(from, to time.Time) []*domainatt.Holiday {
		gotFrom, gotTo = from, to
		return nil
	}

	if _, err := h.uc.ListHolidays(context.Background(), 2026); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if gotFrom.Format("2006-01-02") != "2026-01-01" {
		t.Errorf("từ ngày = %s, muốn 2026-01-01", gotFrom.Format("2006-01-02"))
	}
	if gotTo.Format("2006-01-02") != "2026-12-31" {
		t.Errorf("tới ngày = %s, muốn 2026-12-31", gotTo.Format("2006-01-02"))
	}
}
