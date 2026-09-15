package hr

import (
	"context"
	"strings"

	"github.com/google/uuid"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

type PositionInput struct {
	Code      string
	Name      string
	SalaryMin *float64
	SalaryMax *float64
}

func (u *Usecase) ListPositions(ctx context.Context) ([]*domainhr.Position, error) {
	companyID, err := u.companyID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := u.positions.List(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

func (u *Usecase) GetPosition(ctx context.Context, id uuid.UUID) (*domainhr.Position, error) {
	p, err := u.positions.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("chức vụ")
	}
	return p, nil
}

func (u *Usecase) CreatePosition(ctx context.Context, in PositionInput) (*domainhr.Position, error) {
	companyID, err := u.companyID(ctx)
	if err != nil {
		return nil, err
	}
	if err := u.validatePositionInput(ctx, companyID, in, nil); err != nil {
		return nil, err
	}

	p := &domainhr.Position{
		CompanyID: companyID,
		Code:      strings.TrimSpace(in.Code),
		Name:      strings.TrimSpace(in.Name),
		SalaryMin: in.SalaryMin,
		SalaryMax: in.SalaryMax,
	}
	if err := u.positions.Create(ctx, p); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.positions.GetByID(ctx, p.ID)
}

func (u *Usecase) UpdatePosition(
	ctx context.Context,
	id uuid.UUID,
	in PositionInput,
) (*domainhr.Position, error) {
	existing, err := u.positions.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("chức vụ")
	}
	if err := u.validatePositionInput(ctx, existing.CompanyID, in, &id); err != nil {
		return nil, err
	}

	existing.Code = strings.TrimSpace(in.Code)
	existing.Name = strings.TrimSpace(in.Name)
	existing.SalaryMin = in.SalaryMin
	existing.SalaryMax = in.SalaryMax

	if err := u.positions.Update(ctx, existing); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.positions.GetByID(ctx, id)
}

func (u *Usecase) DeletePosition(ctx context.Context, id uuid.UUID) error {
	if _, err := u.positions.GetByID(ctx, id); err != nil {
		return apperror.NotFound("chức vụ")
	}

	n, err := u.positions.CountEmployees(ctx, id)
	if err != nil {
		return apperror.Internal(err)
	}
	if n > 0 {
		return apperror.Conflict("Chức vụ đang được sử dụng bởi " +
			itoa(n) + " nhân viên. Đổi chức vụ của họ trước khi xoá.")
	}

	if err := u.positions.SoftDelete(ctx, id); err != nil {
		return apperror.Internal(err)
	}
	return nil
}

func (u *Usecase) validatePositionInput(
	ctx context.Context,
	companyID uuid.UUID,
	in PositionInput,
	excludeID *uuid.UUID,
) error {
	if strings.TrimSpace(in.Code) == "" {
		return apperror.Invalid("Mã chức vụ không được để trống", nil)
	}
	if strings.TrimSpace(in.Name) == "" {
		return apperror.Invalid("Tên chức vụ không được để trống", nil)
	}
	if in.SalaryMin != nil && *in.SalaryMin < 0 {
		return apperror.Invalid("Lương tối thiểu không được âm", nil)
	}
	if in.SalaryMin != nil && in.SalaryMax != nil && *in.SalaryMin > *in.SalaryMax {
		return apperror.Invalid("Lương tối thiểu không được lớn hơn lương tối đa", nil)
	}

	dup, err := u.positions.ExistsCode(ctx, companyID, in.Code, excludeID)
	if err != nil {
		return apperror.Internal(err)
	}
	if dup {
		return apperror.Conflict("Mã chức vụ đã tồn tại")
	}
	return nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
