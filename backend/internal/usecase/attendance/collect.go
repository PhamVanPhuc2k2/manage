package attendance

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// CollectInterval là chu kỳ chạy job thu thập presence.
//
// Mỗi lần quét đại diện cho ĐÚNG một phút thời gian làm việc, nên chu kỳ
// này cũng chính là đơn vị đo của cả hệ thống. Đổi nó mà quên đổi phép tính
// active_minutes bên dưới sẽ làm sai lệch toàn bộ số liệu.
const CollectInterval = time.Minute

// CollectResult là kết quả một lần quét, dùng để ghi log và chẩn đoán.
type CollectResult struct {
	Online   int
	Active   int
	Extended int
	Created  int
}

// CollectPresence quét Redis và dựng phiên làm việc.
//
// Đây là trái tim của cơ chế chấm công tự động. Chạy mỗi phút từ worker.
//
// Vì sao quét định kỳ thay vì ghi ngay khi nhận heartbeat: heartbeat đến 30
// giây một lần từ MỌI người đang online. Với 200 nhân viên là 400 lượt ghi
// database mỗi phút, để thu được đúng lượng thông tin mà một lần quét gom
// lại được trong một giao dịch. Cái giá là độ phân giải 1 phút — quá đủ cho
// một bảng chấm công.
//
// Job này PHẢI chịu được chạy lại: worker restart giữa chừng, hoặc hai
// worker cùng chạy. Cơ chế ExtendLatest gộp mọi thứ vào một câu UPDATE có
// FOR UPDATE nên hai lần quét trùng nhau chỉ nới dài cùng một phiên.
func (u *Usecase) CollectPresence(ctx context.Context) (CollectResult, error) {
	log := logger.FromContext(ctx)
	var res CollectResult

	snapshots, err := u.presence.SnapshotOnline(ctx)
	if err != nil {
		return res, apperror.Internal(err)
	}
	if len(snapshots) == 0 {
		return res, nil
	}

	loc := u.location(ctx)
	now := u.clock.Now()

	for _, s := range snapshots {
		res.Online++

		// Mỗi lần quét đại diện cho một phút. Người đang hoạt động được cộng
		// một phút hoạt động; người chỉ mở tab thì không — đó chính là chỗ
		// phân biệt "online" với "đang làm việc".
		activeMinutes := 0
		if s.IsActive {
			activeMinutes = int(CollectInterval.Minutes())
			res.Active++
		}

		extended, err := u.sessions.ExtendLatest(
			ctx, s.EmployeeID, now, activeMinutes, domainatt.SessionGap)
		if err != nil {
			// Một người lỗi không được làm hỏng cả lượt quét: 199 người còn
			// lại vẫn phải được ghi nhận.
			log.Warn().Err(err).
				Str("employee_id", s.EmployeeID.String()).
				Msg("không nới dài được phiên làm việc")
			continue
		}
		if extended {
			res.Extended++
			continue
		}

		// Không có phiên nào để nối: mở phiên mới.
		//
		// Thời điểm bắt đầu lùi lại đúng một chu kỳ quét. Lấy `now` làm điểm
		// bắt đầu sẽ tạo ra phiên dài 0 phút, và phút làm việc đầu tiên của
		// mỗi người biến mất khỏi bảng công mỗi ngày.
		session := &domainatt.Session{
			EmployeeID:    s.EmployeeID,
			WorkDate:      workDate(now, loc),
			StartedAt:     now.Add(-CollectInterval),
			EndedAt:       now,
			ActiveMinutes: activeMinutes,
			Source:        domainatt.SourcePresence,
		}
		if err := u.sessions.Create(ctx, session); err != nil {
			log.Warn().Err(err).
				Str("employee_id", s.EmployeeID.String()).
				Msg("không tạo được phiên làm việc")
			continue
		}
		res.Created++
	}

	log.Debug().
		Int("online", res.Online).
		Int("active", res.Active).
		Int("extended", res.Extended).
		Int("created", res.Created).
		Msg("đã quét presence")

	return res, nil
}

// RollupResult là kết quả một lần tổng hợp cuối ngày.
type RollupResult struct {
	Day       time.Time
	Processed int
	Present   int
	Absent    int
	OnLeave   int
	Locked    int
}

// RollupDay tổng hợp phiên làm việc của một ngày thành một dòng bảng công
// cho MỌI nhân viên đang làm việc.
//
// Chạy cho TẤT CẢ nhân viên chứ không chỉ những người có phiên: người vắng
// mặt không có phiên nào, nên nếu chỉ duyệt bảng phiên thì họ đơn giản
// không xuất hiện trong bảng công — và "không có dòng" rất khác với "có
// dòng ghi vắng mặt" khi tính lương.
func (u *Usecase) RollupDay(ctx context.Context, day time.Time) (RollupResult, error) {
	log := logger.FromContext(ctx)

	loc := u.location(ctx)
	target := workDate(day, loc)
	res := RollupResult{Day: target}

	employees, err := u.employees.ListActiveIDs(ctx, nil)
	if err != nil {
		return res, apperror.Internal(err)
	}

	companyID, err := u.company.CurrentCompanyID(ctx)
	if err != nil {
		return res, err
	}

	// Đọc ngày lễ và đơn nghỉ MỘT LẦN cho cả ngày, thay vì hỏi lại cho từng
	// nhân viên — 200 người sẽ thành 400 truy vấn thừa.
	holidays, err := u.holidays.ListBetween(ctx, companyID, target, target)
	if err != nil {
		return res, apperror.Internal(err)
	}
	isHoliday := len(holidays) > 0

	onLeave, err := u.leaves.ApprovedOn(ctx, target)
	if err != nil {
		return res, apperror.Internal(err)
	}

	for _, empID := range employees {
		schedule, err := u.resolveSchedule(ctx, empID, target)
		if err != nil {
			log.Warn().Err(err).Str("employee_id", empID.String()).
				Msg("không đọc được khung giờ làm việc")
			continue
		}

		online, active, first, last, err := u.sessions.AggregateDay(ctx, empID, target)
		if err != nil {
			log.Warn().Err(err).Str("employee_id", empID.String()).
				Msg("không tổng hợp được phiên làm việc")
			continue
		}

		d := &domainatt.Day{
			EmployeeID:    empID,
			WorkDate:      target,
			OnlineMinutes: online,
			ActiveMinutes: active,
			FirstSeenAt:   first,
			LastSeenAt:    last,
		}

		// Thứ tự xét trạng thái quan trọng: ngày lễ và cuối tuần thắng tất
		// cả, vì không ai phải đi làm thì không thể tính là vắng.
		switch {
		case isHoliday:
			d.Status = domainatt.DayHoliday
		case schedule != nil && !schedule.IsWorkday(target):
			d.Status = domainatt.DayWeekend
		case onLeave[empID] != nil:
			d.Status = domainatt.DayLeave
		case online > 0:
			d.Status = domainatt.DayPresent
			u.applyLateness(d, schedule, loc)
		default:
			d.Status = domainatt.DayAbsent
			if schedule != nil {
				d.ShortfallMinutes = schedule.ExpectedMinutes()
			}
		}

		if err := u.days.Upsert(ctx, d); err != nil {
			// Kỳ đã khoá: bỏ qua trong im lặng là ĐÚNG, không phải lỗi.
			// Đó chính là tác dụng của việc khoá kỳ lương.
			if err == domainatt.ErrLocked {
				res.Locked++
				continue
			}
			log.Warn().Err(err).Str("employee_id", empID.String()).
				Msg("không ghi được bảng công ngày")
			continue
		}

		res.Processed++
		switch d.Status {
		case domainatt.DayPresent:
			res.Present++
		case domainatt.DayAbsent:
			res.Absent++
		case domainatt.DayLeave:
			res.OnLeave++
		}
	}

	log.Info().
		Str("day", target.Format("2006-01-02")).
		Int("processed", res.Processed).
		Int("present", res.Present).
		Int("absent", res.Absent).
		Int("leave", res.OnLeave).
		Int("locked", res.Locked).
		Msg("đã tổng hợp bảng công ngày")

	return res, nil
}

// applyLateness tính đi muộn, về sớm và thiếu giờ.
//
// Mọi phép so sánh đều quy về SỐ PHÚT KỂ TỪ NỬA ĐÊM theo giờ công ty, không
// so sánh trực tiếp hai time.Time: khung giờ làm việc là "08:00" — một mốc
// trong ngày, không gắn với ngày cụ thể nào.
func (u *Usecase) applyLateness(
	d *domainatt.Day,
	s *domainatt.Schedule,
	loc *time.Location,
) {
	if s == nil {
		return
	}

	expected := s.ExpectedMinutes()
	if d.OnlineMinutes < expected {
		d.ShortfallMinutes = expected - d.OnlineMinutes
	}

	if d.FirstSeenAt != nil {
		first := d.FirstSeenAt.In(loc)
		firstMin := first.Hour()*60 + first.Minute()
		// grace_minutes: kẹt xe vài phút không phải vi phạm. Thiếu nó thì
		// bảng công đầy những con số 2 phút, 3 phút — không ai đọc nữa.
		if late := firstMin - s.StartMinutes() - s.GraceMinutes; late > 0 {
			d.LateMinutes = late
		}
	}

	if d.LastSeenAt != nil {
		last := d.LastSeenAt.In(loc)
		lastMin := last.Hour()*60 + last.Minute()
		if early := s.EndMinutes() - lastMin; early > 0 {
			d.EarlyLeaveMinutes = early
		}
	}
}

// resolveSchedule chọn khung giờ làm việc áp dụng cho một người vào một ngày.
//
// Luật ưu tiên: cá nhân > phòng ban > công ty. Khi cùng mức thì lấy cái có
// hiệu lực muộn nhất — đó là bản cập nhật gần nhất.
//
// Luật này nằm ở tầng nghiệp vụ chứ không trong SQL vì nó LÀ nghiệp vụ: nó
// sẽ được hỏi tới, tranh luận và sửa đổi, và nó cần ở chỗ đọc được.
func (u *Usecase) resolveSchedule(
	ctx context.Context,
	employeeID uuid.UUID,
	on time.Time,
) (*domainatt.Schedule, error) {
	list, err := u.schedules.Applicable(ctx, employeeID, on)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, nil
	}

	best := list[0]
	for _, s := range list[1:] {
		switch {
		case s.Specificity() > best.Specificity():
			best = s
		case s.Specificity() == best.Specificity() &&
			s.EffectiveFrom.After(best.EffectiveFrom):
			best = s
		}
	}
	return best, nil
}
