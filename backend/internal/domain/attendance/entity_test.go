package attendance

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func schedule(start, end string, breakMins, grace int, workdays ...int16) *Schedule {
	if len(workdays) == 0 {
		workdays = []int16{1, 2, 3, 4, 5}
	}
	return &Schedule{
		WorkStart:    start,
		WorkEnd:      end,
		BreakMinutes: breakMins,
		GraceMinutes: grace,
		Workdays:     workdays,
	}
}

func TestParseClockAcceptsBothFormats(t *testing.T) {
	// Cột giờ trong PostgreSQL trả về "08:00:00", còn biểu mẫu gửi lên
	// "08:00". Chỉ đỡ một dạng là nửa hệ thống tính ra 0 phút.
	cases := map[string]int{
		"08:00":    8 * 60,
		"08:00:00": 8 * 60,
		"17:30":    17*60 + 30,
		"00:00":    0,
		"23:59":    23*60 + 59,
	}

	for in, want := range cases {
		got, err := parseClock(in)
		if err != nil {
			t.Errorf("parseClock(%q) lỗi: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseClock(%q) = %d, muốn %d", in, got, want)
		}
	}

	if _, err := parseClock("khong-phai-gio"); err == nil {
		t.Error("chuỗi rác phải trả lỗi")
	}
}

func TestExpectedMinutes(t *testing.T) {
	cases := []struct {
		name string
		s    *Schedule
		want int
	}{
		{"ca hành chính trừ nghỉ trưa", schedule("08:00", "17:30", 90, 0), 480},
		{"không có giờ nghỉ", schedule("08:00", "12:00", 0, 0), 240},

		// Ba trường hợp dữ liệu hỏng dưới đây đều phải trả 0 thay vì một con
		// số âm hoặc một lần panic: khung giờ là dữ liệu người dùng nhập, và
		// một số âm lọt vào bảng công sẽ lan sang cả tính lương.
		{"giờ kết thúc trước giờ bắt đầu", schedule("17:00", "08:00", 0, 0), 0},
		{"giờ nghỉ dài hơn cả ca", schedule("08:00", "09:00", 120, 0), 0},
		{"giờ không đọc được", schedule("rác", "17:30", 0, 0), 0},
	}

	for _, c := range cases {
		if got := c.s.ExpectedMinutes(); got != c.want {
			t.Errorf("%s: ExpectedMinutes() = %d, muốn %d", c.name, got, c.want)
		}
	}
}

// TestIsWorkdayWeekdayConvention là phép thử chống lỗi lệch đúng một ngày.
//
// Cột workdays dùng ISODOW (1 = thứ hai, 7 = chủ nhật), còn time.Weekday của
// Go trả 0 = chủ nhật. Trộn hai quy ước là loại lỗi rất khó nhìn ra khi đọc
// code, và nó làm cả hệ thống lệch đúng một ngày.
func TestIsWorkdayWeekdayConvention(t *testing.T) {
	monToFri := schedule("08:00", "17:30", 90, 0, 1, 2, 3, 4, 5)

	// 2026-09-21 là thứ hai.
	monday := time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
	if monday.Weekday() != time.Monday {
		t.Fatalf("mốc kiểm thử sai: 2026-09-21 phải là thứ hai, đang là %s", monday.Weekday())
	}

	for i, want := range []bool{
		true,  // thứ hai
		true,  // thứ ba
		true,  // thứ tư
		true,  // thứ năm
		true,  // thứ sáu
		false, // thứ bảy
		false, // chủ nhật
	} {
		d := monday.AddDate(0, 0, i)
		if got := monToFri.IsWorkday(d); got != want {
			t.Errorf("%s (%s): IsWorkday = %v, muốn %v",
				d.Format("2006-01-02"), d.Weekday(), got, want)
		}
	}
}

func TestIsWorkdayIncludesSunday(t *testing.T) {
	// Ca gồm chủ nhật: nếu quy ước ISODOW bị hiểu sai thành 0, giá trị 7
	// trong workdays sẽ không bao giờ khớp.
	sundayShift := schedule("08:00", "12:00", 0, 0, 7)
	sunday := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)

	if sunday.Weekday() != time.Sunday {
		t.Fatalf("mốc kiểm thử sai: 2026-09-27 phải là chủ nhật")
	}
	if !sundayShift.IsWorkday(sunday) {
		t.Error("ca chủ nhật phải coi chủ nhật là ngày làm việc")
	}
}

// TestSpecificityOrdering khoá lại luật ưu tiên cá nhân > phòng ban > công ty.
//
// Đảo thứ tự này nghĩa là khung giờ riêng của một người bị khung giờ chung của
// công ty ghi đè — đúng ngược với điều người cấu hình mong đợi.
func TestSpecificityOrdering(t *testing.T) {
	id := uuid.New()

	company := schedule("08:00", "17:30", 90, 0)
	dept := schedule("08:00", "17:30", 90, 0)
	dept.DepartmentID = &id
	emp := schedule("08:00", "17:30", 90, 0)
	emp.EmployeeID = &id

	if !(emp.Specificity() > dept.Specificity() &&
		dept.Specificity() > company.Specificity()) {
		t.Fatalf("thứ tự ưu tiên sai: cá nhân=%d, phòng ban=%d, công ty=%d",
			emp.Specificity(), dept.Specificity(), company.Specificity())
	}

	// Khung giờ của một người CỤ THỂ trong một phòng ban vẫn là mức cá nhân:
	// trường hẹp nhất quyết định, không phải trường được điền sau cùng.
	both := schedule("08:00", "17:30", 90, 0)
	both.DepartmentID = &id
	both.EmployeeID = &id
	if both.Scope() != "employee" {
		t.Errorf("có cả hai trường thì Scope() = %q, muốn \"employee\"", both.Scope())
	}
}

func TestSessionMinutes(t *testing.T) {
	base := time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)

	s := &Session{StartedAt: base, EndedAt: base.Add(95 * time.Minute)}
	if got := s.Minutes(); got != 95 {
		t.Errorf("Minutes() = %d, muốn 95", got)
	}

	// Phiên kết thúc trước khi bắt đầu là dữ liệu hỏng. Database đã chặn nó
	// bằng chk_attendance_sessions_range, nên đây là lớp phòng thủ thứ hai
	// cho phiên dựng trong bộ nhớ lúc gộp: một số phút âm cộng vào tổng của
	// cả ngày sẽ làm bảng công sai một cách im lặng.
	bad := &Session{StartedAt: base, EndedAt: base.Add(-time.Hour)}
	if got := bad.Minutes(); got < 0 {
		t.Errorf("phiên hỏng cho số phút âm: %d", got)
	}
}
