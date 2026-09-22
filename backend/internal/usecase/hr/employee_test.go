package hr

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
)

// Bộ kiểm thử hồ sơ nhân viên.
//
// Hai nhóm quan trọng nhất:
//
//  1. PHẠM VI DỮ LIỆU. Middleware chỉ biết "người này có quyền employee:read",
//     nó không biết người này định đọc hồ sơ của ai. Tầng usecase là hàng rào
//     duy nhất, và một lỗi ở đây là nhân viên thường đọc được hồ sơ cả công ty.
//
//  2. VÒNG LẶP CHUỖI CẤP TRÊN. Chuỗi manager_id cũng là một cây và cũng vòng
//     lặp được y như phòng ban, với hậu quả giống hệt.

// =========================================================================
// PHẠM VI DỮ LIỆU KHI LIỆT KÊ
// =========================================================================

// TestListScopeSelfSeesOnlyOwnRecord: người có phạm vi "self" phải bị ép lọc
// theo chính id của mình.
func TestListScopeSelfSeesOnlyOwnRecord(t *testing.T) {
	h := newHRHarness()
	me := h.employee("NV001", nil)

	if _, err := h.uc.ListEmployees(
		context.Background(), actorSelf(me.ID), domainhr.EmployeeFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	got := h.employees.lastFilter.ScopedEmployeeID
	if got == nil || *got != me.ID {
		t.Errorf("ScopedEmployeeID = %v, muốn %v", got, me.ID)
	}
}

// TestListScopeAllHasNoRestriction: người có phạm vi toàn công ty không bị
// thêm điều kiện nào.
func TestListScopeAllHasNoRestriction(t *testing.T) {
	h := newHRHarness()
	h.employee("NV001", nil)

	if _, err := h.uc.ListEmployees(
		context.Background(), actorAll(), domainhr.EmployeeFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.employees.lastFilter
	if f.ScopedEmployeeID != nil || f.ScopedDepartmentIDs != nil {
		t.Errorf("phạm vi ScopeAll vẫn bị thêm điều kiện: %+v", f)
	}
}

// TestListScopeDepartmentUsesManagedDepartments: trưởng phòng thấy cả nhánh
// dưới mình, không chỉ mỗi mình.
func TestListScopeDepartmentUsesManagedDepartments(t *testing.T) {
	h := newHRHarness()
	deptA, deptB := uuid.New(), uuid.New()

	actor := &domainauth.Actor{
		UserID:               uuid.New(),
		EmployeeID:           uuid.New(),
		Scope:                domainauth.ScopeDepartment,
		ManagedDepartmentIDs: []uuid.UUID{deptA, deptB},
	}

	if _, err := h.uc.ListEmployees(
		context.Background(), actor, domainhr.EmployeeFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := len(h.employees.lastFilter.ScopedDepartmentIDs); got != 2 {
		t.Errorf("số phòng ban trong phạm vi = %d, muốn 2", got)
	}
	if h.employees.lastFilter.ScopedEmployeeID != nil {
		t.Error("không được ép lọc theo id cá nhân khi đã có phòng ban quản lý")
	}
}

// TestListScopeDepartmentWithoutDepartmentsFallsBackToSelf.
//
// Trưởng phòng chưa được gán phòng nào phải rơi về "chỉ thấy mình". Để trống
// điều kiện là mở toàn bộ danh sách nhân viên — đúng cách một lỗi phân quyền
// nghiêm trọng thường phát sinh: không phải do viết sai luật, mà do quên mất
// trường hợp danh sách rỗng.
func TestListScopeDepartmentWithoutDepartmentsFallsBackToSelf(t *testing.T) {
	h := newHRHarness()
	actor := &domainauth.Actor{
		UserID:     uuid.New(),
		EmployeeID: uuid.New(),
		Scope:      domainauth.ScopeDepartment,
	}

	if _, err := h.uc.ListEmployees(
		context.Background(), actor, domainhr.EmployeeFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	got := h.employees.lastFilter.ScopedEmployeeID
	if got == nil || *got != actor.EmployeeID {
		t.Errorf("ScopedEmployeeID = %v, muốn %v", got, actor.EmployeeID)
	}
}

// TestListIgnoresScopeFromQueryString là phép thử chống giả mạo.
//
// Hai trường phạm vi nằm trong cùng struct với các trường lọc bình thường.
// Nếu tầng delivery lỡ bind chúng từ query string, người dùng chỉ cần thêm
// `?scoped_employee_id=<id người khác>` là đọc được hồ sơ bất kỳ. Usecase
// phải xoá sạch chúng trước khi áp phạm vi thật.
func TestListIgnoresScopeFromQueryString(t *testing.T) {
	h := newHRHarness()
	me := h.employee("NV001", nil)
	victim := uuid.New()

	forged := domainhr.EmployeeFilter{
		ScopedEmployeeID:    &victim,
		ScopedDepartmentIDs: []uuid.UUID{uuid.New()},
	}

	if _, err := h.uc.ListEmployees(
		context.Background(), actorSelf(me.ID), forged,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.employees.lastFilter
	if f.ScopedEmployeeID == nil || *f.ScopedEmployeeID != me.ID {
		t.Errorf("phạm vi giả mạo đã lọt qua: %v", f.ScopedEmployeeID)
	}
	if f.ScopedDepartmentIDs != nil {
		t.Error("danh sách phòng ban giả mạo đã lọt qua")
	}
}

// TestListNormalizesPageSize: page_size=1000000 là cách kéo sập database
// chỉ bằng một query string.
func TestListNormalizesPageSize(t *testing.T) {
	h := newHRHarness()

	cases := []struct {
		name     string
		page     int
		size     int
		wantPage int
		wantSize int
	}{
		{"mặc định", 0, 0, 1, defaultPageSize},
		{"âm", -5, -5, 1, defaultPageSize},
		{"vượt trần", 2, 1000000, 2, maxPageSize},
		{"hợp lệ", 3, 50, 3, 50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.uc.ListEmployees(
				context.Background(), actorAll(),
				domainhr.EmployeeFilter{Page: tc.page, PageSize: tc.size},
			); err != nil {
				t.Fatalf("lỗi không mong đợi: %v", err)
			}
			f := h.employees.lastFilter
			if f.Page != tc.wantPage || f.PageSize != tc.wantSize {
				t.Errorf("page/size = %d/%d, muốn %d/%d",
					f.Page, f.PageSize, tc.wantPage, tc.wantSize)
			}
		})
	}
}

// =========================================================================
// PHẠM VI DỮ LIỆU KHI ĐỌC MỘT HỒ SƠ
// =========================================================================

// TestGetEmployeeOutsideScopeReturns404, không phải 403.
//
// 403 xác nhận rằng bản ghi có tồn tại, và đó đã là rò rỉ. Kẻ tấn công quét
// id sẽ phân biệt được "id này có nhân viên" với "id này không có" chỉ bằng
// mã lỗi, dựng được danh sách nhân sự của cả công ty mà không đọc nổi một
// hồ sơ nào.
func TestGetEmployeeOutsideScopeReturns404(t *testing.T) {
	h := newHRHarness()
	other := h.employee("NV002", nil)

	_, err := h.uc.GetEmployee(
		context.Background(), actorSelf(uuid.New()), other.ID)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404 (403 sẽ xác nhận bản ghi tồn tại)", got)
	}
}

// TestGetEmployeeMissingAndForbiddenLookAlike: hồ sơ không tồn tại và hồ sơ
// ngoài phạm vi phải trả về cùng một mã lỗi, nếu không mã lỗi tự nó là kênh
// rò rỉ.
func TestGetEmployeeMissingAndForbiddenLookAlike(t *testing.T) {
	h := newHRHarness()
	other := h.employee("NV002", nil)
	actor := actorSelf(uuid.New())

	_, errMissing := h.uc.GetEmployee(context.Background(), actor, uuid.New())
	_, errForbidden := h.uc.GetEmployee(context.Background(), actor, other.ID)

	if statusOf(errMissing) != statusOf(errForbidden) {
		t.Errorf("không tồn tại = %d, ngoài phạm vi = %d — phải giống nhau",
			statusOf(errMissing), statusOf(errForbidden))
	}
}

func TestGetOwnRecordIsAllowed(t *testing.T) {
	h := newHRHarness()
	me := h.employee("NV001", nil)

	got, err := h.uc.GetEmployee(context.Background(), actorSelf(me.ID), me.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.ID != me.ID {
		t.Errorf("trả về hồ sơ %v, muốn %v", got.ID, me.ID)
	}
}

// =========================================================================
// KIỂM TRA ĐẦU VÀO
// =========================================================================

func TestCreateEmployeeRejectsBadInput(t *testing.T) {
	joined := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	earlier := joined.Add(-24 * time.Hour)

	cases := []struct {
		name   string
		mutate func(*EmployeeInput)
		want   int
	}{
		{"thiếu họ tên", func(in *EmployeeInput) { in.FullName = "   " }, http.StatusBadRequest},
		{"thiếu mã nhân viên", func(in *EmployeeInput) { in.EmployeeCode = "" }, http.StatusBadRequest},
		{"thiếu email", func(in *EmployeeInput) { in.Email = "" }, http.StatusBadRequest},
		{"hình thức làm việc lạ", func(in *EmployeeInput) { in.WorkMode = "sao-hoa" }, http.StatusBadRequest},
		{"trạng thái lạ", func(in *EmployeeInput) { in.Status = "nghi-choi" }, http.StatusBadRequest},
		{"thiếu ngày vào làm", func(in *EmployeeInput) { in.JoinedAt = time.Time{} }, http.StatusBadRequest},
		{"nghỉ trước khi vào", func(in *EmployeeInput) { in.ResignedAt = &earlier }, http.StatusBadRequest},
		{"hợp lệ", func(*EmployeeInput) {}, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHRHarness()
			in := validEmployeeInput()
			tc.mutate(&in)

			_, err := h.uc.CreateEmployee(context.Background(), actorAll(), in)
			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d (%v)", got, tc.want, err)
			}
		})
	}
}

// TestCreateEmployeeRejectsDuplicates: mã nhân viên và email trùng phải ra
// 409, không phải 400 — client phân biệt hai loại này để hiển thị khác nhau.
func TestCreateEmployeeRejectsDuplicates(t *testing.T) {
	t.Run("trùng mã", func(t *testing.T) {
		h := newHRHarness()
		h.employees.codes["NV999"] = true

		_, err := h.uc.CreateEmployee(
			context.Background(), actorAll(), validEmployeeInput())
		if got := statusOf(err); got != http.StatusConflict {
			t.Errorf("mã lỗi = %d, muốn 409", got)
		}
	})

	t.Run("trùng email", func(t *testing.T) {
		h := newHRHarness()
		h.employees.emails["nv999@abc.vn"] = true

		_, err := h.uc.CreateEmployee(
			context.Background(), actorAll(), validEmployeeInput())
		if got := statusOf(err); got != http.StatusConflict {
			t.Errorf("mã lỗi = %d, muốn 409", got)
		}
	})
}

// TestCreateEmployeeRejectsDanglingReferences: phòng ban, chức vụ và cấp trên
// phải có thật. Ràng buộc khoá ngoại cũng chặn, nhưng lỗi từ database là
// thông báo kỹ thuật mà người dùng không hiểu được.
func TestCreateEmployeeRejectsDanglingReferences(t *testing.T) {
	ghost := uuid.New()

	cases := []struct {
		name   string
		mutate func(*EmployeeInput)
	}{
		{"phòng ban không tồn tại", func(in *EmployeeInput) { in.DepartmentID = &ghost }},
		{"chức vụ không tồn tại", func(in *EmployeeInput) { in.PositionID = &ghost }},
		{"cấp trên không tồn tại", func(in *EmployeeInput) { in.ManagerID = &ghost }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHRHarness()
			in := validEmployeeInput()
			tc.mutate(&in)

			_, err := h.uc.CreateEmployee(context.Background(), actorAll(), in)
			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
		})
	}
}

func TestCreateEmployeeTrimsWhitespace(t *testing.T) {
	h := newHRHarness()
	in := validEmployeeInput()
	in.FullName = "  Nguyễn Văn A  "
	in.EmployeeCode = " NV999 "
	in.Email = " nv999@abc.vn "

	got, err := h.uc.CreateEmployee(context.Background(), actorAll(), in)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.FullName != "Nguyễn Văn A" || got.EmployeeCode != "NV999" ||
		got.Email != "nv999@abc.vn" {
		t.Errorf("khoảng trắng chưa được cắt: %q / %q / %q",
			got.FullName, got.EmployeeCode, got.Email)
	}
}

// =========================================================================
// CHUỖI CẤP TRÊN
// =========================================================================

func TestCannotBeOwnManager(t *testing.T) {
	h := newHRHarness()
	me := h.employee("NV001", nil)

	in := validEmployeeInput()
	in.ManagerID = &me.ID

	_, err := h.uc.UpdateEmployee(context.Background(), actorAll(), me.ID, in)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestCannotSetSubordinateAsManager là vòng lặp gián tiếp: A quản lý B,
// B quản lý C, rồi đặt C làm cấp trên của A.
//
// Chuỗi khi đó khép kín và mọi truy vấn WITH RECURSIVE trên nó chạy vô hạn.
func TestCannotSetSubordinateAsManager(t *testing.T) {
	h := newHRHarness()
	a := h.employee("NV001", nil)
	b := h.employee("NV002", nil)
	c := h.employee("NV003", nil)

	b.ManagerID = &a.ID
	c.ManagerID = &b.ID

	in := validEmployeeInput()
	in.ManagerID = &c.ID

	_, err := h.uc.UpdateEmployee(context.Background(), actorAll(), a.ID, in)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400 — C là cấp dưới gián tiếp của A", got)
	}
}

// TestCanSetUnrelatedManager: chặn vòng lặp không được chặn nhầm trường hợp
// hợp lệ. Một phép thử chỉ kiểm tra phía "từ chối" sẽ vẫn đạt với một hàm
// từ chối tất cả.
func TestCanSetUnrelatedManager(t *testing.T) {
	h := newHRHarness()
	a := h.employee("NV001", nil)
	boss := h.employee("NV002", nil)

	in := validEmployeeInput()
	in.ManagerID = &boss.ID

	got, err := h.uc.UpdateEmployee(context.Background(), actorAll(), a.ID, in)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.ManagerID == nil || *got.ManagerID != boss.ID {
		t.Errorf("cấp trên = %v, muốn %v", got.ManagerID, boss.ID)
	}
}

// TestUpdateChecksScopeOnOldRecord.
//
// Phạm vi phải kiểm trên bản ghi CŨ. Kiểm trên dữ liệu gửi lên thì người
// dùng chỉ cần gửi kèm department_id thuộc phòng mình là sửa được hồ sơ của
// bất kỳ ai — tự cấp cho mình quyền bằng chính dữ liệu muốn ghi.
func TestUpdateChecksScopeOnOldRecord(t *testing.T) {
	h := newHRHarness()
	myDept := uuid.New()
	victim := h.employee("NV002", nil) // không thuộc phòng nào của actor

	actor := &domainauth.Actor{
		UserID:               uuid.New(),
		EmployeeID:           uuid.New(),
		Scope:                domainauth.ScopeDepartment,
		ManagedDepartmentIDs: []uuid.UUID{myDept},
	}

	in := validEmployeeInput()
	in.DepartmentID = &myDept // cố kéo hồ sơ về phòng mình

	_, err := h.uc.UpdateEmployee(context.Background(), actor, victim.ID, in)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
	if len(h.employees.updated) != 0 {
		t.Error("đã ghi vào hồ sơ ngoài phạm vi")
	}
}

// =========================================================================
// VÔ HIỆU HOÁ
// =========================================================================

// TestDeactivateKillsSessions là phần bảo mật quan trọng nhất của thao tác này.
//
// Không cắt phiên thì người vừa bị cho nghỉ việc vẫn dùng được hệ thống cho
// tới khi access token hết hạn — tối đa 15 phút với đầy đủ quyền cũ, đúng
// khoảng thời gian một người vừa bị sa thải có động cơ mạnh nhất.
func TestDeactivateKillsSessions(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)
	user := h.users.add(&domainhr.User{EmployeeID: emp.ID, Email: emp.Email})

	if err := h.uc.DeactivateEmployee(
		context.Background(), actorAll(), emp.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.killedSessions) != 1 || h.killedSessions[0] != user.ID {
		t.Errorf("phiên bị cắt = %v, muốn [%v]", h.killedSessions, user.ID)
	}
	if len(h.employees.deleted) != 1 {
		t.Error("hồ sơ chưa được xoá mềm")
	}
}

// TestDeactivateWithoutAccountStillSoftDeletes: không phải ai cũng có tài
// khoản đăng nhập. Hồ sơ vẫn phải được xoá mềm.
func TestDeactivateWithoutAccountStillSoftDeletes(t *testing.T) {
	h := newHRHarness()
	emp := h.employee("NV002", nil)

	if err := h.uc.DeactivateEmployee(
		context.Background(), actorAll(), emp.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(h.employees.deleted) != 1 {
		t.Error("hồ sơ chưa được xoá mềm")
	}
	if len(h.killedSessions) != 0 {
		t.Error("không có tài khoản thì không có phiên nào để cắt")
	}
}

// TestCannotDeactivateSelf chặn việc tự khoá mình ra ngoài. Admin duy nhất
// tự vô hiệu hoá là cả hệ thống không còn ai quản trị được.
func TestCannotDeactivateSelf(t *testing.T) {
	h := newHRHarness()
	me := h.employee("NV001", nil)

	actor := actorAll()
	actor.EmployeeID = me.ID

	err := h.uc.DeactivateEmployee(context.Background(), actor, me.ID)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.employees.deleted) != 0 {
		t.Error("hồ sơ không được xoá")
	}
}

func TestDeactivateOutsideScopeReturns404(t *testing.T) {
	h := newHRHarness()
	other := h.employee("NV002", nil)

	err := h.uc.DeactivateEmployee(
		context.Background(), actorSelf(uuid.New()), other.ID)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
	if len(h.employees.deleted) != 0 {
		t.Error("hồ sơ ngoài phạm vi đã bị xoá")
	}
}
