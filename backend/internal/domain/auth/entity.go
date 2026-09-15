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
)

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
