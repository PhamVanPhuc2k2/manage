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

// Bộ kiểm thử các màn hình đọc bảng công.
//
// Phần lớn các luật ở đây sinh ra từ một nhận xét: bảng công ngày chỉ được
// job tổng hợp ghi, nên TRONG LÚC đang làm việc nó vẫn là dữ liệu của hôm
// qua. Mỗi màn hình phải tự xử lý khoảng trống đó, nếu không người dùng nhìn
// thấy số 0 vào đúng lúc họ cần con số nhất.

// =========================================================================
// WIDGET HÔM NAY
// =========================================================================

// TestTodayReadsFromSessionsNotDayTable.
//
// Đọc thẳng từ bảng phiên chứ không từ bảng công ngày. Đọc bảng công ngày
// thì widget hiện số của hôm qua suốt cả ngày hôm nay.
func TestTodayReadsFromSessionsNotDayTable(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()
	first := monday.Add(8 * time.Hour)

	h := newHarness(monday)
	h.sessions.aggregate = func(uuid.UUID, time.Time) (int, int, *time.Time, *time.Time) {
		return 300, 240, &first, nil
	}

	got, err := h.uc.Today(context.Background(), leaveActor(empID))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.OnlineMinutes != 300 || got.ActiveMinutes != 240 {
		t.Errorf("online/hoạt động = %d/%d, muốn 300/240",
			got.OnlineMinutes, got.ActiveMinutes)
	}
	if got.FirstSeenAt == nil || !got.FirstSeenAt.Equal(first) {
		t.Errorf("lần đầu thấy = %v, muốn %v", got.FirstSeenAt, first)
	}
}

// TestTodayDefaultsToOffline: không có thông tin hiện diện thì hiện
// "offline", không để trống. Một ô trạng thái rỗng trông như lỗi giao diện.
func TestTodayDefaultsToOffline(t *testing.T) {
	h := newHarness(mondayOf())

	got, err := h.uc.Today(context.Background(), leaveActor(uuid.New()))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Status != "offline" {
		t.Errorf("trạng thái = %q, muốn offline", got.Status)
	}
}

func TestTodayUsesPresenceStatus(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())
	h.presence.status = map[uuid.UUID]string{empID: "online"}

	got, err := h.uc.Today(context.Background(), leaveActor(empID))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Status != "online" {
		t.Errorf("trạng thái = %q, muốn online", got.Status)
	}
}

// TestTodayIncludesExpectedMinutes: con số "đã làm 240/480 phút" chỉ có
// nghĩa khi biết mẫu số, và mẫu số đến từ khung giờ áp dụng cho người đó.
func TestTodayIncludesExpectedMinutes(t *testing.T) {
	h := newHarness(mondayOf())
	h.withOfficeHours()

	got, err := h.uc.Today(context.Background(), leaveActor(uuid.New()))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.ExpectedMins <= 0 {
		t.Errorf("số phút dự kiến = %d, muốn lớn hơn 0", got.ExpectedMins)
	}
}

func TestTodayRequiresActor(t *testing.T) {
	h := newHarness(mondayOf())

	_, err := h.uc.Today(context.Background(), nil)
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

// =========================================================================
// DANH SÁCH NGÀY
// =========================================================================

func TestListDaysRejectsBadRange(t *testing.T) {
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

			_, err := h.uc.ListDays(context.Background(), leaveActor(uuid.New()),
				domainatt.DayFilter{From: tc.from, To: tc.to})

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
		})
	}
}

// TestListDaysIgnoresScopeFromQueryString là phép thử chống giả mạo, giống
// bên nghỉ phép và bên nhân sự: hai trường phạm vi nằm cùng struct với các
// trường lọc thường nên phải bị xoá trước khi usecase tự đặt lại.
func TestListDaysIgnoresScopeFromQueryString(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)

	forged := domainatt.DayFilter{
		From:              monday,
		To:                monday.AddDate(0, 0, 7),
		ScopedEmployeeIDs: []uuid.UUID{uuid.New()},
		RestrictScope:     false,
	}

	if _, err := h.uc.ListDays(
		context.Background(), leaveActor(empID), forged,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.days.lastFilter
	if !f.RestrictScope {
		t.Error("phạm vi giả mạo đã tắt được giới hạn")
	}
	if len(f.ScopedEmployeeIDs) != 1 || f.ScopedEmployeeIDs[0] != empID {
		t.Errorf("phạm vi = %v, muốn [%v]", f.ScopedEmployeeIDs, empID)
	}
}

func TestListDaysOfOtherPersonNeedsScope(t *testing.T) {
	monday := mondayOf()
	other := uuid.New()
	h := newHarness(monday)

	_, err := h.uc.ListDays(context.Background(), leaveActor(uuid.New()),
		domainatt.DayFilter{From: monday, To: monday, EmployeeID: &other})

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404 (403 sẽ xác nhận người này tồn tại)", got)
	}
}

// =========================================================================
// CHI TIẾT MỘT NGÀY
// =========================================================================

// TestGetDayFallsBackToSessions.
//
// Ngày chưa được tổng hợp — hôm nay, hoặc job chưa chạy — phải dựng một bản
// tạm từ chính các phiên. Trả 404 sẽ khiến màn hình chấm công của HÔM NAY
// luôn trống, đúng lúc người dùng cần nó nhất.
func TestGetDayFallsBackToSessions(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	h.sessions.aggregate = func(uuid.UUID, time.Time) (int, int, *time.Time, *time.Time) {
		return 300, 240, nil, nil
	}

	got, err := h.uc.GetDay(
		context.Background(), leaveActor(empID), empID, monday)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Day.OnlineMinutes != 300 {
		t.Errorf("số phút online = %d, muốn 300", got.Day.OnlineMinutes)
	}
	if got.Day.Status != domainatt.DayPresent {
		t.Errorf("trạng thái = %q, muốn %q", got.Day.Status, domainatt.DayPresent)
	}
}

// TestGetDayFallbackWithNoSessionsIsAbsent: bản tạm không có phiên nào là
// vắng mặt, không phải có mặt với 0 phút.
func TestGetDayFallbackWithNoSessionsIsAbsent(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()
	h := newHarness(monday)

	got, err := h.uc.GetDay(
		context.Background(), leaveActor(empID), empID, monday)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Day.Status != domainatt.DayAbsent {
		t.Errorf("trạng thái = %q, muốn %q", got.Day.Status, domainatt.DayAbsent)
	}
}

// TestGetDayPrefersStoredRow: đã tổng hợp rồi thì dùng con số đã chốt, vì
// nó mới là con số dùng để tính lương.
func TestGetDayPrefersStoredRow(t *testing.T) {
	monday := mondayOf()
	empID := uuid.New()

	h := newHarness(monday)
	h.days.setDay(empID, monday, &domainatt.Day{
		EmployeeID: empID, WorkDate: monday,
		OnlineMinutes: 480, Status: domainatt.DayPresent,
	})
	h.sessions.aggregate = func(uuid.UUID, time.Time) (int, int, *time.Time, *time.Time) {
		return 1, 1, nil, nil
	}

	got, err := h.uc.GetDay(
		context.Background(), leaveActor(empID), empID, monday)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Day.OnlineMinutes != 480 {
		t.Errorf("số phút online = %d, muốn 480 (con số đã chốt)",
			got.Day.OnlineMinutes)
	}
}

func TestGetDayOfOtherPersonNeedsScope(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday)

	_, err := h.uc.GetDay(context.Background(),
		leaveActor(uuid.New()), uuid.New(), monday)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// TỔNG HỢP THÁNG
// =========================================================================

func TestMonthSummaryRejectsBadPeriod(t *testing.T) {
	cases := []struct {
		name  string
		year  int
		month int
	}{
		{"tháng 0", 2026, 0},
		{"tháng 13", 2026, 13},
		{"năm quá xa quá khứ", 1999, 1},
		{"năm quá xa tương lai", 2201, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(mondayOf())

			_, err := h.uc.MonthSummary(context.Background(),
				leaveActor(uuid.New()), tc.year, tc.month, nil, nil)

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
		})
	}
}

// TestMonthSummaryCoversWholeMonth: khoảng phải là ngày 1 tới ngày cuối
// tháng. Lệch một ngày là thiếu công của cả một ngày trong phiếu lương.
func TestMonthSummaryCoversWholeMonth(t *testing.T) {
	h := newHarness(mondayOf())

	if _, err := h.uc.MonthSummary(context.Background(),
		leaveActor(uuid.New(), domainauth.PermAttendanceReadAll),
		2026, 2, nil, nil,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.days.lastFilter
	if f.From.Format("2006-01-02") != "2026-02-01" {
		t.Errorf("từ ngày = %s, muốn 2026-02-01", f.From.Format("2006-01-02"))
	}
	// 2026 không phải năm nhuận, nên tháng Hai có 28 ngày.
	if f.To.Format("2006-01-02") != "2026-02-28" {
		t.Errorf("tới ngày = %s, muốn 2026-02-28", f.To.Format("2006-01-02"))
	}
}

func TestMonthSummaryAppliesScope(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())

	if _, err := h.uc.MonthSummary(context.Background(),
		leaveActor(empID), 2026, 9, nil, nil,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.days.lastFilter
	if !f.RestrictScope || len(f.ScopedEmployeeIDs) != 1 ||
		f.ScopedEmployeeIDs[0] != empID {
		t.Errorf("phạm vi = %v (giới hạn %v), muốn chỉ [%v]",
			f.ScopedEmployeeIDs, f.RestrictScope, empID)
	}
}

func TestMonthSummaryOfOtherPersonNeedsScope(t *testing.T) {
	other := uuid.New()
	h := newHarness(mondayOf())

	_, err := h.uc.MonthSummary(context.Background(),
		leaveActor(uuid.New()), 2026, 9, &other, nil)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// AI ĐANG ONLINE
// =========================================================================

// TestTeamPresenceReturnsStatusOnly.
//
// Màn hình này cố ý KHÔNG trả về số giờ. Nó để biết ai đang có mặt mà hỏi
// việc, không phải công cụ theo dõi. Ai muốn xem số liệu công thì vào bảng
// công, nơi con số được ghi rõ và có đường điều chỉnh.
func TestTeamPresenceReturnsStatusOnly(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())
	h.presence.status = map[uuid.UUID]string{empID: "idle"}

	got, err := h.uc.TeamPresence(context.Background(), leaveActor(empID))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("số dòng = %d, muốn 1", len(got))
	}
	if got[0].Status != "idle" {
		t.Errorf("trạng thái = %q, muốn idle", got[0].Status)
	}
}

func TestTeamPresenceDefaultsToOffline(t *testing.T) {
	empID := uuid.New()
	h := newHarness(mondayOf())

	got, err := h.uc.TeamPresence(context.Background(), leaveActor(empID))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 || got[0].Status != "offline" {
		t.Errorf("kết quả = %+v, muốn một dòng offline", got)
	}
}

// TestTeamPresenceScopeAllListsEveryone: phạm vi toàn công ty không có danh
// sách id sẵn, nên phải đi lấy toàn bộ nhân viên đang làm việc.
func TestTeamPresenceScopeAllListsEveryone(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	h := newHarness(mondayOf())
	h.employees.activeIDs = []uuid.UUID{a, b}

	got, err := h.uc.TeamPresence(context.Background(),
		leaveActor(uuid.New(), domainauth.PermAttendanceReadAll))
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("số dòng = %d, muốn 2", len(got))
	}
}

func TestTeamPresenceRequiresActor(t *testing.T) {
	h := newHarness(mondayOf())

	_, err := h.uc.TeamPresence(context.Background(), nil)
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

// =========================================================================
// GHI PHIÊN THỦ CÔNG
// =========================================================================

func TestCheckInRejectsBadInput(t *testing.T) {
	monday := mondayOf()
	now := monday.Add(14 * time.Hour)
	start := monday.Add(9 * time.Hour)

	cases := []struct {
		name  string
		start time.Time
		end   time.Time
		want  int
	}{
		{"hợp lệ", start, start.Add(2 * time.Hour), http.StatusOK},
		{"kết thúc trước bắt đầu", start, start.Add(-time.Hour), http.StatusBadRequest},
		{"kết thúc trùng bắt đầu", start, start, http.StatusBadRequest},
		{"quá 24 giờ", start, start.Add(25 * time.Hour), http.StatusBadRequest},
		{"trong tương lai", now.Add(time.Hour), now.Add(2 * time.Hour), http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(now)

			_, err := h.uc.CheckIn(context.Background(),
				leaveActor(uuid.New()), tc.start, tc.end, "họp ngoài")

			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d (%v)", got, tc.want, err)
			}
		})
	}
}

// TestCheckInAllowsSmallClockSkew: đồng hồ máy khách lệch vài phút so với
// máy chủ là chuyện thường. Từ chối cứng sẽ tạo ra lỗi ngẫu nhiên mà người
// dùng không hiểu và không tự khắc phục được.
func TestCheckInAllowsSmallClockSkew(t *testing.T) {
	now := mondayOf().Add(14 * time.Hour)
	h := newHarness(now)

	if _, err := h.uc.CheckIn(context.Background(), leaveActor(uuid.New()),
		now.Add(2*time.Minute), now.Add(time.Hour), "",
	); err != nil {
		t.Fatalf("lệch đồng hồ vài phút không được bị từ chối: %v", err)
	}
}

func TestCheckInMarksSessionManual(t *testing.T) {
	monday := mondayOf()
	now := monday.Add(14 * time.Hour)
	h := newHarness(now)

	got, err := h.uc.CheckIn(context.Background(), leaveActor(uuid.New()),
		monday.Add(9*time.Hour), monday.Add(11*time.Hour), "họp ở ngoài")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.Source != domainatt.SourceManual {
		t.Errorf("nguồn = %q, muốn %q — người duyệt phải phân biệt được với dữ liệu tự động",
			got.Source, domainatt.SourceManual)
	}
	if got.ActiveMinutes != 120 {
		t.Errorf("số phút = %d, muốn 120", got.ActiveMinutes)
	}
}

// TestCheckInRejectsLockedDay: thêm phiên vào ngày đã khoá thì tổng hợp
// không chạy lại được nữa, và bảng công sẽ mâu thuẫn với chính các phiên
// tạo ra nó.
func TestCheckInRejectsLockedDay(t *testing.T) {
	monday := mondayOf()
	now := monday.Add(14 * time.Hour)
	empID := uuid.New()

	h := newHarness(now)
	h.days.setDay(empID, monday, &domainatt.Day{
		EmployeeID: empID, WorkDate: monday, IsLocked: true,
	})

	_, err := h.uc.CheckIn(context.Background(), leaveActor(empID),
		monday.Add(9*time.Hour), monday.Add(11*time.Hour), "")

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.sessions.created) != 0 {
		t.Error("đã ghi phiên vào ngày đã khoá")
	}
}

func TestCheckInRequiresActor(t *testing.T) {
	monday := mondayOf()
	h := newHarness(monday.Add(14 * time.Hour))

	_, err := h.uc.CheckIn(context.Background(), nil,
		monday.Add(9*time.Hour), monday.Add(11*time.Hour), "")

	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}
