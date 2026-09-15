package hr

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

type EmployeeInput struct {
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
	WorkMode     domainhr.WorkMode
	Status       domainhr.EmployeeStatus
	JoinedAt     time.Time
	ResignedAt   *time.Time
}

type ListEmployeesResult struct {
	Items      []*domainhr.Employee
	Page       int
	PageSize   int
	TotalItems int
	TotalPages int
}

// applyScope áp phạm vi dữ liệu của actor vào bộ lọc.
//
// ĐÂY là chỗ quyết định ai thấy được gì. Middleware chỉ biết "người này có
// quyền employee:read", nó không biết người này định đọc hồ sơ của ai.
// Bỏ hàm này đi là nhân viên thường đọc được hồ sơ cả công ty.
func applyScope(f *domainhr.EmployeeFilter, actor *domainauth.Actor) {
	switch actor.Scope {
	case domainauth.ScopeAll:
		// không thêm điều kiện

	case domainauth.ScopeDepartment:
		// Không có phòng nào thuộc quyền thì chỉ thấy chính mình.
		if len(actor.ManagedDepartmentIDs) == 0 {
			id := actor.EmployeeID
			f.ScopedEmployeeID = &id
			return
		}
		f.ScopedDepartmentIDs = actor.ManagedDepartmentIDs

	default: // ScopeSelf và mọi giá trị lạ
		id := actor.EmployeeID
		f.ScopedEmployeeID = &id
	}
}

func (u *Usecase) ListEmployees(
	ctx context.Context,
	actor *domainauth.Actor,
	f domainhr.EmployeeFilter,
) (*ListEmployeesResult, error) {
	// Xoá sạch mọi giá trị phạm vi có thể đã lọt vào từ query string.
	// Chỉ applyScope mới được đặt hai trường này.
	f.ScopedEmployeeID = nil
	f.ScopedDepartmentIDs = nil
	applyScope(&f, actor)

	f.Page, f.PageSize = normalizePage(f.Page, f.PageSize)

	items, total, err := u.employees.List(ctx, f)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	totalPages := (total + f.PageSize - 1) / f.PageSize
	return &ListEmployeesResult{
		Items:      items,
		Page:       f.Page,
		PageSize:   f.PageSize,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

func (u *Usecase) GetEmployee(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) (*domainhr.Employee, error) {
	e, err := u.employees.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("nhân viên")
	}

	// Chống IDOR: trả về 404 chứ không phải 403.
	//
	// 403 xác nhận rằng bản ghi có tồn tại — đó đã là rò rỉ thông tin.
	// Người không có quyền xem thì với họ, bản ghi đó không tồn tại.
	if !actor.CanSeeEmployee(e.ID, e.DepartmentID) {
		return nil, apperror.NotFound("nhân viên")
	}
	return e, nil
}

func (u *Usecase) CreateEmployee(
	ctx context.Context,
	actor *domainauth.Actor,
	in EmployeeInput,
) (*domainhr.Employee, error) {
	companyID, err := u.companyID(ctx)
	if err != nil {
		return nil, err
	}
	if err := u.validateEmployeeInput(ctx, companyID, in, nil); err != nil {
		return nil, err
	}

	e := &domainhr.Employee{
		CompanyID:    companyID,
		EmployeeCode: strings.TrimSpace(in.EmployeeCode),
		FullName:     strings.TrimSpace(in.FullName),
		Email:        strings.TrimSpace(in.Email),
		Phone:        in.Phone,
		DateOfBirth:  in.DateOfBirth,
		Gender:       in.Gender,
		Address:      in.Address,
		DepartmentID: in.DepartmentID,
		PositionID:   in.PositionID,
		ManagerID:    in.ManagerID,
		WorkMode:     in.WorkMode,
		Status:       in.Status,
		JoinedAt:     in.JoinedAt,
	}
	if err := u.employees.Create(ctx, e); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.employees.GetByID(ctx, e.ID)
}

func (u *Usecase) UpdateEmployee(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	in EmployeeInput,
) (*domainhr.Employee, error) {
	existing, err := u.employees.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("nhân viên")
	}
	// Kiểm tra phạm vi trên bản ghi CŨ: người sửa phải có quyền với hồ sơ
	// hiện tại, không phải với hồ sơ sau khi sửa.
	if !actor.CanSeeEmployee(existing.ID, existing.DepartmentID) {
		return nil, apperror.NotFound("nhân viên")
	}

	if err := u.validateEmployeeInput(ctx, existing.CompanyID, in, &id); err != nil {
		return nil, err
	}

	// Chặn vòng lặp trong chuỗi cấp trên. Chuỗi manager_id cũng là một cây
	// và cũng vòng lặp được y như phòng ban.
	if in.ManagerID != nil {
		if *in.ManagerID == id {
			return nil, apperror.Invalid("Nhân viên không thể là cấp trên của chính mình", nil)
		}
		ancestors, err := u.employees.ListAncestorIDs(ctx, *in.ManagerID)
		if err != nil {
			return nil, apperror.Internal(err)
		}
		for _, aID := range ancestors {
			if aID == id {
				return nil, apperror.Invalid(
					"Không thể đặt cấp dưới của mình làm cấp trên", nil)
			}
		}
	}

	existing.EmployeeCode = strings.TrimSpace(in.EmployeeCode)
	existing.FullName = strings.TrimSpace(in.FullName)
	existing.Email = strings.TrimSpace(in.Email)
	existing.Phone = in.Phone
	existing.DateOfBirth = in.DateOfBirth
	existing.Gender = in.Gender
	existing.Address = in.Address
	existing.DepartmentID = in.DepartmentID
	existing.PositionID = in.PositionID
	existing.ManagerID = in.ManagerID
	existing.WorkMode = in.WorkMode
	existing.Status = in.Status
	existing.JoinedAt = in.JoinedAt
	existing.ResignedAt = in.ResignedAt

	if err := u.employees.Update(ctx, existing); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.employees.GetByID(ctx, id)
}

// DeactivateEmployee vô hiệu hoá nhân viên. KHÔNG xoá cứng: phiếu lương và
// chấm công tham chiếu tới họ.
func (u *Usecase) DeactivateEmployee(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) error {
	e, err := u.employees.GetByID(ctx, id)
	if err != nil {
		return apperror.NotFound("nhân viên")
	}
	if !actor.CanSeeEmployee(e.ID, e.DepartmentID) {
		return apperror.NotFound("nhân viên")
	}
	if e.ID == actor.EmployeeID {
		return apperror.Invalid("Không thể tự vô hiệu hoá tài khoản của mình", nil)
	}

	// Tra tài khoản TRƯỚC khi xoá mềm, không phải sau.
	//
	// SoftDelete đặt users.deleted_at (bắt buộc, nếu không email bị khoá
	// vĩnh viễn), mà FindByEmployeeID lọc `deleted_at IS NULL`. Tra sau khi
	// xoá thì không bao giờ ra, nhánh cắt phiên bị bỏ qua âm thầm, và người
	// vừa bị vô hiệu hoá vẫn dùng được hệ thống tới khi access token hết
	// hạn — tối đa 15 phút với đầy đủ quyền cũ.
	user, findErr := u.users.FindByEmployeeID(ctx, id)

	if err := u.employees.SoftDelete(ctx, id); err != nil {
		return apperror.Internal(err)
	}

	// Phiên đang mở của người đó phải bị cắt ngay.
	if findErr == nil && user != nil && u.onEmployeeDeactivated != nil {
		u.onEmployeeDeactivated(ctx, user.ID)
	}
	return nil
}

func (u *Usecase) validateEmployeeInput(
	ctx context.Context,
	companyID uuid.UUID,
	in EmployeeInput,
	excludeID *uuid.UUID,
) error {
	if strings.TrimSpace(in.FullName) == "" {
		return apperror.Invalid("Họ tên không được để trống", nil)
	}
	if strings.TrimSpace(in.EmployeeCode) == "" {
		return apperror.Invalid("Mã nhân viên không được để trống", nil)
	}
	if strings.TrimSpace(in.Email) == "" {
		return apperror.Invalid("Email không được để trống", nil)
	}
	if !in.WorkMode.Valid() {
		return apperror.Invalid("Hình thức làm việc không hợp lệ", nil)
	}
	if !in.Status.Valid() {
		return apperror.Invalid("Trạng thái không hợp lệ", nil)
	}
	if in.JoinedAt.IsZero() {
		return apperror.Invalid("Ngày vào làm không được để trống", nil)
	}
	if in.ResignedAt != nil && in.ResignedAt.Before(in.JoinedAt) {
		return apperror.Invalid("Ngày nghỉ việc phải sau ngày vào làm", nil)
	}

	dup, err := u.employees.ExistsCode(ctx, companyID, in.EmployeeCode, excludeID)
	if err != nil {
		return apperror.Internal(err)
	}
	if dup {
		return apperror.Conflict("Mã nhân viên đã tồn tại")
	}

	dup, err = u.employees.ExistsEmail(ctx, in.Email, excludeID)
	if err != nil {
		return apperror.Internal(err)
	}
	if dup {
		return apperror.Conflict("Email đã được sử dụng")
	}

	if in.DepartmentID != nil {
		if _, err := u.departments.GetByID(ctx, *in.DepartmentID); err != nil {
			return apperror.Invalid("Phòng ban không tồn tại", nil)
		}
	}
	if in.PositionID != nil {
		if _, err := u.positions.GetByID(ctx, *in.PositionID); err != nil {
			return apperror.Invalid("Chức vụ không tồn tại", nil)
		}
	}
	if in.ManagerID != nil {
		if _, err := u.employees.GetByID(ctx, *in.ManagerID); err != nil {
			return apperror.Invalid("Cấp trên không tồn tại", nil)
		}
	}
	return nil
}
