// Package attendance là tầng nghiệp vụ của module chấm công và nghỉ phép.
package attendance

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

const (
	defaultPageSize = 20
	maxPageSize     = 100

	// Khoảng tối đa được phép truy vấn một lần. Không chặn thì ai đó hỏi
	// bảng công 10 năm và kéo sập database.
	maxRangeDays = 400
)

// CompanyLookup là cổng lấy công ty hiện tại và múi giờ của nó.
//
// Múi giờ là phần không thể thiếu: toàn bộ module này quy đổi thời điểm
// tuyệt đối thành "ngày làm việc", và dùng giờ máy chủ thay cho giờ công ty
// sẽ đẩy một phần ca đêm sang sai ngày.
type CompanyLookup interface {
	CurrentCompanyID(ctx context.Context) (uuid.UUID, error)
	CurrentTimezone(ctx context.Context) (*time.Location, error)
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type Usecase struct {
	schedules   domainatt.ScheduleRepository
	holidays    domainatt.HolidayRepository
	sessions    domainatt.SessionRepository
	days        domainatt.DayRepository
	adjustments domainatt.AdjustmentRepository
	leaves      domainatt.LeaveRepository
	balances    domainatt.BalanceRepository

	employees domainatt.EmployeeLookup
	presence  domainatt.PresenceReader
	company   CompanyLookup
	clock     domainatt.Clock
}

func NewUsecase(
	schedules domainatt.ScheduleRepository,
	holidays domainatt.HolidayRepository,
	sessions domainatt.SessionRepository,
	days domainatt.DayRepository,
	adjustments domainatt.AdjustmentRepository,
	leaves domainatt.LeaveRepository,
	balances domainatt.BalanceRepository,
	employees domainatt.EmployeeLookup,
	presence domainatt.PresenceReader,
	company CompanyLookup,
) *Usecase {
	return &Usecase{
		schedules:   schedules,
		holidays:    holidays,
		sessions:    sessions,
		days:        days,
		adjustments: adjustments,
		leaves:      leaves,
		balances:    balances,
		employees:   employees,
		presence:    presence,
		company:     company,
		clock:       systemClock{},
	}
}

// SetClock thay đồng hồ, dùng trong test.
func (u *Usecase) SetClock(c domainatt.Clock) { u.clock = c }

func normalizePage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	switch {
	case size <= 0:
		size = defaultPageSize
	case size > maxPageSize:
		size = maxPageSize
	}
	return page, size
}

// location trả về múi giờ công ty, rơi về giờ máy chủ khi chưa cấu hình.
func (u *Usecase) location(ctx context.Context) *time.Location {
	loc, err := u.company.CurrentTimezone(ctx)
	if err != nil || loc == nil {
		return time.Local
	}
	return loc
}

// Timezone công khai múi giờ công ty cho tầng gọi.
//
// Bộ lập lịch của worker cần nó để hẹn giờ chạy job tổng hợp theo giờ VIỆT
// NAM chứ không theo giờ máy chủ — job "chạy lúc 00:30" mà hiểu theo UTC sẽ
// nổ vào 07:30 sáng, giữa lúc mọi người đang bắt đầu làm việc.
func (u *Usecase) Timezone(ctx context.Context) (*time.Location, error) {
	return u.company.CurrentTimezone(ctx)
}

// workDate quy đổi một thời điểm tuyệt đối thành NGÀY LÀM VIỆC theo múi giờ
// công ty.
//
// Đây là phép đổi quan trọng nhất của cả module. Một người ở Việt Nam làm
// tới 23:30 ngày thứ hai; nếu máy chủ chạy UTC thì thời điểm đó đã là
// 16:30 cùng ngày — may mắn vẫn đúng ngày. Nhưng người làm tới 07:30 sáng
// thứ ba theo giờ Việt Nam lại là 00:30 thứ ba UTC ở một múi giờ khác, và
// ranh giới ngày trượt đi. Quy về giờ công ty rồi cắt ngày là cách duy nhất
// cho ra con số mà kế toán công nhận.
func workDate(t time.Time, loc *time.Location) time.Time {
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}

// =========================================================================
// PHẠM VI DỮ LIỆU
//
// Dữ liệu chấm công là dữ liệu cá nhân nhạy cảm. Mặc định là CHỈ XEM ĐƯỢC
// CỦA MÌNH; muốn xem của người khác phải có quyền attendance:read_all, và
// kể cả khi đó vẫn bị giới hạn trong phạm vi phòng ban của actor.
// =========================================================================

// scopeFor trả về danh sách nhân viên actor được xem, và cờ có giới hạn hay
// không.
//
// restrict = false nghĩa là xem tất cả. restrict = true kèm danh sách RỖNG
// nghĩa là không xem được ai — hai trường hợp này phải phân biệt được.
func (u *Usecase) scopeFor(
	ctx context.Context,
	actor *domainauth.Actor,
) (ids []uuid.UUID, restrict bool, err error) {
	if actor == nil {
		return []uuid.UUID{}, true, nil
	}

	// Không có quyền xem người khác: chỉ thấy chính mình, bất kể phạm vi
	// vai trò rộng tới đâu.
	if !actor.Can(domainauth.PermAttendanceReadAll) {
		return []uuid.UUID{actor.EmployeeID}, true, nil
	}

	switch actor.Scope {
	case domainauth.ScopeAll:
		return nil, false, nil

	case domainauth.ScopeDepartment:
		if len(actor.ManagedDepartmentIDs) == 0 {
			return []uuid.UUID{actor.EmployeeID}, true, nil
		}
		list, err := u.employees.ListActiveIDs(ctx, actor.ManagedDepartmentIDs)
		if err != nil {
			return nil, true, apperror.Internal(err)
		}
		// Luôn gồm cả chính mình: trưởng phòng có thể không thuộc phòng mình
		// quản lý (ví dụ phó giám đốc kiêm nhiệm).
		return append(list, actor.EmployeeID), true, nil

	default:
		return []uuid.UUID{actor.EmployeeID}, true, nil
	}
}

// canSee kiểm tra actor có được xem công của một người cụ thể không.
//
// PHẢI gọi trước mọi thao tác trên dữ liệu của một nhân viên. Middleware chỉ
// biết "người này có quyền attendance:read", nó không biết họ định xem công
// của ai.
func (u *Usecase) canSee(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
) error {
	if actor == nil {
		return apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if actor.EmployeeID == employeeID {
		return nil // luôn xem được của chính mình
	}

	ids, restrict, err := u.scopeFor(ctx, actor)
	if err != nil {
		return err
	}
	if !restrict {
		return nil
	}
	for _, id := range ids {
		if id == employeeID {
			return nil
		}
	}
	// 404 chứ không phải 403: xác nhận "người này có tồn tại nhưng bạn không
	// được xem" đã là rò rỉ thông tin. Cùng nguyên tắc với module nhân sự.
	return apperror.NotFound("dữ liệu chấm công")
}

// validRange kiểm tra khoảng ngày truy vấn.
func validRange(from, to time.Time) error {
	if to.Before(from) {
		return apperror.Invalid("Ngày kết thúc phải sau ngày bắt đầu", nil)
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		return apperror.Invalid("Khoảng thời gian quá dài, tối đa 400 ngày", nil)
	}
	return nil
}
