package attendance

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

type LeaveInput struct {
	Type      domainatt.LeaveType
	StartDate time.Time
	EndDate   time.Time
	DayPart   domainatt.DayPart
	Reason    string
}

type ListLeavesResult struct {
	Items      []*domainatt.LeaveRequest
	Page       int
	PageSize   int
	TotalItems int
	TotalPages int
}

// CountLeaveDays đếm số ngày nghỉ thực tế trong một khoảng.
//
// Bỏ qua cuối tuần và ngày lễ: nghỉ phép từ thứ sáu tới thứ hai là 2 ngày
// phép, không phải 4. Tính sai ở đây nghĩa là trừ oan quỹ phép của nhân
// viên, và đó là loại lỗi người ta nhớ rất lâu.
func (u *Usecase) CountLeaveDays(
	ctx context.Context,
	employeeID uuid.UUID,
	from, to time.Time,
	part domainatt.DayPart,
) (float64, error) {
	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return 0, err
	}

	holidays, err := u.holidays.ListBetween(ctx, companyID, from, to)
	if err != nil {
		return 0, apperror.Internal(err)
	}
	holidaySet := make(map[string]struct{}, len(holidays))
	for _, h := range holidays {
		holidaySet[h.Date.Format("2006-01-02")] = struct{}{}
	}

	schedule, err := u.resolveSchedule(ctx, employeeID, from)
	if err != nil {
		return 0, apperror.Internal(err)
	}

	days := 0.0
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if _, isHoliday := holidaySet[d.Format("2006-01-02")]; isHoliday {
			continue
		}
		if schedule != nil && !schedule.IsWorkday(d) {
			continue
		}
		days++
	}

	// Nửa ngày chỉ áp dụng cho đơn một ngày — ràng buộc này cũng được
	// database kiểm tra, xem chk_leave_day_part.
	if part != domainatt.PartFull && days > 0 {
		days = 0.5
	}
	return days, nil
}

func (u *Usecase) CreateLeave(
	ctx context.Context,
	actor *domainauth.Actor,
	in LeaveInput,
) (*domainatt.LeaveRequest, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if !in.Type.Valid() {
		return nil, apperror.Invalid("Loại nghỉ phép không hợp lệ", nil)
	}
	if in.DayPart == "" {
		in.DayPart = domainatt.PartFull
	}
	if !in.DayPart.Valid() {
		return nil, apperror.Invalid("Phần ngày nghỉ không hợp lệ", nil)
	}
	if in.EndDate.Before(in.StartDate) {
		return nil, apperror.Invalid("Ngày kết thúc phải sau ngày bắt đầu", nil)
	}
	if in.DayPart != domainatt.PartFull && !in.StartDate.Equal(in.EndDate) {
		return nil, apperror.Invalid("Nghỉ nửa ngày chỉ áp dụng cho đơn trong một ngày", nil)
	}
	if in.EndDate.Sub(in.StartDate) > 365*24*time.Hour {
		return nil, apperror.Invalid("Đơn nghỉ phép không quá một năm", nil)
	}

	// Chặn hai đơn cho cùng một ngày. Không chặn thì quỹ phép bị trừ hai lần
	// và bảng công có hai nguồn sự thật mâu thuẫn.
	overlapping, err := u.leaves.Overlapping(ctx, actor.EmployeeID,
		in.StartDate, in.EndDate, nil)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if len(overlapping) > 0 {
		return nil, apperror.Conflict(
			"Đã có đơn nghỉ phép trùng khoảng thời gian này")
	}

	days, err := u.CountLeaveDays(ctx, actor.EmployeeID, in.StartDate, in.EndDate, in.DayPart)
	if err != nil {
		return nil, err
	}
	if days <= 0 {
		return nil, apperror.Invalid(
			"Khoảng đã chọn không có ngày làm việc nào (toàn cuối tuần hoặc ngày lễ)", nil)
	}

	// Kiểm tra quỹ phép NGAY LÚC TẠO ĐƠN, không đợi tới lúc duyệt.
	//
	// Báo sớm để nhân viên biết mà chuyển sang nghỉ không lương, thay vì chờ
	// vài ngày rồi bị từ chối vì một lý do họ có thể tự thấy trước.
	if in.Type.DeductsBalance() {
		if err := u.checkBalance(ctx, actor.EmployeeID, in.StartDate.Year(), days); err != nil {
			return nil, err
		}
	}

	r := &domainatt.LeaveRequest{
		EmployeeID: actor.EmployeeID,
		Type:       in.Type,
		StartDate:  in.StartDate,
		EndDate:    in.EndDate,
		DayPart:    in.DayPart,
		Days:       days,
		Reason:     strings.TrimSpace(in.Reason),
	}
	if err := u.leaves.Create(ctx, r); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.leaves.GetByID(ctx, r.ID)
}

func (u *Usecase) checkBalance(
	ctx context.Context,
	employeeID uuid.UUID,
	year int,
	days float64,
) error {
	b, err := u.balances.Get(ctx, employeeID, year)
	if err != nil {
		// Chưa có dòng quỹ: chưa thiết lập cho năm nay. Không chặn — nhân sự
		// có thể chưa kịp khởi tạo, và chặn sẽ làm cả công ty không gửi được
		// đơn vào đầu tháng Một.
		return nil
	}
	if b.Remaining() < days {
		return apperror.Conflict(
			"Không đủ ngày phép. Còn lại " +
				formatDays(b.Remaining()) + " ngày, đơn cần " + formatDays(days) + " ngày.")
	}
	return nil
}

func (u *Usecase) ListLeaves(
	ctx context.Context,
	actor *domainauth.Actor,
	f domainatt.LeaveFilter,
) (*ListLeavesResult, error) {
	// Xoá sạch giá trị phạm vi có thể lọt vào từ query string.
	f.ScopedEmployeeIDs = nil
	f.RestrictScope = false

	if f.EmployeeID != nil {
		if err := u.canSeeLeave(ctx, actor, *f.EmployeeID); err != nil {
			return nil, err
		}
	} else {
		ids, restrict, err := u.leaveScope(ctx, actor)
		if err != nil {
			return nil, err
		}
		f.ScopedEmployeeIDs, f.RestrictScope = ids, restrict
	}

	f.Page, f.PageSize = normalizePage(f.Page, f.PageSize)

	items, total, err := u.leaves.List(ctx, f)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	return &ListLeavesResult{
		Items:      items,
		Page:       f.Page,
		PageSize:   f.PageSize,
		TotalItems: total,
		TotalPages: (total + f.PageSize - 1) / f.PageSize,
	}, nil
}

// leaveScope giống scopeFor nhưng dùng quyền leave:approve.
//
// Người duyệt đơn phải xem được đơn của nhân viên mình, kể cả khi họ không
// có quyền xem BẢNG CÔNG của những người đó — hai loại dữ liệu khác nhau,
// hai mức nhạy cảm khác nhau.
func (u *Usecase) leaveScope(
	ctx context.Context,
	actor *domainauth.Actor,
) ([]uuid.UUID, bool, error) {
	if actor == nil {
		return []uuid.UUID{}, true, nil
	}
	if !actor.Can(domainauth.PermLeaveApprove) {
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
		return append(list, actor.EmployeeID), true, nil
	default:
		return []uuid.UUID{actor.EmployeeID}, true, nil
	}
}

func (u *Usecase) canSeeLeave(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
) error {
	if actor == nil {
		return apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	if actor.EmployeeID == employeeID {
		return nil
	}
	ids, restrict, err := u.leaveScope(ctx, actor)
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
	return apperror.NotFound("đơn nghỉ phép")
}

// DecideLeave duyệt hoặc từ chối một đơn.
func (u *Usecase) DecideLeave(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	approve bool,
	note string,
) (*domainatt.LeaveRequest, error) {
	r, err := u.leaves.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("đơn nghỉ phép")
	}
	if err := u.canSeeLeave(ctx, actor, r.EmployeeID); err != nil {
		return nil, err
	}

	// Không ai tự duyệt đơn của chính mình, kể cả giám đốc.
	//
	// Đây là nguyên tắc kiểm soát nội bộ cơ bản, và nó phải nằm ở tầng
	// nghiệp vụ chứ không ở giao diện: ẩn nút không ngăn được một lời gọi
	// API trực tiếp.
	if actor != nil && actor.EmployeeID == r.EmployeeID {
		return nil, apperror.Forbidden("Không thể tự duyệt đơn của chính mình")
	}
	if r.Status.Final() {
		return nil, apperror.Conflict("Đơn này đã được xử lý rồi")
	}

	now := u.clock.Now()
	r.ApproverID = actorEmployeeID(actor)
	r.DecidedAt = &now
	r.DecisionNote = strings.TrimSpace(note)

	if approve {
		r.Status = domainatt.StatusApproved
	} else {
		r.Status = domainatt.StatusRejected
	}

	if err := u.leaves.Update(ctx, r); err != nil {
		return nil, apperror.Internal(err)
	}

	// Trừ quỹ phép SAU KHI đã ghi trạng thái duyệt.
	//
	// Thứ tự này quan trọng: trừ trước mà ghi trạng thái lỗi thì quỹ phép
	// mất ngày mà đơn vẫn treo — một sai lệch âm thầm và khó phát hiện.
	// Ngược lại, ghi trước mà trừ lỗi thì có thể tính lại từ danh sách đơn
	// đã duyệt.
	if approve && r.Type.DeductsBalance() {
		if err := u.balances.AddUsed(ctx, r.EmployeeID, r.StartDate.Year(), r.Days); err != nil {
			log := logger.FromContext(ctx)
			log.Error().Err(err).
				Str("leave_id", r.ID.String()).
				Msg("đơn đã duyệt nhưng không trừ được quỹ phép")
		}
	}

	// Đánh dấu lại bảng công những ngày trong đơn.
	//
	// Không làm thì những ngày nghỉ đã duyệt vẫn hiện là VẮNG MẶT cho tới
	// lần tổng hợp tiếp theo — và với ngày trong quá khứ thì không bao giờ.
	if approve {
		u.markLeaveDays(ctx, r)
	}

	return u.leaves.GetByID(ctx, id)
}

func (u *Usecase) markLeaveDays(ctx context.Context, r *domainatt.LeaveRequest) {
	log := logger.FromContext(ctx)
	for d := r.StartDate; !d.After(r.EndDate); d = d.AddDate(0, 0, 1) {
		if _, err := u.RollupDay(ctx, d); err != nil {
			log.Warn().Err(err).
				Str("day", d.Format("2006-01-02")).
				Msg("không tổng hợp lại được bảng công sau khi duyệt nghỉ phép")
		}
	}
}

// CancelLeave huỷ đơn của chính mình.
func (u *Usecase) CancelLeave(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) error {
	r, err := u.leaves.GetByID(ctx, id)
	if err != nil {
		return apperror.NotFound("đơn nghỉ phép")
	}
	if actor == nil || actor.EmployeeID != r.EmployeeID {
		return apperror.Forbidden("Chỉ người gửi mới huỷ được đơn của mình")
	}
	if r.Status == domainatt.StatusCancelled {
		return nil
	}
	if r.Status == domainatt.StatusRejected {
		return apperror.Conflict("Đơn đã bị từ chối, không cần huỷ")
	}

	// Đơn đã duyệt mà huỷ thì phải HOÀN quỹ phép, nếu không nhân viên mất
	// trắng số ngày đó.
	wasApproved := r.Status == domainatt.StatusApproved

	now := u.clock.Now()
	r.Status = domainatt.StatusCancelled
	r.DecidedAt = &now

	if err := u.leaves.Update(ctx, r); err != nil {
		return apperror.Internal(err)
	}

	if wasApproved && r.Type.DeductsBalance() {
		if err := u.balances.AddUsed(ctx, r.EmployeeID, r.StartDate.Year(), -r.Days); err != nil {
			log := logger.FromContext(ctx)
			log.Error().Err(err).Str("leave_id", r.ID.String()).
				Msg("huỷ đơn nhưng không hoàn được quỹ phép")
		}
		u.markLeaveDays(ctx, r)
	}
	return nil
}

// =========================================================================
// QUỸ NGÀY PHÉP
// =========================================================================

func (u *Usecase) GetBalance(
	ctx context.Context,
	actor *domainauth.Actor,
	employeeID uuid.UUID,
	year int,
) (*domainatt.Balance, error) {
	if err := u.canSeeLeave(ctx, actor, employeeID); err != nil {
		return nil, err
	}

	b, err := u.balances.Get(ctx, employeeID, year)
	if err != nil {
		// Chưa thiết lập: trả về quỹ rỗng thay vì 404. Màn hình "số phép còn
		// lại" luôn phải hiện được một con số, kể cả con số 0.
		return &domainatt.Balance{EmployeeID: employeeID, Year: year}, nil
	}
	return b, nil
}

func (u *Usecase) ListBalances(
	ctx context.Context,
	actor *domainauth.Actor,
	year int,
) ([]*domainatt.Balance, error) {
	ids, restrict, err := u.leaveScope(ctx, actor)
	if err != nil {
		return nil, err
	}

	list, err := u.balances.List(ctx, year, ids, restrict)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// SetBalance thiết lập quỹ phép. Chỉ người có quyền leave:manage.
func (u *Usecase) SetBalance(
	ctx context.Context,
	employeeID uuid.UUID,
	year int,
	entitled, carriedOver float64,
) (*domainatt.Balance, error) {
	if entitled < 0 || carriedOver < 0 {
		return nil, apperror.Invalid("Số ngày phép không được âm", nil)
	}
	if year < 2000 || year > 2200 {
		return nil, apperror.Invalid("Năm không hợp lệ", nil)
	}

	ok, err := u.employees.Exists(ctx, employeeID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if !ok {
		return nil, apperror.Invalid("Nhân viên không tồn tại hoặc đã nghỉ việc", nil)
	}

	b := &domainatt.Balance{
		EmployeeID:      employeeID,
		Year:            year,
		EntitledDays:    entitled,
		CarriedOverDays: carriedOver,
	}
	if err := u.balances.Upsert(ctx, b); err != nil {
		return nil, apperror.Internal(err)
	}
	return u.balances.Get(ctx, employeeID, year)
}

func actorEmployeeID(actor *domainauth.Actor) *uuid.UUID {
	if actor == nil {
		return nil
	}
	id := actor.EmployeeID
	return &id
}

// formatDays in số ngày phép gọn gàng: 3 thay vì 3.0, nhưng giữ 2.5.
func formatDays(d float64) string {
	if d == float64(int(d)) {
		return itoa(int(d))
	}
	// Chỉ có nửa ngày, nên một chữ số thập phân là đủ.
	whole := int(d)
	return itoa(whole) + ".5"
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
