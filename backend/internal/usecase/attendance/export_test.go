package attendance

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
)

type fakeExports struct {
	byID   map[uuid.UUID]*domainatt.Export
	files  map[uuid.UUID][]byte
	purged time.Time
}

func newFakeExports() *fakeExports {
	return &fakeExports{byID: map[uuid.UUID]*domainatt.Export{}, files: map[uuid.UUID][]byte{}}
}

func (f *fakeExports) Create(_ context.Context, e *domainatt.Export) error {
	e.ID = uuid.New()
	e.CreatedAt = time.Now()
	cp := *e
	f.byID[e.ID] = &cp
	return nil
}

func (f *fakeExports) GetByID(_ context.Context, id uuid.UUID) (*domainatt.Export, error) {
	e := f.byID[id]
	if e == nil {
		return nil, domainatt.ErrNotFound
	}
	cp := *e
	cp.HasFile = f.files[id] != nil
	return &cp, nil
}

func (f *fakeExports) GetFile(_ context.Context, id uuid.UUID) (string, []byte, error) {
	if f.files[id] == nil {
		return "", nil, domainatt.ErrNotFound
	}
	return f.byID[id].FileName, f.files[id], nil
}

func (f *fakeExports) List(_ context.Context, by *uuid.UUID, _ int) ([]*domainatt.Export, error) {
	var out []*domainatt.Export
	for _, e := range f.byID {
		if by == nil || (e.RequestedBy != nil && *e.RequestedBy == *by) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeExports) MarkProcessing(_ context.Context, id uuid.UUID) error {
	f.byID[id].Status = domainatt.ExportProcessing
	return nil
}

func (f *fakeExports) Complete(_ context.Context, id uuid.UUID, name string, data []byte, n int) error {
	e := f.byID[id]
	e.Status, e.FileName, e.EmployeeCount = domainatt.ExportDone, name, n
	f.files[id] = data
	return nil
}

func (f *fakeExports) Fail(_ context.Context, id uuid.UUID, msg string) error {
	f.byID[id].Status, f.byID[id].Error = domainatt.ExportFailed, msg
	return nil
}

func (f *fakeExports) PurgeFiles(_ context.Context, before time.Time) (int64, error) {
	f.purged = before
	return 0, nil
}

type exportHarness struct {
	*harness
	exports *fakeExports
	notices []string
}

func newExportHarness(now time.Time) *exportHarness {
	h := &exportHarness{harness: newHarness(now), exports: newFakeExports()}
	h.uc.SetExports(h.exports)
	h.uc.SetExportNotifier(func(_ context.Context, _ uuid.UUID, title, _, link string) error {
		h.notices = append(h.notices, title+" → "+link)
		return nil
	})
	return h
}

func hrActor() *domainauth.Actor {
	return &domainauth.Actor{
		UserID:      uuid.New(),
		EmployeeID:  uuid.New(),
		Scope:       domainauth.ScopeAll,
		Permissions: map[string]struct{}{domainauth.PermAttendanceReadAll: {}},
	}
}

func TestExport_DungTepHaiTrang_DungSoLieu(t *testing.T) {
	h := newExportHarness(time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC))
	an, binh := uuid.New(), uuid.New()
	h.days.summary = []*domainatt.MonthSummary{
		{EmployeeID: binh, EmployeeName: "Trần Bình", WorkdayCount: 22, PresentDays: 20, AbsentDays: 1,
			LeaveDays: 1, OnlineMinutes: 9630, LateDays: 2, LateMinutes: 25},
		{EmployeeID: an, EmployeeName: "Nguyễn An", WorkdayCount: 22, PresentDays: 22},
	}
	day := func(emp uuid.UUID, d int, st domainatt.DayStatus) *domainatt.Day {
		return &domainatt.Day{EmployeeID: emp, WorkDate: time.Date(2026, 8, d, 0, 0, 0, 0, time.UTC),
			Status: st, DepartmentName: "Kế toán"}
	}
	h.days.list = []*domainatt.Day{
		day(binh, 3, domainatt.DayPresent),
		day(binh, 4, domainatt.DayAbsent),
		day(binh, 5, domainatt.DayLeave),
		day(an, 3, domainatt.DayPresent),
	}

	e, err := h.uc.RequestExport(context.Background(), hrActor(), 2026, 8, nil)
	if err != nil {
		t.Fatalf("RequestExport: %v", err)
	}
	if e.Status != domainatt.ExportDone || !e.HasFile || e.EmployeeCount != 2 {
		t.Fatalf("muốn xong kèm tệp 2 người, được %+v", e)
	}
	if e.FileName != "cham-cong-2026-08.xlsx" {
		t.Errorf("tên tệp: %q", e.FileName)
	}

	f, err := excelize.OpenReader(bytes.NewReader(h.exports.files[e.ID]))
	if err != nil {
		t.Fatalf("tệp không mở được bằng Excel: %v", err)
	}
	if got := f.GetSheetList(); len(got) != 2 || got[0] != "Tổng hợp" || got[1] != "Chi tiết" {
		t.Fatalf("trang tính: %v", got)
	}

	get := func(sheet, c string) string {
		v, _ := f.GetCellValue(sheet, c)
		return v
	}
	// Sắp theo tên trong cùng phòng: An trước Bình.
	if get("Tổng hợp", "A4") != "Nguyễn An" || get("Tổng hợp", "A5") != "Trần Bình" {
		t.Errorf("thứ tự dòng sai: %q, %q", get("Tổng hợp", "A4"), get("Tổng hợp", "A5"))
	}
	if get("Tổng hợp", "B5") != "Kế toán" || get("Tổng hợp", "E5") != "1" || get("Tổng hợp", "G5") != "2" {
		t.Errorf("số liệu Bình sai: phòng %q vắng %q muộn %q",
			get("Tổng hợp", "B5"), get("Tổng hợp", "E5"), get("Tổng hợp", "G5"))
	}
	// 9630 phút = 160.5 giờ.
	if get("Tổng hợp", "J5") != "160.5" {
		t.Errorf("giờ online: %q", get("Tổng hợp", "J5"))
	}

	// Chi tiết: cột C là ngày 1, nên ngày 3/4/5 là E/F/G. Dòng 6 là Bình.
	if get("Chi tiết", "E6") != "X" || get("Chi tiết", "F6") != "V" || get("Chi tiết", "G6") != "P" {
		t.Errorf("ký hiệu ngày sai: %q %q %q", get("Chi tiết", "E6"), get("Chi tiết", "F6"), get("Chi tiết", "G6"))
	}
	if get("Chi tiết", "C4") != "T7" { // 01/08/2026 là thứ Bảy
		t.Errorf("thứ của ngày 1: %q", get("Chi tiết", "C4"))
	}
	// Tháng 8 có 31 ngày: cột cuối là AG (2 + 31 = 33).
	if get("Chi tiết", "AG3") != "31" || get("Chi tiết", "AH3") != "" {
		t.Errorf("số cột ngày sai")
	}

	if len(h.notices) != 1 || h.notices[0] != "Báo cáo chấm công tháng 8/2026 đã sẵn sàng → /attendance/exports" {
		t.Errorf("thông báo: %v", h.notices)
	}
	if h.exports.purged.IsZero() {
		t.Error("xuất xong phải dọn tệp cũ")
	}
}

func TestExport_TruongPhong_ChiCoNguoiTrongPhamVi(t *testing.T) {
	h := newExportHarness(time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC))
	member := uuid.New()
	h.employees.activeIDs = []uuid.UUID{member}

	lead := &domainauth.Actor{
		UserID:               uuid.New(),
		EmployeeID:           uuid.New(),
		Scope:                domainauth.ScopeDepartment,
		ManagedDepartmentIDs: []uuid.UUID{uuid.New()},
		Permissions:          map[string]struct{}{domainauth.PermAttendanceReadAll: {}},
	}
	if _, err := h.uc.RequestExport(context.Background(), lead, 2026, 9, nil); err != nil {
		t.Fatal(err)
	}

	// Worker dựng lại actor từ ảnh chụp — bộ lọc gửi xuống database phải
	// mang đúng phạm vi của trưởng phòng, không phải toàn công ty.
	f := h.days.lastFilter
	if !f.RestrictScope || len(f.ScopedEmployeeIDs) != 2 ||
		f.ScopedEmployeeIDs[0] != member || f.ScopedEmployeeIDs[1] != lead.EmployeeID {
		t.Fatalf("phạm vi không được áp ở worker: %+v", f)
	}
}

func TestExport_ThangChuaToi_BiTuChoi(t *testing.T) {
	h := newExportHarness(time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC))
	if _, err := h.uc.RequestExport(context.Background(), hrActor(), 2026, 10, nil); statusOf(err) != 400 {
		t.Fatalf("tháng chưa tới phải bị từ chối, được %v", err)
	}
	if _, err := h.uc.RequestExport(context.Background(), hrActor(), 2026, 13, nil); statusOf(err) != 400 {
		t.Fatalf("tháng 13 phải bị từ chối, được %v", err)
	}
}

func TestExport_NguoiKhacKhongTaiDuoc_TepQuaHanBaoRo(t *testing.T) {
	h := newExportHarness(time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC))
	owner := hrActor()
	owner.Scope = domainauth.ScopeDepartment
	e, err := h.uc.RequestExport(context.Background(), owner, 2026, 9, nil)
	if err != nil {
		t.Fatal(err)
	}

	other := hrActor()
	other.Scope = domainauth.ScopeDepartment
	if _, _, err := h.uc.ExportFile(context.Background(), other, e.ID); statusOf(err) != 404 {
		t.Fatalf("người khác phải nhận 404, được %v", err)
	}
	if _, data, err := h.uc.ExportFile(context.Background(), owner, e.ID); err != nil || len(data) == 0 {
		t.Fatalf("chủ lượt xuất phải tải được: %v", err)
	}

	delete(h.exports.files, e.ID) // tệp bị dọn sau 7 ngày
	_, _, err = h.uc.ExportFile(context.Background(), owner, e.ID)
	if statusOf(err) != 404 || !contains(err.Error(), "7 ngày") {
		t.Fatalf("tệp đã dọn phải nói rõ lý do, được %v", err)
	}
}

func TestExport_KhongXepDuocViec_DongLuotXuat(t *testing.T) {
	h := newExportHarness(time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC))
	h.uc.SetExportJobs(failingExportJobs{})
	if _, err := h.uc.RequestExport(context.Background(), hrActor(), 2026, 9, nil); err == nil {
		t.Fatal("phải báo lỗi")
	}
	for _, e := range h.exports.byID {
		if e.Status != domainatt.ExportFailed {
			t.Fatalf("lượt xuất không được nằm mãi ở %s", e.Status)
		}
	}
}

type failingExportJobs struct{}

func (failingExportJobs) PublishAttendanceExport(context.Context, uuid.UUID) error {
	return errors.New("rabbitmq sập")
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }
