package project

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// =========================================================================
// GHI NHẬN THỜI GIAN
// =========================================================================

type TimelogInput struct {
	SpentMinutes int
	Note         string
	LoggedOn     time.Time
}

func (u *Usecase) ListTimelogs(
	ctx context.Context,
	actor *domainauth.Actor,
	taskID uuid.UUID,
) ([]*domainproject.Timelog, error) {
	if _, err := u.GetTask(ctx, actor, taskID); err != nil {
		return nil, err
	}
	list, err := u.timelogs.ListByTask(ctx, taskID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

func (u *Usecase) LogTime(
	ctx context.Context,
	actor *domainauth.Actor,
	taskID uuid.UUID,
	in TimelogInput,
) (*domainproject.Timelog, error) {
	t, err := u.GetTask(ctx, actor, taskID)
	if err != nil {
		return nil, err
	}
	acc, err := u.loadAccess(ctx, actor, t.ProjectID)
	if err != nil {
		return nil, err
	}
	if !acc.canWrite() {
		return nil, apperror.Forbidden("Bạn chỉ có quyền xem dự án này")
	}

	if in.SpentMinutes <= 0 {
		return nil, apperror.Invalid("Thời gian phải lớn hơn 0", nil)
	}
	// Chặn trên 24 giờ cho một lần ghi. Không phải để soi nhân viên mà để
	// bắt lỗi nhập: gõ nhầm 480 thành 4800 phút là 80 giờ, và con số đó sẽ
	// âm thầm làm hỏng mọi báo cáo sau này.
	if in.SpentMinutes > 24*60 {
		return nil, apperror.Invalid("Một lần ghi không quá 24 giờ", nil)
	}

	loggedOn := in.LoggedOn
	if loggedOn.IsZero() {
		loggedOn = time.Now()
	}
	// Không cho ghi cho ngày trong tương lai — chắc chắn là lỗi nhập liệu.
	if loggedOn.After(time.Now().Add(24 * time.Hour)) {
		return nil, apperror.Invalid("Không ghi được thời gian cho ngày trong tương lai", nil)
	}

	tl := &domainproject.Timelog{
		TaskID:       taskID,
		EmployeeID:   actor.EmployeeID,
		SpentMinutes: in.SpentMinutes,
		Note:         in.Note,
		LoggedOn:     loggedOn,
	}
	if err := u.timelogs.Create(ctx, tl); err != nil {
		return nil, apperror.Internal(err)
	}

	u.logActivity(ctx, &domainproject.Activity{
		TaskID:   taskID,
		ActorID:  actorEmployeeID(actor),
		Action:   domainproject.ActionTimeLogged,
		NewValue: minutesLabel(in.SpentMinutes),
	})

	return u.timelogs.GetByID(ctx, tl.ID)
}

func (u *Usecase) DeleteTimelog(
	ctx context.Context,
	actor *domainauth.Actor,
	timelogID uuid.UUID,
) error {
	tl, err := u.timelogs.GetByID(ctx, timelogID)
	if err != nil {
		return apperror.NotFound("bản ghi thời gian")
	}
	t, err := u.GetTask(ctx, actor, tl.TaskID)
	if err != nil {
		return apperror.NotFound("bản ghi thời gian")
	}
	acc, err := u.loadAccess(ctx, actor, t.ProjectID)
	if err != nil {
		return apperror.NotFound("bản ghi thời gian")
	}
	if tl.EmployeeID != actor.EmployeeID && !acc.canManage() {
		return apperror.Forbidden("Chỉ người ghi hoặc chủ dự án mới xoá được bản ghi này")
	}

	if err := u.timelogs.Delete(ctx, timelogID); err != nil {
		return apperror.Internal(err)
	}
	return nil
}

// =========================================================================
// BÁO CÁO
// =========================================================================

func (u *Usecase) ProjectProgress(
	ctx context.Context,
	actor *domainauth.Actor,
	projectID uuid.UUID,
) (*domainproject.ProjectProgress, error) {
	if _, err := u.loadAccess(ctx, actor, projectID); err != nil {
		return nil, err
	}
	p, err := u.reports.ProjectProgress(ctx, projectID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return p, nil
}

// OverdueTasks liệt kê việc quá hạn trong phạm vi actor được xem.
func (u *Usecase) OverdueTasks(
	ctx context.Context,
	actor *domainauth.Actor,
	projectID *uuid.UUID,
	page, pageSize int,
) (*ListTasksResult, error) {
	now := time.Now()
	f := domainproject.TaskFilter{
		ProjectID:  projectID,
		DueBefore:  &now,
		Unfinished: true,
		Page:       page,
		PageSize:   pageSize,
		SortBy:     "due_date",
	}
	return u.ListTasks(ctx, actor, f)
}

// Workload thống kê khối lượng việc theo nhân viên.
//
// Phạm vi bám theo phạm vi dự án của actor: trưởng phòng chỉ thấy khối lượng
// trong những dự án mình tham gia, giám đốc thấy toàn công ty. Không giới
// hạn ở đây thì bất kỳ ai cũng đọc được bức tranh nhân sự của cả công ty.
func (u *Usecase) Workload(
	ctx context.Context,
	actor *domainauth.Actor,
	projectID *uuid.UUID,
) ([]*domainproject.Workload, error) {
	var (
		ids      []uuid.UUID
		restrict bool
	)

	if projectID != nil {
		if _, err := u.loadAccess(ctx, actor, *projectID); err != nil {
			return nil, err
		}
		ids, restrict = []uuid.UUID{*projectID}, true
	} else {
		var err error
		ids, restrict, err = u.visibleProjectIDs(ctx, actor)
		if err != nil {
			return nil, err
		}
	}

	list, err := u.reports.Workload(ctx, ids, restrict)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

func minutesLabel(m int) string {
	h := m / 60
	rem := m % 60
	switch {
	case h == 0:
		return itoa(rem) + " phút"
	case rem == 0:
		return itoa(h) + " giờ"
	default:
		return itoa(h) + " giờ " + itoa(rem) + " phút"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
