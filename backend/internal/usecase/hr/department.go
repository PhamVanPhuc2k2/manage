package hr

import (
	"context"
	"strings"

	"github.com/google/uuid"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

type DepartmentInput struct {
	ParentID    *uuid.UUID
	Code        string
	Name        string
	Description string
	ManagerID   *uuid.UUID
}

func (u *Usecase) ListDepartments(ctx context.Context) ([]*domainhr.Department, error) {
	companyID, err := u.companyID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := u.departments.List(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// DepartmentTree dựng cây từ danh sách phẳng.
//
// Dựng trong bộ nhớ chứ không truy vấn đệ quy: công ty có vài chục phòng ban
// nên một truy vấn phẳng rồi ghép trong Go nhanh hơn và dễ đọc hơn.
func (u *Usecase) DepartmentTree(ctx context.Context) ([]*domainhr.Department, error) {
	flat, err := u.ListDepartments(ctx)
	if err != nil {
		return nil, err
	}

	byID := make(map[uuid.UUID]*domainhr.Department, len(flat))
	for _, d := range flat {
		d.Children = []*domainhr.Department{}
		byID[d.ID] = d
	}

	roots := make([]*domainhr.Department, 0)
	for _, d := range flat {
		if d.ParentID != nil {
			if parent, ok := byID[*d.ParentID]; ok {
				parent.Children = append(parent.Children, d)
				continue
			}
			// Cha đã bị xoá mềm: coi như phòng gốc thay vì làm mất phòng này
			// khỏi cây.
		}
		roots = append(roots, d)
	}
	return roots, nil
}

func (u *Usecase) GetDepartment(ctx context.Context, id uuid.UUID) (*domainhr.Department, error) {
	d, err := u.departments.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("phòng ban")
	}
	return d, nil
}

func (u *Usecase) CreateDepartment(ctx context.Context, in DepartmentInput) (*domainhr.Department, error) {
	companyID, err := u.companyID(ctx)
	if err != nil {
		return nil, err
	}
	if err := u.validateDepartmentInput(ctx, companyID, in, nil); err != nil {
		return nil, err
	}

	d := &domainhr.Department{
		CompanyID:   companyID,
		ParentID:    in.ParentID,
		Code:        strings.TrimSpace(in.Code),
		Name:        strings.TrimSpace(in.Name),
		Description: in.Description,
		ManagerID:   in.ManagerID,
	}
	if err := u.departments.Create(ctx, d); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.departments.GetByID(ctx, d.ID)
}

func (u *Usecase) UpdateDepartment(
	ctx context.Context,
	id uuid.UUID,
	in DepartmentInput,
) (*domainhr.Department, error) {
	existing, err := u.departments.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("phòng ban")
	}
	if err := u.validateDepartmentInput(ctx, existing.CompanyID, in, &id); err != nil {
		return nil, err
	}
	if err := u.validateNoCycle(ctx, id, in.ParentID); err != nil {
		return nil, err
	}

	existing.ParentID = in.ParentID
	existing.Code = strings.TrimSpace(in.Code)
	existing.Name = strings.TrimSpace(in.Name)
	existing.Description = in.Description
	existing.ManagerID = in.ManagerID

	if err := u.departments.Update(ctx, existing); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.departments.GetByID(ctx, id)
}

func (u *Usecase) DeleteDepartment(ctx context.Context, id uuid.UUID) error {
	if _, err := u.departments.GetByID(ctx, id); err != nil {
		return apperror.NotFound("phòng ban")
	}

	// Không cho xoá phòng còn người. Xoá được thì nhân viên sẽ trỏ tới một
	// phòng đã biến mất, và mọi báo cáo theo phòng sẽ sai.
	n, err := u.departments.CountEmployees(ctx, id)
	if err != nil {
		return apperror.Internal(err)
	}
	if n > 0 {
		return apperror.Conflict(
			"Phòng ban còn nhân viên. Chuyển họ sang phòng khác trước khi xoá.")
	}

	// Cũng không cho xoá phòng còn phòng con — nếu không, nhánh dưới sẽ
	// mồ côi và biến mất khỏi cây.
	subtree, err := u.departments.ListSubtreeIDs(ctx, id)
	if err != nil {
		return apperror.Internal(err)
	}
	if len(subtree) > 1 {
		return apperror.Conflict("Phòng ban còn phòng con. Xoá hoặc chuyển phòng con trước.")
	}

	if err := u.departments.SoftDelete(ctx, id); err != nil {
		return apperror.Internal(err)
	}
	return nil
}

func (u *Usecase) validateDepartmentInput(
	ctx context.Context,
	companyID uuid.UUID,
	in DepartmentInput,
	excludeID *uuid.UUID,
) error {
	if strings.TrimSpace(in.Code) == "" {
		return apperror.Invalid("Mã phòng ban không được để trống", nil)
	}
	if strings.TrimSpace(in.Name) == "" {
		return apperror.Invalid("Tên phòng ban không được để trống", nil)
	}

	dup, err := u.departments.ExistsCode(ctx, companyID, in.Code, excludeID)
	if err != nil {
		return apperror.Internal(err)
	}
	if dup {
		return apperror.Conflict("Mã phòng ban đã tồn tại")
	}

	if in.ParentID != nil {
		if _, err := u.departments.GetByID(ctx, *in.ParentID); err != nil {
			return apperror.Invalid("Phòng ban cấp trên không tồn tại", nil)
		}
	}
	if in.ManagerID != nil {
		if _, err := u.employees.GetByID(ctx, *in.ManagerID); err != nil {
			return apperror.Invalid("Trưởng phòng không tồn tại", nil)
		}
	}
	return nil
}

// validateNoCycle kiểm tra việc đặt newParentID làm cha của departmentID có
// tạo ra vòng lặp hay không.
//
// Vòng lặp trong cây phòng ban là lỗi nghiêm trọng: mọi truy vấn WITH
// RECURSIVE sẽ chạy vô hạn và treo cả connection pool. Database không tự
// chặn được, phải chặn ở tầng ghi.
func (u *Usecase) validateNoCycle(
	ctx context.Context,
	departmentID uuid.UUID,
	newParentID *uuid.UUID,
) error {
	if newParentID == nil {
		return nil // thành phòng gốc, luôn an toàn
	}
	if *newParentID == departmentID {
		return apperror.Invalid("Phòng ban không thể là cấp trên của chính nó", nil)
	}

	// Đi ngược lên tổ tiên của cha mới. Gặp lại chính phòng đang sửa nghĩa
	// là ta đang tạo vòng lặp.
	ancestors, err := u.departments.ListAncestorIDs(ctx, *newParentID)
	if err != nil {
		return apperror.Internal(err)
	}
	for _, id := range ancestors {
		if id == departmentID {
			return apperror.Invalid(
				"Không thể chuyển phòng ban vào bên trong chính phòng con của nó", nil)
		}
	}
	return nil
}
