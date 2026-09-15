package hr

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type CompanyRepository interface {
	Create(ctx context.Context, c *Company) error
	GetFirst(ctx context.Context) (*Company, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Company, error)
}

type DepartmentRepository interface {
	Create(ctx context.Context, d *Department) error
	Update(ctx context.Context, d *Department) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Department, error)
	List(ctx context.Context, companyID uuid.UUID) ([]*Department, error)

	// ListSubtreeIDs trả về id của phòng ban và TẤT CẢ phòng con, đệ quy.
	// Dùng cho phạm vi "department": trưởng phòng thấy cả nhánh dưới mình.
	ListSubtreeIDs(ctx context.Context, rootID uuid.UUID) ([]uuid.UUID, error)

	// ListAncestorIDs trả về id của mọi tổ tiên, dùng để chặn vòng lặp.
	ListAncestorIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error)

	CountEmployees(ctx context.Context, id uuid.UUID) (int, error)
	ExistsCode(ctx context.Context, companyID uuid.UUID, code string, excludeID *uuid.UUID) (bool, error)
}

type PositionRepository interface {
	Create(ctx context.Context, p *Position) error
	Update(ctx context.Context, p *Position) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Position, error)
	List(ctx context.Context, companyID uuid.UUID) ([]*Position, error)
	CountEmployees(ctx context.Context, id uuid.UUID) (int, error)
	ExistsCode(ctx context.Context, companyID uuid.UUID, code string, excludeID *uuid.UUID) (bool, error)
}

type EmployeeRepository interface {
	Create(ctx context.Context, e *Employee) error
	Update(ctx context.Context, e *Employee) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (*Employee, error)

	// List trả về danh sách và tổng số bản ghi khớp điều kiện.
	List(ctx context.Context, f EmployeeFilter) ([]*Employee, int, error)

	ListAncestorIDs(ctx context.Context, id uuid.UUID) ([]uuid.UUID, error)
	ExistsCode(ctx context.Context, companyID uuid.UUID, code string, excludeID *uuid.UUID) (bool, error)
	ExistsEmail(ctx context.Context, email string, excludeID *uuid.UUID) (bool, error)
	UpdateAvatarKey(ctx context.Context, id uuid.UUID, key string) error
}

type UserRepository interface {
	Create(ctx context.Context, u *User) error
	FindByEmail(ctx context.Context, email string) (*User, error)
	FindByID(ctx context.Context, id uuid.UUID) (*User, error)
	FindByEmployeeID(ctx context.Context, employeeID uuid.UUID) (*User, error)

	UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error
	SetMustChangePassword(ctx context.Context, id uuid.UUID, must bool) error
	UpdateLastLogin(ctx context.Context, id uuid.UUID) error
	IncrementFailedAttempts(ctx context.Context, id uuid.UUID) error
	ResetFailedAttempts(ctx context.Context, id uuid.UUID) error
	SetActive(ctx context.Context, id uuid.UUID, active bool) error

	ListRoleCodes(ctx context.Context, userID uuid.UUID) ([]string, error)
	AssignRole(ctx context.Context, userID, roleID, assignedBy uuid.UUID) error
	RemoveRole(ctx context.Context, userID, roleID uuid.UUID) error
}

type RoleRepository interface {
	List(ctx context.Context) ([]*Role, error)
	GetByCode(ctx context.Context, code string) (*Role, error)
}

// FileStorage là cổng lưu trữ tệp.
//
// Khai báo ở tầng domain nên usecase không biết đằng sau là Cloudflare R2,
// AWS S3 hay ổ đĩa — đổi nhà cung cấp chỉ phải sửa ở composition root.
type FileStorage interface {
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	Stat(ctx context.Context, key string) (size int64, contentType string, err error)
	// DetectContentType đoán kiểu tệp từ NỘI DUNG thật, không tin header
	// do client khai báo.
	DetectContentType(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
}

type Role struct {
	ID          uuid.UUID
	Code        string
	Name        string
	Description string
	Scope       string
	IsSystem    bool
	Permissions []string
}
