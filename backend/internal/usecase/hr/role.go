package hr

import (
	"context"
	"slices"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

func (u *Usecase) ListRoles(ctx context.Context) ([]*domainhr.Role, error) {
	roles, err := u.roles.List(ctx)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return roles, nil
}

type EmployeeRoles struct {
	EmployeeID uuid.UUID `json:"employee_id"`
	UserID     uuid.UUID `json:"user_id"`
	Roles      []string  `json:"roles"`
}

func (u *Usecase) GetEmployeeRoles(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
) (*EmployeeRoles, error) {
	emp, err := u.employees.GetByID(ctx, employeeID)
	if err != nil {
		return nil, apperror.NotFound("nhân viên")
	}
	if !actor.CanSeeEmployee(emp.ID, emp.DepartmentID) {
		return nil, apperror.NotFound("nhân viên")
	}

	user, err := u.users.FindByEmployeeID(ctx, employeeID)
	if err != nil || user == nil {
		return nil, apperror.NotFound("tài khoản")
	}

	codes, err := u.users.ListRoleCodes(ctx, user.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return &EmployeeRoles{EmployeeID: employeeID, UserID: user.ID, Roles: codes}, nil
}

// SetEmployeeRoles đặt lại TOÀN BỘ danh sách vai trò của một người.
//
// Dùng "đặt lại cả danh sách" thay vì "thêm/gỡ từng cái": giao diện gửi lên
// đúng những gì người quản trị thấy trên màn hình, nên không có cảnh hai
// người sửa cùng lúc rồi ra kết quả lẫn lộn.
func (u *Usecase) SetEmployeeRoles(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
	roleCodes []string,
) (*EmployeeRoles, error) {
	log := logger.FromContext(ctx)

	emp, err := u.employees.GetByID(ctx, employeeID)
	if err != nil {
		return nil, apperror.NotFound("nhân viên")
	}
	if !actor.CanSeeEmployee(emp.ID, emp.DepartmentID) {
		return nil, apperror.NotFound("nhân viên")
	}

	user, err := u.users.FindByEmployeeID(ctx, employeeID)
	if err != nil || user == nil {
		return nil, apperror.NotFound("tài khoản")
	}

	current, err := u.users.ListRoleCodes(ctx, user.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	// --- Chặn tự khoá mình ra ngoài ---
	//
	// Người đang có vai trò admin mà tự gỡ nó đi thì mất luôn quyền gán lại.
	// Nếu đó là admin duy nhất, cả hệ thống không còn ai quản trị được và
	// phải sửa tay trong database.
	if user.ID == actor.UserID &&
		slices.Contains(current, "admin") && !slices.Contains(roleCodes, "admin") {
		return nil, apperror.Invalid(
			"Không thể tự gỡ vai trò quản trị của chính mình. "+
				"Nhờ một quản trị viên khác thực hiện.", nil)
	}

	// --- Chặn leo thang đặc quyền ---
	//
	// Chỉ admin mới gán được vai trò admin. Thiếu kiểm tra này, bất kỳ ai có
	// quyền role:assign đều tự nâng mình lên admin được.
	if slices.Contains(roleCodes, "admin") && !slices.Contains(actor.Roles, "admin") {
		return nil, apperror.Forbidden("Chỉ quản trị viên mới gán được vai trò quản trị")
	}

	// Kiểm tra mọi mã vai trò có thật, TRƯỚC khi ghi bất cứ thứ gì.
	// Ghi nửa chừng rồi mới phát hiện mã sai sẽ để lại trạng thái dở dang.
	wanted := make(map[string]uuid.UUID, len(roleCodes))
	for _, code := range roleCodes {
		role, err := u.roles.GetByCode(ctx, code)
		if err != nil {
			return nil, apperror.Invalid("Vai trò không tồn tại: "+code, nil)
		}
		wanted[code] = role.ID
	}
	if len(wanted) == 0 {
		return nil, apperror.Invalid("Phải có ít nhất một vai trò", nil)
	}

	for _, code := range current {
		if _, keep := wanted[code]; keep {
			continue
		}
		role, err := u.roles.GetByCode(ctx, code)
		if err != nil {
			continue
		}
		if err := u.users.RemoveRole(ctx, user.ID, role.ID); err != nil {
			return nil, apperror.Internal(err)
		}
	}

	for code, roleID := range wanted {
		if slices.Contains(current, code) {
			continue
		}
		if err := u.users.AssignRole(ctx, user.ID, roleID, actor.UserID); err != nil {
			return nil, apperror.Internal(err)
		}
	}

	// Quyền nằm trong access token nên người bị đổi vai trò vẫn giữ quyền cũ
	// tới khi token hết hạn (tối đa 15 phút). Cắt phiên để buộc đăng nhập
	// lại — đổi quyền phải có hiệu lực ngay, nhất là khi đang GỠ quyền.
	if u.onEmployeeDeactivated != nil && user.ID != actor.UserID {
		u.onEmployeeDeactivated(ctx, user.ID)
	}

	log.Info().
		Str("employee_id", employeeID.String()).
		Strs("roles", roleCodes).
		Str("changed_by", actor.UserID.String()).
		Msg("đã đổi vai trò")

	updated, err := u.users.ListRoleCodes(ctx, user.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return &EmployeeRoles{EmployeeID: employeeID, UserID: user.ID, Roles: updated}, nil
}
