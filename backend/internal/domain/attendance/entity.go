// Package attendance chứa entity và port của module chấm công và nghỉ phép.
package attendance

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound = errors.New("không tìm thấy")
	ErrLocked   = errors.New("kỳ công đã khoá")
)

// SessionGap là khoảng hở tối đa giữa hai lần quét để vẫn coi là một phiên.
//
// Mạng chớp, máy ngủ vài phút, đi họp rồi quay lại — nếu mỗi lần như vậy đều
// cắt thành phiên mới thì một ngày sinh ra hàng chục dòng và dòng thời gian
// trên giao diện thành vô nghĩa. 5 phút đủ rộng để bỏ qua nhiễu, đủ hẹp để
// một buổi họp ngoài văn phòng vẫn hiện thành khoảng trống thật.
const SessionGap = 5 * time.Minute

// =========================================================================
// KHUNG GIỜ LÀM VIỆC
// =========================================================================

type Schedule struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	DepartmentID *uuid.UUID
	EmployeeID   *uuid.UUID

	Name         string
	WorkStart    string // "08:00"
	WorkEnd      string // "17:30"
	Workdays     []int16
	BreakMinutes int
	GraceMinutes int

	EffectiveFrom time.Time
	EffectiveTo   *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Scope cho biết khung giờ này áp dụng ở mức nào. Mức hẹp hơn thắng.
func (s *Schedule) Scope() string {
	switch {
	case s.EmployeeID != nil:
		return "employee"
	case s.DepartmentID != nil:
		return "department"
	default:
		return "company"
	}
}

// Specificity dùng để chọn khung giờ thắng khi có nhiều khung cùng áp dụng.
func (s *Schedule) Specificity() int {
	switch s.Scope() {
	case "employee":
		return 3
	case "department":
		return 2
	default:
		return 1
	}
}

// IsWorkday cho biết một ngày có phải ngày làm việc theo khung giờ này không.
//
// Dùng ISODOW (1 = thứ hai) để khớp với dữ liệu lưu trong cột workdays.
// time.Weekday của Go trả 0 = chủ nhật, nên phải đổi — trộn hai quy ước là
// nguồn của lỗi lệch đúng một ngày, thứ rất khó nhìn ra khi đọc code.
func (s *Schedule) IsWorkday(d time.Time) bool {
	iso := int16(d.Weekday())
	if iso == 0 {
		iso = 7 // chủ nhật
	}
	for _, w := range s.Workdays {
		if w == iso {
			return true
		}
	}
	return false
}

// ExpectedMinutes là số phút làm việc chuẩn của một ngày, đã trừ giờ nghỉ.
func (s *Schedule) ExpectedMinutes() int {
	start, err1 := parseClock(s.WorkStart)
	end, err2 := parseClock(s.WorkEnd)
	if err1 != nil || err2 != nil || end <= start {
		return 0
	}
	n := end - start - s.BreakMinutes
	if n < 0 {
		return 0
	}
	return n
}

// StartMinutes / EndMinutes là mốc giờ tính bằng phút kể từ 00:00.
func (s *Schedule) StartMinutes() int { m, _ := parseClock(s.WorkStart); return m }
func (s *Schedule) EndMinutes() int   { m, _ := parseClock(s.WorkEnd); return m }

// parseClock đọc "HH:MM" hoặc "HH:MM:SS" thành số phút kể từ nửa đêm.
func parseClock(v string) (int, error) {
	t, err := time.Parse("15:04:05", v)
	if err != nil {
		t, err = time.Parse("15:04", v)
		if err != nil {
			return 0, err
		}
	}
	return t.Hour()*60 + t.Minute(), nil
}

// =========================================================================
// NGÀY LỄ
// =========================================================================

type Holiday struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	Date      time.Time
	Name      string
	IsPaid    bool
	CreatedAt time.Time
}

// =========================================================================
// PHIÊN LÀM VIỆC
// =========================================================================

type Source string

const (
	SourcePresence   Source = "presence"
	SourceManual     Source = "manual"
	SourceAdjustment Source = "adjustment"
)

type Session struct {
	ID         uuid.UUID
	EmployeeID uuid.UUID
	WorkDate   time.Time
	StartedAt  time.Time
	EndedAt    time.Time
	// ActiveMinutes là số phút CÓ hoạt động, luôn <= độ dài phiên.
	ActiveMinutes int
	Source        Source
	Note          string

	CreatedAt time.Time
	UpdatedAt time.Time

	EmployeeName string // JOIN lúc đọc
}

// Minutes là độ dài phiên, làm tròn xuống. Không bao giờ âm.
//
// Database đã có chk_attendance_sessions_range chặn phiên kết thúc trước khi
// bắt đầu, nên trường hợp này không xảy ra với dữ liệu đã lưu. Chặn thêm ở
// đây vì hàm này cũng được gọi trên phiên dựng trong bộ nhớ lúc gộp, và một
// số phút âm cộng vào tổng của cả ngày sẽ làm bảng công sai một cách im lặng
// — khác với lỗi ràng buộc, vốn báo ngay.
func (s *Session) Minutes() int {
	n := int(s.EndedAt.Sub(s.StartedAt).Minutes())
	if n < 0 {
		return 0
	}
	return n
}

// =========================================================================
// TỔNG HỢP NGÀY
// =========================================================================

type DayStatus string

const (
	DayPresent DayStatus = "present"
	DayAbsent  DayStatus = "absent"
	DayLeave   DayStatus = "leave"
	DayHoliday DayStatus = "holiday"
	DayWeekend DayStatus = "weekend"
)

type Day struct {
	ID         uuid.UUID
	EmployeeID uuid.UUID
	WorkDate   time.Time

	OnlineMinutes int
	ActiveMinutes int

	FirstSeenAt *time.Time
	LastSeenAt  *time.Time

	LateMinutes       int
	EarlyLeaveMinutes int
	ShortfallMinutes  int

	Status   DayStatus
	IsLocked bool

	CreatedAt time.Time
	UpdatedAt time.Time

	EmployeeName   string // JOIN lúc đọc
	DepartmentName string
}

// DayFilter gom điều kiện lọc bảng công.
type DayFilter struct {
	EmployeeID   *uuid.UUID
	DepartmentID *uuid.UUID
	From         time.Time
	To           time.Time

	// ScopedEmployeeIDs giới hạn kết quả trong những người actor được xem.
	// KHÔNG đến từ query string — usecase đặt theo phạm vi của actor.
	ScopedEmployeeIDs []uuid.UUID
	RestrictScope     bool
}

// MonthSummary là tổng hợp một tháng của một người.
type MonthSummary struct {
	EmployeeID   uuid.UUID
	EmployeeName string
	Year         int
	Month        int

	WorkdayCount  int
	PresentDays   int
	AbsentDays    int
	LeaveDays     int
	OnlineMinutes int
	ActiveMinutes int
	LateMinutes   int
	LateDays      int
	ShortfallMins int
}

// =========================================================================
// DUYỆT ĐƠN — dùng chung cho điều chỉnh công và nghỉ phép
// =========================================================================

type ApprovalStatus string

const (
	StatusPending   ApprovalStatus = "pending"
	StatusApproved  ApprovalStatus = "approved"
	StatusRejected  ApprovalStatus = "rejected"
	StatusCancelled ApprovalStatus = "cancelled"
)

func (s ApprovalStatus) Valid() bool {
	switch s {
	case StatusPending, StatusApproved, StatusRejected, StatusCancelled:
		return true
	}
	return false
}

// Final: đơn đã có kết luận, không sửa được nữa.
func (s ApprovalStatus) Final() bool { return s != StatusPending }

type Adjustment struct {
	ID         uuid.UUID
	EmployeeID uuid.UUID
	WorkDate   time.Time

	RequestedStart time.Time
	RequestedEnd   time.Time
	Reason         string

	Status       ApprovalStatus
	ApproverID   *uuid.UUID
	DecidedAt    *time.Time
	DecisionNote string

	CreatedAt time.Time
	UpdatedAt time.Time

	EmployeeName string // JOIN lúc đọc
	ApproverName string
}

// =========================================================================
// NGHỈ PHÉP
// =========================================================================

type LeaveType string

const (
	LeaveAnnual    LeaveType = "annual"
	LeaveSick      LeaveType = "sick"
	LeaveUnpaid    LeaveType = "unpaid"
	LeaveMaternity LeaveType = "maternity"
	LeaveOther     LeaveType = "other"
)

func (t LeaveType) Valid() bool {
	switch t {
	case LeaveAnnual, LeaveSick, LeaveUnpaid, LeaveMaternity, LeaveOther:
		return true
	}
	return false
}

// DeductsBalance cho biết loại phép này có trừ vào quỹ phép năm không.
//
// Chỉ phép năm trừ quỹ. Nghỉ ốm, nghỉ thai sản và nghỉ không lương đều có
// quy định riêng, gộp chung vào một quỹ là sai về nghiệp vụ.
func (t LeaveType) DeductsBalance() bool { return t == LeaveAnnual }

type DayPart string

const (
	PartFull      DayPart = "full"
	PartMorning   DayPart = "morning"
	PartAfternoon DayPart = "afternoon"
)

func (p DayPart) Valid() bool {
	switch p {
	case PartFull, PartMorning, PartAfternoon:
		return true
	}
	return false
}

type LeaveRequest struct {
	ID         uuid.UUID
	EmployeeID uuid.UUID

	Type      LeaveType
	StartDate time.Time
	EndDate   time.Time
	DayPart   DayPart
	Days      float64
	Reason    string

	Status       ApprovalStatus
	ApproverID   *uuid.UUID
	DecidedAt    *time.Time
	DecisionNote string

	CreatedAt time.Time
	UpdatedAt time.Time

	EmployeeName string // JOIN lúc đọc
	ApproverName string
}

type LeaveFilter struct {
	EmployeeID *uuid.UUID
	Status     *ApprovalStatus
	Type       *LeaveType
	From       *time.Time
	To         *time.Time

	ScopedEmployeeIDs []uuid.UUID
	RestrictScope     bool

	Page     int
	PageSize int
}

// Balance là quỹ ngày phép của một người trong một năm.
type Balance struct {
	EmployeeID      uuid.UUID
	Year            int
	EntitledDays    float64
	CarriedOverDays float64
	UsedDays        float64

	CreatedAt time.Time
	UpdatedAt time.Time

	EmployeeName string // JOIN lúc đọc
}

// Remaining là số ngày phép còn lại.
func (b *Balance) Remaining() float64 {
	return b.EntitledDays + b.CarriedOverDays - b.UsedDays
}
