package hr

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// ImportJobPublisher đẩy một lượt nhập sang worker.
type ImportJobPublisher interface {
	PublishEmployeeImport(ctx context.Context, importID uuid.UUID, requestID string) error
}

// ImportNotifier báo cho người bấm nhập biết lượt nhập đã xong.
//
// Là một hàm chứ không phải import thẳng usecase/notification: hr không
// được phụ thuộc module khác. Composition root của worker nối dây.
type ImportNotifier func(ctx context.Context, recipientEmployeeID uuid.UUID, title, body, link string) error

// SetImports bật chức năng nhập hàng loạt. Thiếu repo thì StartImport báo
// lỗi rõ ràng thay vì panic.
func (u *Usecase) SetImports(repo domainhr.EmployeeImportRepository) { u.imports = repo }

// SetImportJobs chọn chạy nền. Để nil thì StartImport xử lý ngay trong
// request — chỉ dùng cho test.
func (u *Usecase) SetImportJobs(p ImportJobPublisher) { u.importJobs = p }

func (u *Usecase) SetImportNotifier(fn ImportNotifier) { u.importNotifier = fn }

// importRequestIDKey mang request_id từ handler xuống để lần vết một lượt
// nhập từ HTTP sang worker — cùng cách module lương làm với auditMeta.
type importRequestIDKey struct{}

// WithImportRequestID gắn request_id vào context. Gọi ở handler.
func WithImportRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, importRequestIDKey{}, id)
}

// StartImport đọc tệp, lưu thành một lượt nhập rồi đẩy sang worker.
//
// Mọi lỗi CẢ TỆP (sai định dạng, thiếu cột) trả về ngay ở đây — người dùng
// đang nhìn màn hình tải lên. Lỗi từng dòng thì xem ở trang kết quả.
func (u *Usecase) StartImport(
	ctx context.Context,
	actor *domainauth.Actor,
	fileName string,
	data []byte,
	createAccounts bool,
) (*domainhr.EmployeeImport, error) {
	if u.imports == nil {
		return nil, apperror.New(apperror.KindUnprocessable, "Chức năng nhập hàng loạt chưa được bật")
	}
	companyID, err := u.companyID(ctx)
	if err != nil {
		return nil, err
	}

	rows, err := parseImportFile(fileName, data)
	if err != nil {
		return nil, err
	}

	creator := actor.EmployeeID
	imp := &domainhr.EmployeeImport{
		CompanyID:      companyID,
		FileName:       truncate(fileName, 255),
		Status:         domainhr.ImportQueued,
		CreateAccounts: createAccounts,
		Rows:           rows,
		CreatedBy:      &creator,
		Actor: domainhr.ImportActor{
			UserID:               actor.UserID,
			EmployeeID:           actor.EmployeeID,
			Scope:                string(actor.Scope),
			ManagedDepartmentIDs: actor.ManagedDepartmentIDs,
		},
	}
	if err := u.imports.Create(ctx, imp); err != nil {
		return nil, apperror.Internal(err)
	}

	if u.importJobs == nil {
		if err := u.ProcessImport(ctx, imp.ID); err != nil {
			return nil, err
		}
	} else {
		reqID, _ := ctx.Value(importRequestIDKey{}).(string)
		if err := u.importJobs.PublishEmployeeImport(ctx, imp.ID, reqID); err != nil {
			// Không đẩy được thì đóng lượt nhập lại ngay. Để nó nằm ở
			// 'queued' mãi mãi thì trang kết quả quay vòng không bao giờ dừng.
			_ = u.imports.Finish(ctx, imp.ID, domainhr.ImportFailed, "Không gửi được sang hàng đợi xử lý")
			return nil, apperror.Internal(err)
		}
	}
	return u.imports.GetByID(ctx, imp.ID)
}

// GetImport trả về một lượt nhập kèm kết quả từng dòng.
//
// Người có phạm vi toàn công ty xem được mọi lượt nhập; người khác chỉ xem
// lượt của chính mình. Không thấy thì 404, như mọi chỗ khác.
func (u *Usecase) GetImport(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) (*domainhr.EmployeeImport, error) {
	if u.imports == nil {
		return nil, apperror.NotFound("lượt nhập")
	}
	imp, err := u.imports.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("lượt nhập")
	}
	if actor.Scope != domainauth.ScopeAll &&
		(imp.CreatedBy == nil || *imp.CreatedBy != actor.EmployeeID) {
		return nil, apperror.NotFound("lượt nhập")
	}
	return imp, nil
}

// ListImports trả về 20 lượt nhập gần nhất mà actor được xem.
func (u *Usecase) ListImports(
	ctx context.Context,
	actor *domainauth.Actor,
) ([]*domainhr.EmployeeImport, error) {
	if u.imports == nil {
		return nil, nil
	}
	companyID, err := u.companyID(ctx)
	if err != nil {
		return nil, err
	}
	var createdBy *uuid.UUID
	if actor.Scope != domainauth.ScopeAll {
		id := actor.EmployeeID
		createdBy = &id
	}
	items, err := u.imports.List(ctx, companyID, createdBy, 20)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return items, nil
}

// ProcessImport chạy một lượt nhập. Gọi từ worker.
//
// PHẢI chịu được chạy lại: RabbitMQ giao lại message khi worker chết giữa
// chừng. Kết quả được ghi SAU MỖI DÒNG, và lần chạy lại bắt đầu từ dòng đầu
// tiên chưa có kết quả — không tạo trùng những người đã tạo.
func (u *Usecase) ProcessImport(ctx context.Context, id uuid.UUID) error {
	log := logger.FromContext(ctx)

	imp, err := u.imports.GetByID(ctx, id)
	if errors.Is(err, domainhr.ErrNotFound) {
		// Bản ghi không còn: không có gì để làm, và chạy lại cũng không
		// làm nó xuất hiện. Trả nil để message không vào dead-letter.
		log.Warn().Str("import_id", id.String()).Msg("không thấy lượt nhập, bỏ qua")
		return nil
	}
	if err != nil {
		return err
	}
	if imp.Status.Finished() {
		return nil
	}
	if err := u.imports.MarkProcessing(ctx, id); err != nil {
		return err
	}

	actor := &domainauth.Actor{
		UserID:               imp.Actor.UserID,
		EmployeeID:           imp.Actor.EmployeeID,
		Scope:                domainauth.Scope(imp.Actor.Scope),
		ManagedDepartmentIDs: imp.Actor.ManagedDepartmentIDs,
	}
	lk, err := u.loadImportLookups(ctx, imp.CompanyID)
	if err != nil {
		return err
	}

	for i := len(imp.Results); i < len(imp.Rows); i++ {
		res := u.importRow(ctx, actor, imp, imp.Rows[i], lk)
		if err := u.imports.AppendResult(ctx, id, res); err != nil {
			return err
		}
		imp.Results = append(imp.Results, res)
	}

	if err := u.imports.Finish(ctx, id, domainhr.ImportDone, ""); err != nil {
		return err
	}

	ok, failed := countResults(imp.Results)
	log.Info().
		Str("import_id", id.String()).
		Int("created", ok).
		Int("failed", failed).
		Msg("đã nhập xong nhân viên")

	u.notifyImportDone(ctx, imp, ok, failed)
	return nil
}

func (u *Usecase) notifyImportDone(ctx context.Context, imp *domainhr.EmployeeImport, ok, failed int) {
	if u.importNotifier == nil || imp.CreatedBy == nil {
		return
	}
	title := fmt.Sprintf("Đã nhập xong %d nhân viên", ok)
	body := fmt.Sprintf("Tệp %s: %d dòng thành công", imp.FileName, ok)
	if failed > 0 {
		title = fmt.Sprintf("Nhập nhân viên: %d thành công, %d lỗi", ok, failed)
		body = fmt.Sprintf("Tệp %s có %d dòng không nhập được — mở để xem lý do từng dòng", imp.FileName, failed)
	}
	link := "/employees/imports/" + imp.ID.String()
	if err := u.importNotifier(ctx, *imp.CreatedBy, title, body, link); err != nil {
		// Không trả lỗi: nhân viên đã tạo xong, chạy lại job vì một thông
		// báo không gửi được là không đáng. Kết quả vẫn xem được ở trang
		// danh sách lượt nhập.
		log := logger.FromContext(ctx)
		log.Error().Err(err).Str("import_id", imp.ID.String()).Msg("không gửi được thông báo nhập xong")
	}
}

func countResults(results []domainhr.ImportRowResult) (ok, failed int) {
	for _, r := range results {
		if r.Error == "" {
			ok++
		} else {
			failed++
		}
	}
	return ok, failed
}

// importLookups tra phòng ban và chức vụ theo mã hoặc tên. Nạp một lần cho
// cả lượt nhập thay vì mỗi dòng một truy vấn.
type importLookups struct {
	departments map[string][]uuid.UUID
	positions   map[string][]uuid.UUID
}

func (u *Usecase) loadImportLookups(ctx context.Context, companyID uuid.UUID) (*importLookups, error) {
	lk := &importLookups{
		departments: map[string][]uuid.UUID{},
		positions:   map[string][]uuid.UUID{},
	}
	depts, err := u.departments.List(ctx, companyID)
	if err != nil {
		return nil, err
	}
	for _, d := range depts {
		addLookup(lk.departments, d.ID, d.Code, d.Name)
	}
	positions, err := u.positions.List(ctx, companyID)
	if err != nil {
		return nil, err
	}
	for _, p := range positions {
		addLookup(lk.positions, p.ID, p.Code, p.Name)
	}
	return lk, nil
}

// addLookup ghi một bản ghi dưới cả mã lẫn tên (đã bỏ dấu, viết thường).
// Người nhập gõ "Kế toán", "ke toan" hay "KT" đều ra cùng một phòng.
func addLookup(m map[string][]uuid.UUID, id uuid.UUID, keys ...string) {
	seen := map[string]bool{}
	for _, k := range keys {
		k = foldVietnamese(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		m[k] = append(m[k], id)
	}
}

// resolveLookup trả về id duy nhất khớp với giá trị. Hai phòng trùng tên
// thì báo lỗi chứ không chọn đại một phòng — xếp nhầm phòng là lỗi rất khó
// phát hiện về sau.
func resolveLookup(m map[string][]uuid.UUID, raw, what string) (*uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	ids := m[foldVietnamese(raw)]
	switch len(ids) {
	case 0:
		return nil, fmt.Errorf("không có %s %q", what, raw)
	case 1:
		return &ids[0], nil
	default:
		return nil, fmt.Errorf("có nhiều %s tên %q — dùng mã thay cho tên", what, raw)
	}
}

func (u *Usecase) importRow(
	ctx context.Context,
	actor *domainauth.Actor,
	imp *domainhr.EmployeeImport,
	row domainhr.ImportRow,
	lk *importLookups,
) domainhr.ImportRowResult {
	v := row.Values
	res := domainhr.ImportRowResult{
		Line:         row.Line,
		EmployeeCode: v[colCode],
		FullName:     v[colName],
	}

	in, err := u.buildImportInput(ctx, imp.CompanyID, v, lk)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	emp, err := u.CreateEmployee(ctx, actor, in)
	if err != nil {
		// Trùng mã NGAY SAU khi chính lượt nhập này đã tạo: worker chết
		// giữa lúc tạo xong và lúc ghi kết quả, rồi message được giao lại.
		// Nhận ra hồ sơ của mình (cùng mã, cùng email, tạo sau lúc bấm nhập)
		// thì coi là thành công thay vì báo trùng một người vừa tạo.
		if existing := u.ownImportedEmployee(ctx, imp, in); existing != nil {
			emp = existing
		} else {
			res.Error = userMessage(err)
			return res
		}
	}
	res.EmployeeID = &emp.ID

	if imp.CreateAccounts {
		if _, err := u.CreateAccount(ctx, actor, emp.ID); err != nil {
			res.Warning = "Đã tạo hồ sơ nhưng chưa tạo được tài khoản: " + userMessage(err)
		} else {
			res.AccountCreated = true
		}
	}
	return res
}

func (u *Usecase) ownImportedEmployee(
	ctx context.Context,
	imp *domainhr.EmployeeImport,
	in EmployeeInput,
) *domainhr.Employee {
	e, err := u.employees.FindByCode(ctx, imp.CompanyID, in.EmployeeCode)
	if err != nil || e == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(e.Email), strings.TrimSpace(in.Email)) ||
		e.CreatedAt.Before(imp.CreatedAt) {
		return nil
	}
	return e
}

// buildImportInput đổi một dòng chuỗi thô thành EmployeeInput.
func (u *Usecase) buildImportInput(
	ctx context.Context,
	companyID uuid.UUID,
	v map[string]string,
	lk *importLookups,
) (EmployeeInput, error) {
	in := EmployeeInput{
		EmployeeCode: v[colCode],
		FullName:     v[colName],
		Email:        v[colEmail],
		Phone:        v[colPhone],
		Address:      v[colAddress],
		WorkMode:     domainhr.WorkModeOnsite,
		Status:       domainhr.StatusProbation,
	}

	joined, err := parseImportDate(v[colJoined])
	if err != nil {
		return in, fmt.Errorf("ngày vào làm: %w", err)
	}
	if joined == nil {
		return in, errors.New("thiếu ngày vào làm")
	}
	in.JoinedAt = *joined
	if in.DateOfBirth, err = parseImportDate(v[colBirth]); err != nil {
		return in, fmt.Errorf("ngày sinh: %w", err)
	}

	if raw := v[colGender]; raw != "" {
		g, ok := importGenders[foldVietnamese(raw)]
		if !ok {
			return in, fmt.Errorf("giới tính %q không hợp lệ (nhận: %s)", raw, choices(importGenders))
		}
		in.Gender = g
	}
	if raw := v[colWorkMode]; raw != "" {
		w, ok := importWorkModes[foldVietnamese(raw)]
		if !ok {
			return in, fmt.Errorf("hình thức làm việc %q không hợp lệ (nhận: %s)", raw, choices(importWorkModes))
		}
		in.WorkMode = w
	}
	if raw := v[colStatus]; raw != "" {
		s, ok := importStatuses[foldVietnamese(raw)]
		if !ok {
			return in, fmt.Errorf("trạng thái %q không hợp lệ (nhận: %s)", raw, choices(importStatuses))
		}
		in.Status = s
	}

	if in.DepartmentID, err = resolveLookup(lk.departments, v[colDepartment], "phòng ban"); err != nil {
		return in, err
	}
	if in.PositionID, err = resolveLookup(lk.positions, v[colPosition], "chức vụ"); err != nil {
		return in, err
	}

	// Cấp trên tra theo mã nhân viên, kể cả người vừa được tạo ở một dòng
	// PHÍA TRÊN trong cùng tệp — nên trưởng phòng đặt trước nhân viên.
	if raw := v[colManager]; raw != "" {
		m, err := u.employees.FindByCode(ctx, companyID, raw)
		if err != nil || m == nil {
			return in, fmt.Errorf("không có nhân viên mã %q để làm cấp trên (cấp trên phải có sẵn hoặc nằm ở dòng phía trên)", raw)
		}
		in.ManagerID = &m.ID
	}
	return in, nil
}

// userMessage lấy câu báo lỗi dành cho người dùng. Lỗi nội bộ không lộ
// chi tiết ra ngoài — như ở tầng HTTP.
func userMessage(err error) string {
	var ae *apperror.Error
	if errors.As(err, &ae) && ae.Kind != apperror.KindInternal {
		return ae.Message
	}
	return "lỗi hệ thống, thử lại sau"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
