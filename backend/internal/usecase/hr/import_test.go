package hr

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
)

// =========================================================================
// GIẢ LẬP
// =========================================================================

type fakeImports struct {
	byID map[uuid.UUID]*domainhr.EmployeeImport

	// appendErrAt làm AppendResult hỏng ở lần gọi thứ N (đếm từ 1) — giả
	// lập worker chết giữa chừng.
	appendErrAt int
	appends     int
}

func newFakeImports() *fakeImports {
	return &fakeImports{byID: map[uuid.UUID]*domainhr.EmployeeImport{}}
}

func (f *fakeImports) Create(_ context.Context, imp *domainhr.EmployeeImport) error {
	imp.ID = uuid.New()
	imp.CreatedAt = time.Now()
	imp.TotalRows = len(imp.Rows)
	cp := *imp
	f.byID[imp.ID] = &cp
	return nil
}

func (f *fakeImports) GetByID(_ context.Context, id uuid.UUID) (*domainhr.EmployeeImport, error) {
	imp := f.byID[id]
	if imp == nil {
		return nil, domainhr.ErrNotFound
	}
	cp := *imp
	cp.Results = append([]domainhr.ImportRowResult(nil), imp.Results...)
	cp.Succeeded, cp.Failed = countResults(cp.Results)
	return &cp, nil
}

func (f *fakeImports) List(
	_ context.Context, _ uuid.UUID, createdBy *uuid.UUID, _ int,
) ([]*domainhr.EmployeeImport, error) {
	var out []*domainhr.EmployeeImport
	for _, imp := range f.byID {
		if createdBy == nil || (imp.CreatedBy != nil && *imp.CreatedBy == *createdBy) {
			out = append(out, imp)
		}
	}
	return out, nil
}

func (f *fakeImports) MarkProcessing(_ context.Context, id uuid.UUID) error {
	f.byID[id].Status = domainhr.ImportProcessing
	return nil
}

func (f *fakeImports) AppendResult(_ context.Context, id uuid.UUID, r domainhr.ImportRowResult) error {
	f.appends++
	if f.appendErrAt > 0 && f.appends == f.appendErrAt {
		return errors.New("mất kết nối database")
	}
	f.byID[id].Results = append(f.byID[id].Results, r)
	return nil
}

func (f *fakeImports) Finish(_ context.Context, id uuid.UUID, st domainhr.ImportStatus, msg string) error {
	f.byID[id].Status = st
	f.byID[id].Error = msg
	return nil
}

type fakeImportJobs struct {
	published []uuid.UUID
	err       error
}

func (f *fakeImportJobs) PublishEmployeeImport(_ context.Context, id uuid.UUID, _ string) error {
	if f.err != nil {
		return f.err
	}
	f.published = append(f.published, id)
	return nil
}

type sentNotice struct {
	to                uuid.UUID
	title, body, link string
}

type importHarness struct {
	*hrHarness
	imports *fakeImports
	notices []sentNotice
	actor   *domainauth.Actor
}

func newImportHarness() *importHarness {
	h := &importHarness{hrHarness: newHRHarness(), imports: newFakeImports()}
	h.uc.SetImports(h.imports)
	h.uc.SetImportNotifier(func(_ context.Context, to uuid.UUID, title, body, link string) error {
		h.notices = append(h.notices, sentNotice{to, title, body, link})
		return nil
	})
	h.actor = actorAll("hr")
	return h
}

func csvFile(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n"))
}

// =========================================================================
// ĐỌC TỆP
// =========================================================================

func TestParseImport_TieuDeTiengVietCoDau_BOM_ChamPhay(t *testing.T) {
	// Đúng thứ Excel trên máy vùng Việt Nam lưu ra khi chọn "CSV UTF-8":
	// có BOM, dấu chấm phẩy, tiêu đề có dấu, thêm cột STT.
	data := append([]byte("\xef\xbb\xbf"), csvFile(
		"STT;Mã nhân viên;Họ và tên;Email;Ngày vào làm;SĐT",
		"1;NV001;Nguyễn Văn A;a@abc.vn;15/01/2024;0901",
	)...)

	rows, err := parseImportFile("ds.csv", data)
	if err != nil {
		t.Fatalf("không đọc được: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("muốn 1 dòng, được %d", len(rows))
	}
	v := rows[0].Values
	if v[colCode] != "NV001" || v[colName] != "Nguyễn Văn A" || v[colPhone] != "0901" {
		t.Errorf("sai giá trị: %+v", v)
	}
	if _, ok := v["stt"]; ok {
		t.Error("cột STT phải bị bỏ qua")
	}
	if rows[0].Line != 2 {
		t.Errorf("số dòng phải theo bảng tính (2), được %d", rows[0].Line)
	}
}

func TestParseImport_CotLa_BiTuChoi(t *testing.T) {
	_, err := parseImportFile("ds.csv", csvFile(
		"ma_nhan_vien,ho_ten,email,ngay_vao_lam,luong_co_ban",
		"NV1,A,a@abc.vn,2024-01-01,10000000",
	))
	if err == nil || !strings.Contains(err.Error(), "luong_co_ban") {
		t.Fatalf("cột lạ phải bị từ chối và nêu tên, được: %v", err)
	}
}

func TestParseImport_ThieuCotBatBuoc(t *testing.T) {
	_, err := parseImportFile("ds.csv", csvFile("ma_nhan_vien,ho_ten", "NV1,A"))
	if err == nil || !strings.Contains(err.Error(), "email") ||
		!strings.Contains(err.Error(), "ngay_vao_lam") {
		t.Fatalf("phải nêu đủ các cột thiếu, được: %v", err)
	}
}

func TestParseImport_CSVKhongPhaiUTF8_BiTuChoi(t *testing.T) {
	// "Nguyễn" trong bảng mã Windows-1258 — không phải UTF-8 hợp lệ.
	data := []byte("ma_nhan_vien,ho_ten,email,ngay_vao_lam\nNV1,Nguy\xd2n,a@abc.vn,2024-01-01")
	_, err := parseImportFile("ds.csv", data)
	if err == nil || !strings.Contains(err.Error(), "CSV UTF-8") {
		t.Fatalf("phải chỉ cách lưu lại thành CSV UTF-8, được: %v", err)
	}
}

func TestParseImport_QuaNhieuDong(t *testing.T) {
	lines := []string{"ma_nhan_vien,ho_ten,email,ngay_vao_lam"}
	for i := 0; i <= domainhr.MaxImportRows; i++ {
		lines = append(lines, "NV,A,a@abc.vn,2024-01-01")
	}
	if _, err := parseImportFile("ds.csv", csvFile(lines...)); err == nil {
		t.Fatal("quá giới hạn dòng phải bị từ chối")
	}
}

func TestParseImport_SaiDuoiTep(t *testing.T) {
	if _, err := parseImportFile("ds.xls", []byte("abc")); err == nil {
		t.Fatal(".xls (định dạng cũ) phải bị từ chối")
	}
}

func TestParseImport_XLSX_NgayDangSo(t *testing.T) {
	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	_ = f.SetSheetRow(sheet, "A1", &[]any{"Mã nhân viên", "Họ tên", "Email", "Ngày vào làm"})
	_ = f.SetSheetRow(sheet, "A2", &[]any{"NV001", "Trần Thị B", "b@abc.vn", time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC)})
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}

	rows, err := parseImportFile("ds.xlsx", buf.Bytes())
	if err != nil {
		t.Fatalf("không đọc được xlsx: %v", err)
	}
	d, err := parseImportDate(rows[0].Values[colJoined])
	if err != nil || d == nil || d.Format("2006-01-02") != "2024-03-05" {
		t.Fatalf("ô ngày của Excel phải đọc ra 2024-03-05, được %v (thô %q, lỗi %v)",
			d, rows[0].Values[colJoined], err)
	}
}

func TestParseImportDate(t *testing.T) {
	cases := map[string]string{
		"2024-01-15": "2024-01-15",
		"15/01/2024": "2024-01-15",
		"5/1/2024":   "2024-01-05", // ngày trước, tháng sau — không phải kiểu Mỹ
		"45306":      "2024-01-15", // số sê-ri Excel
	}
	for in, want := range cases {
		got, err := parseImportDate(in)
		if err != nil || got.Format("2006-01-02") != want {
			t.Errorf("%q: muốn %s, được %v (%v)", in, want, got, err)
		}
	}
	for _, bad := range []string{"15-Jan-2024", "0901234567", "31/02/2024"} {
		if _, err := parseImportDate(bad); err == nil {
			t.Errorf("%q phải bị từ chối", bad)
		}
	}
}

// =========================================================================
// XỬ LÝ
// =========================================================================

func TestImport_TaoNhanVien_TraPhongBanTheoTenKhongDau_CapTrenODongTren(t *testing.T) {
	h := newImportHarness()
	kt := h.departments.add(&domainhr.Department{CompanyID: h.company.ID, Code: "KT", Name: "Kế toán"})
	pos := h.positions.add(&domainhr.Position{CompanyID: h.company.ID, Code: "TP", Name: "Trưởng phòng"})

	imp, err := h.uc.StartImport(context.Background(), h.actor, "ds.csv", csvFile(
		"ma_nhan_vien,ho_ten,email,ngay_vao_lam,phong_ban,chuc_vu,ma_cap_tren,hinh_thuc_lam_viec,trang_thai,gioi_tinh",
		"NV001,Trưởng Phòng,tp@abc.vn,2024-01-01,ke toan,TP,,Tại văn phòng,Chính thức,Nữ",
		"NV002,Nhân Viên,nv@abc.vn,01/02/2024,KT,,nv001,remote,,nam",
	), false)
	if err != nil {
		t.Fatalf("StartImport: %v", err)
	}
	if imp.Status != domainhr.ImportDone || imp.Failed != 0 || imp.Succeeded != 2 {
		t.Fatalf("muốn xong 2/2, được %s %d/%d — %+v", imp.Status, imp.Succeeded, imp.Failed, imp.Results)
	}

	tp, _ := h.employees.FindByCode(context.Background(), h.company.ID, "NV001")
	nv, _ := h.employees.FindByCode(context.Background(), h.company.ID, "NV002")
	if tp.DepartmentID == nil || *tp.DepartmentID != kt.ID {
		t.Error("\"ke toan\" (không dấu) phải khớp phòng \"Kế toán\"")
	}
	if tp.PositionID == nil || *tp.PositionID != pos.ID {
		t.Error("chức vụ tra theo mã phải khớp")
	}
	if tp.Status != domainhr.StatusOfficial || tp.Gender != "nu" || tp.WorkMode != domainhr.WorkModeOnsite {
		t.Errorf("giá trị liệt kê tiếng Việt đọc sai: %s %s %s", tp.Status, tp.Gender, tp.WorkMode)
	}
	if nv.ManagerID == nil || *nv.ManagerID != tp.ID {
		t.Error("cấp trên ở dòng phía trên (mã viết thường) phải được tìm thấy")
	}
	if nv.Status != domainhr.StatusProbation {
		t.Error("trạng thái để trống phải mặc định là thử việc")
	}

	if len(h.notices) != 1 || h.notices[0].to != h.actor.EmployeeID ||
		h.notices[0].link != "/employees/imports/"+imp.ID.String() {
		t.Fatalf("người nhập phải nhận MỘT thông báo trỏ về trang kết quả, được %+v", h.notices)
	}
}

func TestImport_DongLoi_KhongChanDongKhac(t *testing.T) {
	h := newImportHarness()
	h.departments.add(&domainhr.Department{CompanyID: h.company.ID, Code: "A", Name: "Kinh doanh"})
	h.departments.add(&domainhr.Department{CompanyID: h.company.ID, Code: "B", Name: "Kinh doanh"})

	imp, err := h.uc.StartImport(context.Background(), h.actor, "ds.csv", csvFile(
		"ma_nhan_vien,ho_ten,email,ngay_vao_lam,phong_ban,ma_cap_tren",
		"NV1,A,a@abc.vn,2024-01-01,,",
		"NV1,Trùng Mã,khac@abc.vn,2024-01-01,,", // dòng 3: trùng mã dòng 2
		"NV3,C,c@abc.vn,32/13/2024,,",           // dòng 4: ngày sai
		"NV4,D,d@abc.vn,2024-01-01,Không Có,",   // dòng 5: phòng không có
		"NV5,E,e@abc.vn,2024-01-01,Kinh doanh,", // dòng 6: tên phòng trùng
		"NV6,F,f@abc.vn,2024-01-01,,NV99",       // dòng 7: cấp trên không có
		"NV7,G,g@abc.vn,2024-01-01,,",           // dòng 8: hợp lệ
	), false)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Succeeded != 2 || imp.Failed != 5 {
		t.Fatalf("muốn 2 thành công 5 lỗi, được %d/%d: %+v", imp.Succeeded, imp.Failed, imp.Results)
	}
	wantErr := map[int]string{3: "Mã nhân viên đã tồn tại", 4: "ngày vào làm", 5: "không có phòng ban",
		6: "dùng mã thay cho tên", 7: "cấp trên"}
	for _, r := range imp.Results {
		want, bad := wantErr[r.Line]
		if bad && !strings.Contains(r.Error, want) {
			t.Errorf("dòng %d: muốn lỗi chứa %q, được %q", r.Line, want, r.Error)
		}
		if !bad && r.Error != "" {
			t.Errorf("dòng %d không được lỗi, được %q", r.Line, r.Error)
		}
	}
	if !strings.Contains(h.notices[0].title, "5 lỗi") {
		t.Errorf("tiêu đề thông báo phải nói có lỗi: %q", h.notices[0].title)
	}
}

func TestImport_TaoTaiKhoan(t *testing.T) {
	h := newImportHarness()
	imp, err := h.uc.StartImport(context.Background(), h.actor, "ds.csv", csvFile(
		"ma_nhan_vien,ho_ten,email,ngay_vao_lam",
		"NV1,A,a@abc.vn,2024-01-01",
	), true)
	if err != nil {
		t.Fatal(err)
	}
	r := imp.Results[0]
	if !r.AccountCreated || r.EmployeeID == nil {
		t.Fatalf("phải tạo tài khoản: %+v", r)
	}
	if u := h.users.byEmployeeID[*r.EmployeeID]; u == nil || !u.MustChangePassword {
		t.Fatal("tài khoản phải tồn tại và bắt đổi mật khẩu lần đầu")
	}
}

func TestImport_ChayLai_LamTiepTuDongChuaXong_KhongTaoTrung(t *testing.T) {
	h := newImportHarness()
	jobs := &fakeImportJobs{}
	h.uc.SetImportJobs(jobs)

	imp, err := h.uc.StartImport(context.Background(), h.actor, "ds.csv", csvFile(
		"ma_nhan_vien,ho_ten,email,ngay_vao_lam",
		"NV1,A,a@abc.vn,2024-01-01",
		"NV2,B,b@abc.vn,2024-01-01",
		"NV3,C,c@abc.vn,2024-01-01",
	), false)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Status != domainhr.ImportQueued || len(jobs.published) != 1 {
		t.Fatalf("có hàng đợi thì chỉ xếp việc, chưa chạy: %s, %d job", imp.Status, len(jobs.published))
	}

	// Lần chạy đầu: tạo xong NV2 rồi chết ĐÚNG lúc ghi kết quả của nó.
	h.imports.appendErrAt = 2
	if err := h.uc.ProcessImport(context.Background(), imp.ID); err == nil {
		t.Fatal("lỗi ghi kết quả phải trả ra để message được giao lại")
	}

	// RabbitMQ giao lại message.
	h.imports.appendErrAt = 0
	if err := h.uc.ProcessImport(context.Background(), imp.ID); err != nil {
		t.Fatal(err)
	}

	got, _ := h.imports.GetByID(context.Background(), imp.ID)
	if got.Status != domainhr.ImportDone || len(got.Results) != 3 || got.Failed != 0 {
		t.Fatalf("muốn đủ 3 dòng, không lỗi — NV2 đã tạo ở lần trước phải được nhận ra, "+
			"không báo trùng mã: %+v", got.Results)
	}
	if n := len(h.employees.byID); n != 3 {
		t.Fatalf("muốn đúng 3 nhân viên, được %d", n)
	}

	// Chạy thêm lần nữa trên lượt đã xong: không làm gì, không báo lại.
	if err := h.uc.ProcessImport(context.Background(), imp.ID); err != nil {
		t.Fatal(err)
	}
	if len(h.notices) != 1 {
		t.Fatalf("chỉ báo MỘT lần, được %d", len(h.notices))
	}
}

func TestImport_KhongXepDuocViec_DongLuotNhap(t *testing.T) {
	h := newImportHarness()
	h.uc.SetImportJobs(&fakeImportJobs{err: errors.New("rabbitmq sập")})

	_, err := h.uc.StartImport(context.Background(), h.actor, "ds.csv", csvFile(
		"ma_nhan_vien,ho_ten,email,ngay_vao_lam", "NV1,A,a@abc.vn,2024-01-01",
	), false)
	if err == nil {
		t.Fatal("phải báo lỗi")
	}
	for _, imp := range h.imports.byID {
		if imp.Status != domainhr.ImportFailed {
			t.Fatalf("lượt nhập không được nằm mãi ở %s", imp.Status)
		}
	}
}

func TestImport_NguoiKhacKhongXemDuoc(t *testing.T) {
	h := newImportHarness()
	imp, err := h.uc.StartImport(context.Background(), h.actor, "ds.csv", csvFile(
		"ma_nhan_vien,ho_ten,email,ngay_vao_lam", "NV1,A,a@abc.vn,2024-01-01",
	), false)
	if err != nil {
		t.Fatal(err)
	}

	other := &domainauth.Actor{UserID: uuid.New(), EmployeeID: uuid.New(), Scope: domainauth.ScopeDepartment}
	if _, err := h.uc.GetImport(context.Background(), other, imp.ID); statusOf(err) != 404 {
		t.Fatalf("người khác (không có phạm vi toàn công ty) phải nhận 404, được %v", err)
	}
	if _, err := h.uc.GetImport(context.Background(), actorAll("admin"), imp.ID); err != nil {
		t.Fatalf("phạm vi toàn công ty phải xem được: %v", err)
	}
	list, _ := h.uc.ListImports(context.Background(), other)
	if len(list) != 0 {
		t.Fatal("danh sách của người khác không được có lượt nhập này")
	}
}
