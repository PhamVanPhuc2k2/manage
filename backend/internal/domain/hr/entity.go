// Package hr chứa entity và port của module nhân sự: công ty, phòng ban,
// chức vụ, nhân viên, tài khoản đăng nhập.
package hr

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound      = errors.New("không tìm thấy")
	ErrDuplicateCode = errors.New("mã đã tồn tại")
	ErrCycle         = errors.New("tạo ra vòng lặp trong cây")
)

// =========================================================================
// CÔNG TY
// =========================================================================

type Company struct {
	ID        uuid.UUID
	Name      string
	TaxCode   string
	Address   string
	Timezone  string
	WorkStart string
	WorkEnd   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// =========================================================================
// PHÒNG BAN
// =========================================================================

type Department struct {
	ID          uuid.UUID
	CompanyID   uuid.UUID
	ParentID    *uuid.UUID
	Code        string
	Name        string
	Description string
	ManagerID   *uuid.UUID

	// Các trường dưới đây được JOIN vào lúc đọc, không lưu trong bảng.
	ManagerName   string
	EmployeeCount int
	Children      []*Department

	CreatedAt time.Time
	UpdatedAt time.Time
}

// =========================================================================
// CHỨC VỤ
// =========================================================================

type Position struct {
	ID        uuid.UUID
	CompanyID uuid.UUID
	Code      string
	Name      string
	SalaryMin *float64
	SalaryMax *float64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// =========================================================================
// NHÂN VIÊN
// =========================================================================

type WorkMode string

const (
	WorkModeOnsite WorkMode = "onsite"
	WorkModeRemote WorkMode = "remote"
	WorkModeHybrid WorkMode = "hybrid"
)

func (w WorkMode) Valid() bool {
	switch w {
	case WorkModeOnsite, WorkModeRemote, WorkModeHybrid:
		return true
	}
	return false
}

type EmployeeStatus string

const (
	StatusProbation EmployeeStatus = "probation"
	StatusOfficial  EmployeeStatus = "official"
	StatusResigned  EmployeeStatus = "resigned"
)

func (s EmployeeStatus) Valid() bool {
	switch s {
	case StatusProbation, StatusOfficial, StatusResigned:
		return true
	}
	return false
}

type Employee struct {
	ID           uuid.UUID
	CompanyID    uuid.UUID
	EmployeeCode string
	FullName     string
	Email        string
	Phone        string
	DateOfBirth  *time.Time
	Gender       string
	Address      string

	DepartmentID *uuid.UUID
	PositionID   *uuid.UUID
	ManagerID    *uuid.UUID

	WorkMode   WorkMode
	Status     EmployeeStatus
	JoinedAt   time.Time
	ResignedAt *time.Time

	AvatarKey string

	// JOIN vào lúc đọc.
	DepartmentName string
	PositionName   string
	ManagerName    string
	HasAccount     bool

	DeletedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// EmployeeFilter gom mọi điều kiện lọc danh sách nhân viên.
//
// Phạm vi dữ liệu được áp vào đây ở tầng usecase: ScopeSelf đặt EmployeeID,
// ScopeDepartment đặt DepartmentIDs, ScopeAll không đặt gì.
type EmployeeFilter struct {
	Search       string
	DepartmentID *uuid.UUID
	PositionID   *uuid.UUID
	Status       *EmployeeStatus
	WorkMode     *WorkMode

	// Hai trường dưới KHÔNG đến từ người dùng — chúng do usecase đặt
	// dựa trên phạm vi của actor. Đừng bao giờ bind chúng từ query string.
	ScopedEmployeeID    *uuid.UUID
	ScopedDepartmentIDs []uuid.UUID

	Page     int
	PageSize int
	SortBy   string
	SortDesc bool
}

// =========================================================================
// TÀI KHOẢN
// =========================================================================

type User struct {
	ID                 uuid.UUID
	EmployeeID         uuid.UUID
	Email              string
	PasswordHash       string
	IsActive           bool
	MustChangePassword bool
	LastLoginAt        *time.Time
	FailedAttempts     int
	LockedUntil        *time.Time
	DeletedAt          *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time

	// JOIN từ bảng employees để kiểm tra nhân viên còn làm việc không.
	EmployeeName      string
	EmployeeStatus    EmployeeStatus
	EmployeeDeletedAt *time.Time
	DepartmentID      *uuid.UUID
}

// CanLogin gom mọi điều kiện để một tài khoản được phép đăng nhập.
//
// Gộp vào một chỗ để không bỏ sót điều kiện nào — mỗi lần thêm điều kiện
// mới chỉ phải sửa ở đây.
func (u *User) CanLogin() error {
	if u.DeletedAt != nil || !u.IsActive {
		return errors.New("tài khoản đã bị vô hiệu hoá")
	}
	if u.EmployeeDeletedAt != nil {
		return errors.New("hồ sơ nhân viên đã bị xoá")
	}
	if u.EmployeeStatus == StatusResigned {
		return errors.New("nhân viên đã nghỉ việc")
	}
	return nil
}
