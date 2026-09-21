package attendance

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// TodayStatus là thứ hiển thị trên widget góc trên màn hình.
type TodayStatus struct {
	EmployeeID    uuid.UUID
	WorkDate      time.Time
	Status        string // online / idle / offline
	OnlineMinutes int
	ActiveMinutes int
	FirstSeenAt   *time.Time
	ExpectedMins  int
	ScheduleName  string
}

// Today tổng hợp tình hình hôm nay của chính actor.
//
// Đọc TRỰC TIẾP từ bảng phiên chứ không từ bảng công ngày: bảng công ngày
// chỉ được job cuối ngày ghi, nên trong lúc đang làm việc nó vẫn là dữ liệu
// của hôm qua. Người dùng nhìn widget này để biết mình đã làm bao lâu HÔM
// NAY, và một con số cũ 24 giờ thì vô nghĩa.
func (u *Usecase) Today(
	ctx context.Context,
	actor *domainauth.Actor,
) (*TodayStatus, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}

	loc := u.location(ctx)
	today := workDate(u.clock.Now(), loc)

	online, active, first, _, err := u.sessions.AggregateDay(ctx, actor.EmployeeID, today)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	out := &TodayStatus{
		EmployeeID:    actor.EmployeeID,
		WorkDate:      today,
		Status:        string("offline"),
		OnlineMinutes: online,
		ActiveMinutes: active,
		FirstSeenAt:   first,
	}

	if statuses, err := u.presence.StatusOf(ctx, []uuid.UUID{actor.EmployeeID}); err == nil {
		if s, ok := statuses[actor.EmployeeID]; ok && s != "" {
			out.Status = s
		}
	}

	if s, err := u.resolveSchedule(ctx, actor.EmployeeID, today); err == nil && s != nil {
		out.ExpectedMins = s.ExpectedMinutes()
		out.ScheduleName = s.Name
	}
	return out, nil
}

// ListDays trả về bảng công theo khoảng ngày, đã áp phạm vi của actor.
func (u *Usecase) ListDays(
	ctx context.Context,
	actor *domainauth.Actor,
	f domainatt.DayFilter,
) ([]*domainatt.Day, error) {
	if err := validRange(f.From, f.To); err != nil {
		return nil, err
	}

	// Xoá sạch giá trị phạm vi có thể lọt vào từ query string.
	f.ScopedEmployeeIDs = nil
	f.RestrictScope = false

	if f.EmployeeID != nil {
		if err := u.canSee(ctx, actor, *f.EmployeeID); err != nil {
			return nil, err
		}
	} else {
		ids, restrict, err := u.scopeFor(ctx, actor)
		if err != nil {
			return nil, err
		}
		f.ScopedEmployeeIDs, f.RestrictScope = ids, restrict
	}

	list, err := u.days.List(ctx, f)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// DayDetail là một ngày kèm chi tiết từng phiên, phục vụ dòng thời gian.
type DayDetail struct {
	Day      *domainatt.Day
	Sessions []*domainatt.Session
}

func (u *Usecase) GetDay(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
	day time.Time,
) (*DayDetail, error) {
	if err := u.canSee(ctx, actor, employeeID); err != nil {
		return nil, err
	}

	loc := u.location(ctx)
	target := workDate(day, loc)

	sessions, err := u.sessions.ListByDay(ctx, employeeID, target)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	d, err := u.days.GetByDate(ctx, employeeID, target)
	if err != nil {
		// Chưa tổng hợp (hôm nay, hoặc job chưa chạy): dựng một bản tạm từ
		// chính các phiên. Trả 404 ở đây sẽ khiến màn hình chấm công của
		// ngày hôm nay luôn trống, đúng lúc người dùng cần nó nhất.
		online, active, first, last, aggErr := u.sessions.AggregateDay(ctx, employeeID, target)
		if aggErr != nil {
			return nil, apperror.Internal(aggErr)
		}
		d = &domainatt.Day{
			EmployeeID:    employeeID,
			WorkDate:      target,
			OnlineMinutes: online,
			ActiveMinutes: active,
			FirstSeenAt:   first,
			LastSeenAt:    last,
			Status:        domainatt.DayAbsent,
		}
		if online > 0 {
			d.Status = domainatt.DayPresent
		}
	}

	return &DayDetail{Day: d, Sessions: sessions}, nil
}

// MonthSummary tổng hợp công theo tháng.
func (u *Usecase) MonthSummary(
	ctx context.Context,
	actor *domainauth.Actor,
	year, month int,
	employeeID, departmentID *uuid.UUID,
) ([]*domainatt.MonthSummary, error) {
	if month < 1 || month > 12 {
		return nil, apperror.Invalid("Tháng không hợp lệ", nil)
	}
	if year < 2000 || year > 2200 {
		return nil, apperror.Invalid("Năm không hợp lệ", nil)
	}

	loc := u.location(ctx)
	from := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, loc)
	to := from.AddDate(0, 1, -1)

	f := domainatt.DayFilter{
		From:         from,
		To:           to,
		EmployeeID:   employeeID,
		DepartmentID: departmentID,
	}

	if employeeID != nil {
		if err := u.canSee(ctx, actor, *employeeID); err != nil {
			return nil, err
		}
	} else {
		ids, restrict, err := u.scopeFor(ctx, actor)
		if err != nil {
			return nil, err
		}
		f.ScopedEmployeeIDs, f.RestrictScope = ids, restrict
	}

	list, err := u.days.MonthSummary(ctx, f)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// TeamMember là một dòng trên màn hình "ai đang online".
type TeamMember struct {
	EmployeeID uuid.UUID
	Status     string
}

// TeamPresence trả về trạng thái hiện diện của những người actor được xem.
//
// Chỉ trả về TRẠNG THÁI, không trả về số giờ: đây là màn hình để biết ai
// đang có mặt mà hỏi việc, không phải công cụ theo dõi. Ai muốn xem số liệu
// công thì vào bảng công, nơi có ghi rõ ràng và có thể điều chỉnh.
func (u *Usecase) TeamPresence(
	ctx context.Context,
	actor *domainauth.Actor,
) ([]*TeamMember, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}

	ids, restrict, err := u.scopeFor(ctx, actor)
	if err != nil {
		return nil, err
	}
	if !restrict {
		// Phạm vi toàn công ty: lấy toàn bộ nhân viên đang làm việc.
		ids, err = u.employees.ListActiveIDs(ctx, nil)
		if err != nil {
			return nil, apperror.Internal(err)
		}
	}

	statuses, err := u.presence.StatusOf(ctx, ids)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	out := make([]*TeamMember, 0, len(ids))
	for _, id := range ids {
		s := statuses[id]
		if s == "" {
			s = "offline"
		}
		out = append(out, &TeamMember{EmployeeID: id, Status: s})
	}
	return out, nil
}

// CheckIn ghi một phiên THỦ CÔNG.
//
// Cần thiết cho các trường hợp presence không ghi nhận được: mất mạng, họp ở
// ngoài, quên mở máy. Không có đường sửa hợp lệ thì người dùng sẽ tìm cách
// khác và dữ liệu công mất hết giá trị.
//
// Phiên thủ công được đánh dấu source = 'manual' để người duyệt phân biệt
// được với dữ liệu tự động.
func (u *Usecase) CheckIn(
	ctx context.Context,
	actor *domainauth.Actor,
	start, end time.Time,
	note string,
) (*domainatt.Session, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if !end.After(start) {
		return nil, apperror.Invalid("Giờ kết thúc phải sau giờ bắt đầu", nil)
	}
	if end.Sub(start) > 24*time.Hour {
		return nil, apperror.Invalid("Một phiên không quá 24 giờ", nil)
	}

	now := u.clock.Now()
	if start.After(now.Add(5 * time.Minute)) {
		return nil, apperror.Invalid("Không ghi được thời gian trong tương lai", nil)
	}

	loc := u.location(ctx)
	target := workDate(start, loc)

	// Ngày đã khoá thì không thêm phiên được: thêm vào rồi thì tổng hợp
	// không chạy lại được nữa (Upsert bỏ qua dòng khoá), và bảng công sẽ
	// mâu thuẫn với chính các phiên của nó.
	if d, err := u.days.GetByDate(ctx, actor.EmployeeID, target); err == nil && d.IsLocked {
		return nil, apperror.Conflict("Kỳ công của ngày này đã khoá")
	}

	s := &domainatt.Session{
		EmployeeID:    actor.EmployeeID,
		WorkDate:      target,
		StartedAt:     start,
		EndedAt:       end,
		ActiveMinutes: int(end.Sub(start).Minutes()),
		Source:        domainatt.SourceManual,
		Note:          note,
	}
	if err := u.sessions.Create(ctx, s); err != nil {
		return nil, apperror.Internal(err)
	}

	// Tổng hợp lại ngay để người dùng thấy con số cập nhật, không phải chờ
	// tới job cuối ngày.
	if _, err := u.RollupDay(ctx, target); err != nil {
		return s, nil // phiên đã ghi thành công, tổng hợp lỗi không phải lỗi của họ
	}
	return s, nil
}
