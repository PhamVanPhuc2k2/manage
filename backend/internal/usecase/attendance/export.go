package attendance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// ExportJobPublisher đẩy một lượt xuất sang worker.
type ExportJobPublisher interface {
	PublishAttendanceExport(ctx context.Context, exportID uuid.UUID) error
}

// ExportNotifier báo cho người yêu cầu biết tệp đã sẵn sàng. Là một hàm để
// module chấm công không phụ thuộc module thông báo — worker nối dây.
type ExportNotifier func(ctx context.Context, recipientEmployeeID uuid.UUID, title, body, link string) error

func (u *Usecase) SetExports(repo domainatt.ExportRepository) { u.exports = repo }

// SetExportJobs chọn chạy nền. Để nil thì RequestExport làm ngay trong
// request — chỉ dùng cho test.
func (u *Usecase) SetExportJobs(p ExportJobPublisher) { u.exportJobs = p }

func (u *Usecase) SetExportNotifier(fn ExportNotifier) { u.exportNotifier = fn }

// RequestExport ghi nhận một yêu cầu xuất và đẩy sang worker.
//
// Chỉ kiểm những gì kiểm được ngay (tháng hợp lệ, không phải tháng tương
// lai). Dữ liệu được đọc ở worker, theo đúng phạm vi quyền của người yêu
// cầu lúc bấm.
func (u *Usecase) RequestExport(
	ctx context.Context,
	actor *domainauth.Actor,
	year, month int,
	departmentID *uuid.UUID,
) (*domainatt.Export, error) {
	if u.exports == nil {
		return nil, apperror.New(apperror.KindUnprocessable, "Chức năng xuất báo cáo chưa được bật")
	}
	if month < 1 || month > 12 || year < 2000 || year > 2200 {
		return nil, apperror.Invalid("Tháng hoặc năm không hợp lệ", nil)
	}
	now := u.clock.Now().In(u.location(ctx))
	if year > now.Year() || (year == now.Year() && month > int(now.Month())) {
		return nil, apperror.Invalid("Không xuất được báo cáo cho tháng chưa tới", nil)
	}

	creator := actor.EmployeeID
	e := &domainatt.Export{
		Year:         year,
		Month:        month,
		DepartmentID: departmentID,
		Status:       domainatt.ExportQueued,
		RequestedBy:  &creator,
		Actor: domainatt.ExportActor{
			UserID:               actor.UserID,
			EmployeeID:           actor.EmployeeID,
			Scope:                string(actor.Scope),
			ManagedDepartmentIDs: actor.ManagedDepartmentIDs,
		},
	}
	if err := u.exports.Create(ctx, e); err != nil {
		return nil, apperror.Internal(err)
	}

	if u.exportJobs == nil {
		if err := u.ProcessExport(ctx, e.ID); err != nil {
			return nil, err
		}
	} else if err := u.exportJobs.PublishAttendanceExport(ctx, e.ID); err != nil {
		// Không đẩy được thì đóng lượt xuất ngay — để nó nằm ở 'queued'
		// thì trang danh sách quay vòng mãi.
		_ = u.exports.Fail(ctx, e.ID, "Không gửi được sang hàng đợi xử lý")
		return nil, apperror.Internal(err)
	}
	return u.exports.GetByID(ctx, e.ID)
}

// ListExports trả về 20 lượt xuất gần nhất mà actor được xem: phạm vi toàn
// công ty thấy của mọi người, còn lại chỉ thấy của mình.
func (u *Usecase) ListExports(ctx context.Context, actor *domainauth.Actor) ([]*domainatt.Export, error) {
	if u.exports == nil {
		return nil, nil
	}
	var by *uuid.UUID
	if actor.Scope != domainauth.ScopeAll {
		id := actor.EmployeeID
		by = &id
	}
	list, err := u.exports.List(ctx, by, 20)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// ExportFile trả về tệp đã xuất. Cùng luật xem như ListExports.
func (u *Usecase) ExportFile(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) (string, []byte, error) {
	if u.exports == nil {
		return "", nil, apperror.NotFound("tệp")
	}
	e, err := u.exports.GetByID(ctx, id)
	if err != nil {
		return "", nil, apperror.NotFound("tệp")
	}
	if actor.Scope != domainauth.ScopeAll &&
		(e.RequestedBy == nil || *e.RequestedBy != actor.EmployeeID) {
		return "", nil, apperror.NotFound("tệp")
	}
	name, data, err := u.exports.GetFile(ctx, id)
	if errors.Is(err, domainatt.ErrNotFound) {
		return "", nil, apperror.New(apperror.KindNotFound,
			"Tệp không còn — tệp chỉ giữ 7 ngày, hãy xuất lại")
	}
	if err != nil {
		return "", nil, apperror.Internal(err)
	}
	return name, data, nil
}

// ProcessExport dựng tệp Excel cho một lượt xuất. Gọi từ worker.
//
// Chạy lại an toàn: tệp được dựng lại từ đầu và ghi đè, không có trạng
// thái nửa chừng nào để lại.
func (u *Usecase) ProcessExport(ctx context.Context, id uuid.UUID) error {
	log := logger.FromContext(ctx)

	e, err := u.exports.GetByID(ctx, id)
	if errors.Is(err, domainatt.ErrNotFound) {
		log.Warn().Str("export_id", id.String()).Msg("không thấy lượt xuất, bỏ qua")
		return nil
	}
	if err != nil {
		return err
	}
	if e.Status.Finished() {
		return nil
	}
	if err := u.exports.MarkProcessing(ctx, id); err != nil {
		return err
	}

	// Dựng lại actor từ ảnh chụp. Route đã đòi attendance:read_all nên
	// quyền đó có sẵn; phần còn lại là phạm vi.
	actor := &domainauth.Actor{
		UserID:               e.Actor.UserID,
		EmployeeID:           e.Actor.EmployeeID,
		Scope:                domainauth.Scope(e.Actor.Scope),
		ManagedDepartmentIDs: e.Actor.ManagedDepartmentIDs,
		Permissions:          map[string]struct{}{domainauth.PermAttendanceReadAll: {}},
	}

	summaries, err := u.MonthSummary(ctx, actor, e.Year, e.Month, nil, e.DepartmentID)
	if err != nil {
		return u.failExport(ctx, e, err)
	}
	loc := u.location(ctx)
	from := time.Date(e.Year, time.Month(e.Month), 1, 0, 0, 0, 0, loc)
	days, err := u.ListDays(ctx, actor, domainatt.DayFilter{
		From:         from,
		To:           from.AddDate(0, 1, -1),
		DepartmentID: e.DepartmentID,
	})
	if err != nil {
		return u.failExport(ctx, e, err)
	}

	data, err := buildAttendanceWorkbook(e, summaries, days)
	if err != nil {
		return u.failExport(ctx, e, err)
	}
	name := fmt.Sprintf("cham-cong-%04d-%02d.xlsx", e.Year, e.Month)
	if err := u.exports.Complete(ctx, id, name, data, len(summaries)); err != nil {
		return err
	}

	// Dọn tệp cũ ở đây thay vì một job định kỳ riêng: có lượt xuất mới
	// mới có tệp mới, nên dọn cùng lúc là đủ để kho không phình ra.
	if n, err := u.exports.PurgeFiles(ctx, u.clock.Now().Add(-domainatt.ExportRetention)); err != nil {
		log.Warn().Err(err).Msg("không dọn được tệp xuất cũ")
	} else if n > 0 {
		log.Info().Int64("files", n).Msg("đã dọn tệp xuất quá hạn")
	}

	log.Info().Str("export_id", id.String()).Int("employees", len(summaries)).
		Msg("đã xuất xong báo cáo chấm công")

	if u.exportNotifier != nil && e.RequestedBy != nil {
		title := fmt.Sprintf("Báo cáo chấm công tháng %d/%d đã sẵn sàng", e.Month, e.Year)
		body := fmt.Sprintf("%d nhân viên. Tải về trong vòng 7 ngày.", len(summaries))
		if err := u.exportNotifier(ctx, *e.RequestedBy, title, body, "/attendance/exports"); err != nil {
			log.Error().Err(err).Msg("không gửi được thông báo xuất xong")
		}
	}
	return nil
}

// failExport ghi lỗi của cả lượt. Trả nil để message không bị giao lại:
// lỗi ở đây là lỗi dữ liệu hoặc quyền, chạy lại cũng ra đúng lỗi đó.
func (u *Usecase) failExport(ctx context.Context, e *domainatt.Export, cause error) error {
	msg := "Không dựng được tệp báo cáo"
	var ae *apperror.Error
	if errors.As(cause, &ae) && ae.Kind != apperror.KindInternal {
		msg = ae.Message
	}
	log := logger.FromContext(ctx)
	log.Error().Err(cause).Str("export_id", e.ID.String()).Msg("xuất báo cáo chấm công thất bại")
	return u.exports.Fail(ctx, e.ID, msg)
}
