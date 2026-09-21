package attendance

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// =========================================================================
// YÊU CẦU ĐIỀU CHỈNH CÔNG
// =========================================================================

func (u *Usecase) CreateAdjustment(
	ctx context.Context,
	actor *domainauth.Actor,
	start, end time.Time,
	reason string,
) (*domainatt.Adjustment, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if !end.After(start) {
		return nil, apperror.Invalid("Giờ kết thúc phải sau giờ bắt đầu", nil)
	}
	if end.Sub(start) > 24*time.Hour {
		return nil, apperror.Invalid("Một lần điều chỉnh không quá 24 giờ", nil)
	}
	if strings.TrimSpace(reason) == "" {
		return nil, apperror.Invalid("Phải nêu lý do điều chỉnh", nil)
	}

	loc := u.location(ctx)
	target := workDate(start, loc)

	if d, err := u.days.GetByDate(ctx, actor.EmployeeID, target); err == nil && d.IsLocked {
		return nil, apperror.Conflict("Kỳ công của ngày này đã khoá")
	}

	a := &domainatt.Adjustment{
		EmployeeID:     actor.EmployeeID,
		WorkDate:       target,
		RequestedStart: start,
		RequestedEnd:   end,
		Reason:         strings.TrimSpace(reason),
	}
	if err := u.adjustments.Create(ctx, a); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.adjustments.GetByID(ctx, a.ID)
}

func (u *Usecase) ListAdjustments(
	ctx context.Context,
	actor *domainauth.Actor,
	status *domainatt.ApprovalStatus,
) ([]*domainatt.Adjustment, error) {
	ids, restrict, err := u.scopeFor(ctx, actor)
	if err != nil {
		return nil, err
	}

	list, err := u.adjustments.List(ctx, ids, restrict, status)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// DecideAdjustment duyệt hoặc từ chối một yêu cầu điều chỉnh công.
//
// Duyệt thì GHI THÊM một phiên có source = 'adjustment' rồi tổng hợp lại
// ngày đó. Ghi thêm phiên chứ không sửa thẳng con số tổng: bảng công phải
// luôn truy ngược được về các phiên tạo ra nó, nếu không thì khi có tranh
// cãi sẽ không ai giải thích được con số từ đâu ra.
func (u *Usecase) DecideAdjustment(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	approve bool,
	note string,
) (*domainatt.Adjustment, error) {
	a, err := u.adjustments.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("yêu cầu điều chỉnh")
	}
	if err := u.canSee(ctx, actor, a.EmployeeID); err != nil {
		return nil, err
	}
	if actor != nil && actor.EmployeeID == a.EmployeeID {
		return nil, apperror.Forbidden("Không thể tự duyệt yêu cầu của chính mình")
	}
	if a.Status.Final() {
		return nil, apperror.Conflict("Yêu cầu này đã được xử lý rồi")
	}

	now := u.clock.Now()
	a.ApproverID = actorEmployeeID(actor)
	a.DecidedAt = &now
	a.DecisionNote = strings.TrimSpace(note)

	if approve {
		a.Status = domainatt.StatusApproved
	} else {
		a.Status = domainatt.StatusRejected
	}

	if err := u.adjustments.Update(ctx, a); err != nil {
		return nil, apperror.Internal(err)
	}

	if approve {
		s := &domainatt.Session{
			EmployeeID:    a.EmployeeID,
			WorkDate:      a.WorkDate,
			StartedAt:     a.RequestedStart,
			EndedAt:       a.RequestedEnd,
			ActiveMinutes: int(a.RequestedEnd.Sub(a.RequestedStart).Minutes()),
			Source:        domainatt.SourceAdjustment,
			Note:          a.Reason,
		}
		if err := u.sessions.Create(ctx, s); err != nil {
			return nil, apperror.Internal(err)
		}
		if _, err := u.RollupDay(ctx, a.WorkDate); err != nil {
			return a, nil // phiên đã ghi, tổng hợp lỗi không chặn kết quả
		}
	}

	return u.adjustments.GetByID(ctx, id)
}

// =========================================================================
// KHOÁ KỲ CÔNG
// =========================================================================

// LockPeriod khoá một khoảng ngày để chốt lương.
//
// Sau khi khoá, job tổng hợp tự động sẽ BỎ QUA những ngày này (xem điều
// kiện WHERE NOT is_locked trong Upsert), và mọi thay đổi phải đi qua một
// bút toán điều chỉnh riêng. Đây là câu trả lời cho rủi ro "Tính lương sai
// do sửa dữ liệu công sau khi chốt" trong bảng rủi ro của dự án.
func (u *Usecase) LockPeriod(
	ctx context.Context,
	from, to time.Time,
	locked bool,
) (int64, error) {
	if err := validRange(from, to); err != nil {
		return 0, err
	}

	loc := u.location(ctx)
	n, err := u.days.SetLocked(ctx, workDate(from, loc), workDate(to, loc), locked)
	if err != nil {
		return 0, apperror.Internal(err)
	}
	return n, nil
}

// =========================================================================
// KHUNG GIỜ LÀM VIỆC
// =========================================================================

type ScheduleInput struct {
	DepartmentID  *uuid.UUID
	EmployeeID    *uuid.UUID
	Name          string
	WorkStart     string
	WorkEnd       string
	Workdays      []int16
	BreakMinutes  int
	GraceMinutes  int
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
}

func (u *Usecase) ListSchedules(ctx context.Context) ([]*domainatt.Schedule, error) {
	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}
	list, err := u.schedules.List(ctx, companyID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

func (u *Usecase) CreateSchedule(
	ctx context.Context,
	in ScheduleInput,
) (*domainatt.Schedule, error) {
	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateScheduleInput(in); err != nil {
		return nil, err
	}

	s := &domainatt.Schedule{
		CompanyID:     companyID,
		DepartmentID:  in.DepartmentID,
		EmployeeID:    in.EmployeeID,
		Name:          strings.TrimSpace(in.Name),
		WorkStart:     in.WorkStart,
		WorkEnd:       in.WorkEnd,
		Workdays:      in.Workdays,
		BreakMinutes:  in.BreakMinutes,
		GraceMinutes:  in.GraceMinutes,
		EffectiveFrom: in.EffectiveFrom,
		EffectiveTo:   in.EffectiveTo,
	}
	if s.EffectiveFrom.IsZero() {
		s.EffectiveFrom = u.clock.Now()
	}
	if len(s.Workdays) == 0 {
		s.Workdays = []int16{1, 2, 3, 4, 5}
	}

	if err := u.schedules.Create(ctx, s); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.schedules.GetByID(ctx, s.ID)
}

func (u *Usecase) UpdateSchedule(
	ctx context.Context,
	id uuid.UUID,
	in ScheduleInput,
) (*domainatt.Schedule, error) {
	existing, err := u.schedules.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("khung giờ làm việc")
	}
	if err := validateScheduleInput(in); err != nil {
		return nil, err
	}

	existing.Name = strings.TrimSpace(in.Name)
	existing.WorkStart = in.WorkStart
	existing.WorkEnd = in.WorkEnd
	existing.BreakMinutes = in.BreakMinutes
	existing.GraceMinutes = in.GraceMinutes
	existing.EffectiveTo = in.EffectiveTo
	if len(in.Workdays) > 0 {
		existing.Workdays = in.Workdays
	}
	if !in.EffectiveFrom.IsZero() {
		existing.EffectiveFrom = in.EffectiveFrom
	}

	if err := u.schedules.Update(ctx, existing); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.schedules.GetByID(ctx, id)
}

func (u *Usecase) DeleteSchedule(ctx context.Context, id uuid.UUID) error {
	s, err := u.schedules.GetByID(ctx, id)
	if err != nil {
		return apperror.NotFound("khung giờ làm việc")
	}

	// Không cho xoá khung giờ CÔNG TY: nó là mốc cuối cùng để tính đi muộn
	// và thiếu giờ. Xoá đi thì mọi phép tính im lặng trả về 0, và bảng công
	// trông vẫn bình thường trong khi đã mất hết ý nghĩa.
	if s.Scope() == "company" {
		return apperror.Conflict(
			"Không xoá được khung giờ mặc định của công ty. Hãy sửa nó thay vì xoá.")
	}

	if err := u.schedules.Delete(ctx, id); err != nil {
		return apperror.Internal(err)
	}
	return nil
}

func validateScheduleInput(in ScheduleInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return apperror.Invalid("Tên khung giờ không được để trống", nil)
	}
	if in.DepartmentID != nil && in.EmployeeID != nil {
		return apperror.Invalid(
			"Một khung giờ chỉ áp dụng cho phòng ban HOẶC cá nhân, không cả hai", nil)
	}
	if in.BreakMinutes < 0 || in.BreakMinutes > 8*60 {
		return apperror.Invalid("Thời gian nghỉ không hợp lệ", nil)
	}
	if in.GraceMinutes < 0 || in.GraceMinutes > 120 {
		return apperror.Invalid("Số phút cho phép đi muộn không hợp lệ", nil)
	}
	for _, d := range in.Workdays {
		if d < 1 || d > 7 {
			return apperror.Invalid("Ngày làm việc phải từ 1 (thứ hai) tới 7 (chủ nhật)", nil)
		}
	}

	tmp := domainatt.Schedule{WorkStart: in.WorkStart, WorkEnd: in.WorkEnd}
	if tmp.StartMinutes() == 0 && in.WorkStart != "00:00" && in.WorkStart != "00:00:00" {
		return apperror.Invalid("Giờ bắt đầu phải có dạng HH:MM", nil)
	}
	if tmp.EndMinutes() <= tmp.StartMinutes() {
		return apperror.Invalid("Giờ kết thúc phải sau giờ bắt đầu", nil)
	}
	return nil
}

// =========================================================================
// NGÀY LỄ
// =========================================================================

func (u *Usecase) ListHolidays(ctx context.Context, year int) ([]*domainatt.Holiday, error) {
	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}

	loc := u.location(ctx)
	from := time.Date(year, 1, 1, 0, 0, 0, 0, loc)
	to := time.Date(year, 12, 31, 0, 0, 0, 0, loc)

	list, err := u.holidays.ListBetween(ctx, companyID, from, to)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

func (u *Usecase) CreateHoliday(
	ctx context.Context,
	date time.Time,
	name string,
	isPaid bool,
) (*domainatt.Holiday, error) {
	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, apperror.Invalid("Tên ngày lễ không được để trống", nil)
	}

	loc := u.location(ctx)
	h := &domainatt.Holiday{
		CompanyID: companyID,
		Date:      workDate(date, loc),
		Name:      strings.TrimSpace(name),
		IsPaid:    isPaid,
	}
	if err := u.holidays.Create(ctx, h); err != nil {
		return nil, apperror.Internal(err)
	}

	// Tổng hợp lại ngày đó: những người đã bị đánh dấu vắng mặt vì chưa biết
	// đây là ngày lễ phải được sửa lại ngay.
	if _, err := u.RollupDay(ctx, h.Date); err != nil {
		return h, nil
	}
	return h, nil
}

func (u *Usecase) DeleteHoliday(ctx context.Context, id uuid.UUID) error {
	if err := u.holidays.Delete(ctx, id); err != nil {
		return apperror.NotFound("ngày lễ")
	}
	return nil
}
