// Package auth chứa entity và port của module xác thực, phân quyền.
package auth

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTokenReused  = errors.New("refresh token đã được sử dụng")
	ErrTokenInvalid = errors.New("refresh token không hợp lệ")
	ErrNoSession    = errors.New("phiên không tồn tại")

	// ErrTokenReusedInGrace: token dùng lại NGAY SAU lần dùng đầu tiên.
	//
	// Đây gần như chắc chắn không phải đánh cắp mà là hai tình huống bình
	// thường:
	//   - Người dùng mở nhiều tab, hai tab cùng gọi refresh một lúc. Cơ chế
	//     gộp request trong api-client chỉ hoạt động trong PHẠM VI MỘT TAB,
	//     không chặn được nhiều tab.
	//   - Phản hồi refresh rơi mất giữa đường, client gửi lại token cũ.
	//
	// Kẻ trộm thật gần như không thể dùng token trong đúng vài giây sau khi
	// chủ nhân vừa dùng. Xem RefreshGracePeriod.
	ErrTokenReusedInGrace = errors.New("refresh token dùng lại trong thời gian ân hạn")
)

// Lỗi của bước xác minh OTP.
//
// Tách riêng ErrOTPWrongCode với ErrOTPNotFound là CÓ CHỦ Ý và ngược với
// quy tắc "một thông báo chung" ở màn hình đăng nhập. Ở đây người dùng đã
// qua bước mật khẩu, ta biết chắc họ là ai; giấu đi chuyện "mã sai" chỉ làm
// họ loay hoay không biết nên nhập lại hay bấm gửi lại mã.
var (
	ErrOTPNotFound        = errors.New("phiên xác minh không tồn tại hoặc đã hết hạn")
	ErrOTPWrongCode       = errors.New("mã xác minh không đúng")
	ErrOTPTooManyAttempts = errors.New("nhập sai mã quá nhiều lần")
	ErrOTPResendTooSoon   = errors.New("gửi lại mã quá sớm")
	ErrOTPTooManyResends  = errors.New("gửi lại mã quá nhiều lần")
)

// Tham số của bước xác minh OTP.
//
// Vì sao 6 chữ số là đủ? Một triệu khả năng, tối đa 5 lần thử, thử thách
// sống 5 phút và chỉ gửi lại được 3 lần. Kẻ tấn công đoán mò có xác suất
// 5/1.000.000 mỗi phiên, và không kéo dài phiên ra được. Tăng lên 8 chữ số
// chỉ làm người dùng gõ sai nhiều hơn.
//
// Điều kiện tiên quyết: phải chặn được số lần thử. OTP 6 chữ số KHÔNG có
// giới hạn lần thử thì vét cạn xong trong vài phút.
const (
	OTPDigits         = 6
	OTPTTL            = 5 * time.Minute
	OTPMaxAttempts    = 5
	OTPResendCooldown = 60 * time.Second
	OTPMaxResends     = 3
)

// RefreshGracePeriod là khoảng thời gian sau lần dùng đầu tiên mà việc dùng
// lại refresh token vẫn được chấp nhận.
//
// Đánh đổi: khoảng này càng dài, kẻ trộm càng có nhiều thời gian dùng token
// đã bị lộ mà không bị phát hiện. 10 giây đủ để xử lý hai tab cùng tải và
// một lần gửi lại do mạng chập, nhưng quá ngắn để khai thác trong thực tế.
//
// Các nhà cung cấp lớn cũng làm vậy — Auth0 gọi là "rotation leeway".
const RefreshGracePeriod = 10 * time.Second

// Mã lỗi trả về cho client. Frontend dựa vào chúng để quyết định hành động:
// gặp UNAUTHORIZED thì thử refresh token, gặp FORBIDDEN thì báo thiếu quyền.
const (
	ErrCodeUnauthorized = "UNAUTHORIZED"
	ErrCodeForbidden    = "FORBIDDEN"
)

type Scope string

const (
	ScopeAll        Scope = "all"
	ScopeDepartment Scope = "department"
	ScopeSelf       Scope = "self"
)

// Rank dùng để chọn phạm vi rộng nhất khi một người có nhiều vai trò.
func (s Scope) Rank() int {
	switch s {
	case ScopeAll:
		return 3
	case ScopeDepartment:
		return 2
	default:
		return 1
	}
}

// Danh mục mã quyền. Khai báo hằng để tránh gõ sai chuỗi ở hai nơi —
// gõ sai trong middleware thì route sẽ chặn nhầm mọi người, rất khó phát hiện.
const (
	PermEmployeeRead   = "employee:read"
	PermEmployeeCreate = "employee:create"
	PermEmployeeUpdate = "employee:update"
	PermEmployeeDelete = "employee:delete"

	PermDepartmentRead   = "department:read"
	PermDepartmentCreate = "department:create"
	PermDepartmentUpdate = "department:update"
	PermDepartmentDelete = "department:delete"

	PermPositionRead   = "position:read"
	PermPositionManage = "position:manage"

	PermRoleRead   = "role:read"
	PermRoleAssign = "role:assign"

	// Phase 2. Lưu ý: các quyền này chỉ trả lời "được làm loại việc này
	// không". Việc "được đụng vào ĐÚNG dự án nào" do bảng project_members
	// quyết định và kiểm tra ở tầng usecase.
	PermProjectRead   = "project:read"
	PermProjectCreate = "project:create"
	PermProjectUpdate = "project:update"
	PermProjectDelete = "project:delete"

	PermTaskRead   = "task:read"
	PermTaskCreate = "task:create"
	PermTaskUpdate = "task:update"
	PermTaskDelete = "task:delete"

	// Phase 3. Tách attendance:read (công của mình) khỏi
	// attendance:read_all (công của người khác) là điểm then chốt: dữ liệu
	// chấm công là dữ liệu cá nhân, và "ai cũng xem được của nhau" là thứ
	// không thu hồi lại được sau khi đã lỡ mở.
	PermAttendanceRead    = "attendance:read"
	PermAttendanceReadAll = "attendance:read_all"
	PermAttendanceManage  = "attendance:manage"

	PermLeaveRead    = "leave:read"
	PermLeaveCreate  = "leave:create"
	PermLeaveApprove = "leave:approve"
	PermLeaveManage  = "leave:manage"

	PermScheduleManage = "schedule:manage"
)

// Actor là danh tính của người đang thực hiện request.
// Middleware dựng nó từ access token và đặt vào context.
type Actor struct {
	UserID       uuid.UUID
	EmployeeID   uuid.UUID
	SessionID    uuid.UUID
	DepartmentID *uuid.UUID
	Roles        []string
	Permissions  map[string]struct{}
	Scope        Scope

	// Phòng ban mà người này quản lý, GỒM CẢ phòng con.
	// Chỉ có giá trị khi Scope == ScopeDepartment.
	ManagedDepartmentIDs []uuid.UUID
}

func (a *Actor) Can(permission string) bool {
	if a == nil {
		return false
	}
	_, ok := a.Permissions[permission]
	return ok
}

// CanSeeEmployee quyết định actor có được đụng tới hồ sơ của một nhân viên
// cụ thể hay không.
//
// PHẢI gọi trước MỌI thao tác trên một nhân viên — cả đọc lẫn ghi. Bỏ sót
// là dính lỗ hổng IDOR: middleware chỉ biết "người này có quyền
// employee:read", nó không biết người này định đọc hồ sơ của ai.
func (a *Actor) CanSeeEmployee(employeeID uuid.UUID, departmentID *uuid.UUID) bool {
	if a == nil {
		return false
	}
	switch a.Scope {
	case ScopeAll:
		return true
	case ScopeSelf:
		return a.EmployeeID == employeeID
	case ScopeDepartment:
		if a.EmployeeID == employeeID {
			return true
		}
		if departmentID == nil {
			return false
		}
		for _, id := range a.ManagedDepartmentIDs {
			if id == *departmentID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// Session là một lần đăng nhập trên một thiết bị.
type Session struct {
	ID         uuid.UUID `json:"id"`
	UserID     uuid.UUID `json:"user_id"`
	DeviceName string    `json:"device_name"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// Authorization là kết quả tính quyền của một người dùng.
type Authorization struct {
	Roles                []string
	Permissions          []string
	Scope                Scope
	ManagedDepartmentIDs []uuid.UUID
}
