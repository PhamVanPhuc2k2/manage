//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
)

// Integration test cho phần nhân sự.
//
// Chỉ kiểm những thứ mà bản giả lập KHÔNG kiểm được: truy vấn đệ quy trên
// cây phòng ban, chỉ mục unique một phần, hành vi của xoá mềm, và tìm kiếm
// tiếng Việt không dấu. Phần luật nghiệp vụ đã có bộ kiểm thử riêng ở tầng
// usecase và không lặp lại ở đây.

// newCompany chèn thẳng một công ty.
//
// Đi vòng qua repository ở đây không thêm giá trị: công ty chỉ là điểm neo
// khoá ngoại cho mọi thứ còn lại, không phải thứ đang được kiểm.
func newCompany(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()

	id := uuid.New()
	_, err := testPool.Exec(ctx,
		`INSERT INTO companies (id, name, timezone) VALUES ($1, $2, $3)`,
		id, "Công ty kiểm thử", "Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatalf("tạo công ty: %v", err)
	}
	return id
}

// =========================================================================
// CÂY PHÒNG BAN — TRUY VẤN ĐỆ QUY
// =========================================================================

// chain dựng chuỗi phòng ban cha–con và trả về id theo thứ tự.
func chain(t *testing.T, repo *DepartmentRepository, companyID uuid.UUID, names ...string) []uuid.UUID {
	t.Helper()
	ctx := context.Background()

	var out []uuid.UUID
	var parent *uuid.UUID

	for _, name := range names {
		d := &domainhr.Department{
			CompanyID: companyID,
			Code:      name,
			Name:      name,
			ParentID:  parent,
		}
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("tạo phòng ban %s: %v", name, err)
		}
		out = append(out, d.ID)
		id := d.ID
		parent = &id
	}
	return out
}

// TestListSubtreeIDsWalksWholeBranch là phép thử quan trọng nhất của gói này.
//
// ListSubtreeIDs quyết định trưởng phòng thấy được dữ liệu của những ai. Nó
// là một truy vấn WITH RECURSIVE — thứ không có cách nào kiểm bằng bản giả
// lập, vì bản giả lập sẽ tự cài lại thuật toán và kiểm chính nó.
func TestListSubtreeIDsWalksWholeBranch(t *testing.T) {
	db := testDB(t)
	repo := NewDepartmentRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	ids := chain(t, repo, companyID, "KHOI", "PHONG", "TO", "NHOM")

	got, err := repo.ListSubtreeIDs(ctx, ids[0])
	if err != nil {
		t.Fatalf("ListSubtreeIDs: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("số phòng trong nhánh = %d, muốn 4 (gồm cả gốc): %v", len(got), got)
	}

	// Từ giữa chuỗi thì chỉ thấy phần từ đó trở xuống.
	got, err = repo.ListSubtreeIDs(ctx, ids[2])
	if err != nil {
		t.Fatalf("ListSubtreeIDs: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("nhánh từ cấp ba = %d phòng, muốn 2: %v", len(got), got)
	}
}

// TestListSubtreeIDsIgnoresSiblings: nhánh bên cạnh không được lọt vào, nếu
// không trưởng phòng đọc được dữ liệu của phòng không thuộc quyền mình.
func TestListSubtreeIDsIgnoresSiblings(t *testing.T) {
	db := testDB(t)
	repo := NewDepartmentRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	left := chain(t, repo, companyID, "TRAI", "TRAI-CON")
	chain(t, repo, companyID, "PHAI", "PHAI-CON")

	got, err := repo.ListSubtreeIDs(ctx, left[0])
	if err != nil {
		t.Fatalf("ListSubtreeIDs: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("nhánh trái = %d phòng, muốn 2 — nhánh phải đã lọt vào: %v",
			len(got), got)
	}
}

// TestListAncestorIDsIncludesSelf khoá lại hợp đồng của hàm này.
//
// Nó trả về CHÍNH NÚT ĐÓ kèm toàn bộ tổ tiên, vì truy vấn đệ quy lấy hàng
// neo `WHERE id = $1` rồi mới đi ngược lên. ListSubtreeIDs cũng gồm chính
// gốc, nên hai hàm đối xứng.
//
// Phép thử này tồn tại vì sự lệch đó từng có thật: bản giả lập ở tầng
// usecase trả tổ tiên chặt, không gồm chính nó. Luật nghiệp vụ không đổi
// theo, nhưng một bản giả lập nói sai về hợp đồng là quả mìn chờ người sau.
func TestListAncestorIDsIncludesSelf(t *testing.T) {
	db := testDB(t)
	repo := NewDepartmentRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	ids := chain(t, repo, companyID, "KHOI", "PHONG", "TO")

	got, err := repo.ListAncestorIDs(ctx, ids[2])
	if err != nil {
		t.Fatalf("ListAncestorIDs: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("số nút trả về = %d, muốn 3 (chính nó + hai tổ tiên): %v",
			len(got), got)
	}

	seen := map[uuid.UUID]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for i, want := range ids {
		if !seen[want] {
			t.Errorf("thiếu nút cấp %d (%v) trong kết quả %v", i+1, want, got)
		}
	}
}

// TestListAncestorIDsOfRootIsSelfOnly: phòng gốc không có tổ tiên nào, nên
// kết quả chỉ có chính nó.
func TestListAncestorIDsOfRootIsSelfOnly(t *testing.T) {
	db := testDB(t)
	repo := NewDepartmentRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	ids := chain(t, repo, companyID, "KHOI")

	got, err := repo.ListAncestorIDs(ctx, ids[0])
	if err != nil {
		t.Fatalf("ListAncestorIDs: %v", err)
	}
	if len(got) != 1 || got[0] != ids[0] {
		t.Errorf("kết quả = %v, muốn chỉ [%v]", got, ids[0])
	}
}

// =========================================================================
// XOÁ MỀM VÀ CHỈ MỤC UNIQUE MỘT PHẦN
// =========================================================================

func newEmployee(t *testing.T, repo *EmployeeRepository, companyID uuid.UUID,
	code, email string) *domainhr.Employee {
	t.Helper()
	ctx := context.Background()

	e := &domainhr.Employee{
		CompanyID:    companyID,
		EmployeeCode: code,
		FullName:     "Nguyễn Văn " + code,
		Email:        email,
		WorkMode:     domainhr.WorkModeOnsite,
		Status:       domainhr.StatusOfficial,
		JoinedAt:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("tạo nhân viên %s: %v", code, err)
	}
	return e
}

// TestEmailFreedAfterSoftDelete khoá lại lý do tồn tại của chỉ mục unique
// MỘT PHẦN trên email.
//
// Nhân viên nghỉ rồi quay lại phải dùng được email cũ. Nếu chỉ mục là unique
// toàn phần, hàng đã xoá mềm giữ chỗ email đó vĩnh viễn, và lỗi hiện ra với
// người dùng là 500 chứ không nói rõ vì sao.
func TestEmailFreedAfterSoftDelete(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	first := newEmployee(t, repo, companyID, "NV001", "a@abc.vn")

	// Còn sống thì email bị coi là đã dùng.
	used, err := repo.ExistsEmail(ctx, "a@abc.vn", nil)
	if err != nil {
		t.Fatalf("ExistsEmail: %v", err)
	}
	if !used {
		t.Fatal("email đang dùng mà báo chưa dùng")
	}

	if err := repo.SoftDelete(ctx, first.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	used, err = repo.ExistsEmail(ctx, "a@abc.vn", nil)
	if err != nil {
		t.Fatalf("ExistsEmail: %v", err)
	}
	if used {
		t.Error("email vẫn bị coi là đã dùng sau khi xoá mềm")
	}

	// Và tạo lại được thật, không chỉ là ExistsEmail nói vậy.
	newEmployee(t, repo, companyID, "NV002", "a@abc.vn")
}

// TestExistsEmailIsCaseInsensitive: email không phân biệt hoa thường, nên
// "A@ABC.VN" và "a@abc.vn" là một người.
func TestExistsEmailIsCaseInsensitive(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	newEmployee(t, repo, companyID, "NV001", "a@abc.vn")

	used, err := repo.ExistsEmail(ctx, "A@ABC.VN", nil)
	if err != nil {
		t.Fatalf("ExistsEmail: %v", err)
	}
	if !used {
		t.Error("email viết hoa không khớp với email đã có")
	}
}

// TestExistsCodeExcludesSelf: sửa hồ sơ mà giữ nguyên mã thì không được báo
// trùng với chính mình.
func TestExistsCodeExcludesSelf(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	e := newEmployee(t, repo, companyID, "NV001", "a@abc.vn")

	used, err := repo.ExistsCode(ctx, companyID, "NV001", &e.ID)
	if err != nil {
		t.Fatalf("ExistsCode: %v", err)
	}
	if used {
		t.Error("mã của chính mình bị báo là trùng")
	}

	used, err = repo.ExistsCode(ctx, companyID, "NV001", nil)
	if err != nil {
		t.Fatalf("ExistsCode: %v", err)
	}
	if !used {
		t.Error("mã đang dùng mà báo chưa dùng")
	}
}

// TestSoftDeletedEmployeeIsInvisible: GetByID phải trả không tìm thấy.
func TestSoftDeletedEmployeeIsInvisible(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	e := newEmployee(t, repo, companyID, "NV001", "a@abc.vn")

	if err := repo.SoftDelete(ctx, e.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	if _, err := repo.GetByID(ctx, e.ID); err == nil {
		t.Error("vẫn đọc được hồ sơ đã xoá mềm")
	}
}

// TestSoftDeleteTwiceReportsNotFound: xoá hai lần phải báo không tìm thấy,
// không báo thành công — nếu không, giao diện hiện "đã xoá" cho một thao tác
// không làm gì cả.
func TestSoftDeleteTwiceReportsNotFound(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	e := newEmployee(t, repo, companyID, "NV001", "a@abc.vn")

	if err := repo.SoftDelete(ctx, e.ID); err != nil {
		t.Fatalf("lần một: %v", err)
	}
	if err := repo.SoftDelete(ctx, e.ID); err == nil {
		t.Error("lần hai phải báo không tìm thấy")
	}
}

// =========================================================================
// TÌM KIẾM TIẾNG VIỆT
// =========================================================================

// TestSearchIgnoresDiacritics là lý do tồn tại của extension unaccent.
//
// Người dùng gõ "nguyen van" và phải ra "Nguyễn Văn". Không có nó thì ô tìm
// kiếm chỉ dùng được khi gõ đúng dấu — mà gõ đúng dấu thì đã biết tên rồi,
// và ô tìm kiếm mất phần lớn giá trị.
func TestSearchIgnoresDiacritics(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)

	e := &domainhr.Employee{
		CompanyID:    companyID,
		EmployeeCode: "NV001",
		FullName:     "Nguyễn Thị Ánh Tuyết",
		Email:        "tuyet@abc.vn",
		WorkMode:     domainhr.WorkModeOnsite,
		Status:       domainhr.StatusOfficial,
		JoinedAt:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("tạo nhân viên: %v", err)
	}

	cases := []struct {
		name  string
		query string
	}{
		{"không dấu", "nguyen thi anh"},
		{"có dấu", "Nguyễn Thị Ánh"},
		{"một từ không dấu", "tuyet"},
		{"hoa thường lẫn lộn", "NGUYEN thi"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := repo.List(ctx, domainhr.EmployeeFilter{
				Search:   tc.query,
				Page:     1,
				PageSize: 20,
			})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if total != 1 || len(items) != 1 {
				t.Errorf("tìm %q ra %d kết quả, muốn 1", tc.query, total)
			}
		})
	}
}

// TestSearchDoesNotMatchUnrelated: tìm kiếm không dấu không được nới lỏng tới
// mức khớp bừa. Một ô tìm kiếm trả về tất cả cũng vô dụng như trả về không gì.
func TestSearchDoesNotMatchUnrelated(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	newEmployee(t, repo, companyID, "NV001", "a@abc.vn")

	_, total, err := repo.List(ctx, domainhr.EmployeeFilter{
		Search:   "hoang kim long",
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 {
		t.Errorf("tìm tên không tồn tại ra %d kết quả, muốn 0", total)
	}
}

// =========================================================================
// PHẠM VI DỮ LIỆU Ở TẦNG SQL
// =========================================================================

// TestListRespectsScopedEmployeeID: usecase đặt trường này, nhưng chính SQL
// mới là chỗ thi hành nó. Một lỗi ở đây mở toàn bộ danh sách nhân viên.
func TestListRespectsScopedEmployeeID(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	me := newEmployee(t, repo, companyID, "NV001", "a@abc.vn")
	newEmployee(t, repo, companyID, "NV002", "b@abc.vn")
	newEmployee(t, repo, companyID, "NV003", "c@abc.vn")

	items, total, err := repo.List(ctx, domainhr.EmployeeFilter{
		ScopedEmployeeID: &me.ID,
		Page:             1,
		PageSize:         20,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("phạm vi cá nhân ra %d kết quả, muốn 1", total)
	}
	if items[0].ID != me.ID {
		t.Errorf("trả về %v, muốn %v", items[0].ID, me.ID)
	}
}

// TestListRespectsScopedDepartmentIDs.
func TestListRespectsScopedDepartmentIDs(t *testing.T) {
	db := testDB(t)
	empRepo := NewEmployeeRepository(db)
	deptRepo := NewDepartmentRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	depts := chain(t, deptRepo, companyID, "PHONG-A")
	other := chain(t, deptRepo, companyID, "PHONG-B")

	inA := newEmployee(t, empRepo, companyID, "NV001", "a@abc.vn")
	inA.DepartmentID = &depts[0]
	if err := empRepo.Update(ctx, inA); err != nil {
		t.Fatalf("gán phòng ban: %v", err)
	}

	inB := newEmployee(t, empRepo, companyID, "NV002", "b@abc.vn")
	inB.DepartmentID = &other[0]
	if err := empRepo.Update(ctx, inB); err != nil {
		t.Fatalf("gán phòng ban: %v", err)
	}

	items, total, err := empRepo.List(ctx, domainhr.EmployeeFilter{
		ScopedDepartmentIDs: []uuid.UUID{depts[0]},
		Page:                1,
		PageSize:            20,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("phạm vi phòng ban ra %d kết quả, muốn 1", total)
	}
	if items[0].ID != inA.ID {
		t.Errorf("trả về người của phòng khác: %v", items[0].EmployeeCode)
	}
}

// TestListExcludesSoftDeleted: người đã nghỉ không được xuất hiện trong danh
// sách, kể cả khi không truyền bộ lọc trạng thái nào.
func TestListExcludesSoftDeleted(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	newEmployee(t, repo, companyID, "NV001", "a@abc.vn")
	gone := newEmployee(t, repo, companyID, "NV002", "b@abc.vn")

	if err := repo.SoftDelete(ctx, gone.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	_, total, err := repo.List(ctx, domainhr.EmployeeFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("số nhân viên = %d, muốn 1 (người đã nghỉ vẫn hiện)", total)
	}
}

// =========================================================================
// ĐẾM NHÂN VIÊN THEO PHÒNG BAN
// =========================================================================

// TestCountEmployeesIgnoresSoftDeleted: con số này chặn việc xoá phòng ban.
// Đếm cả người đã nghỉ thì một phòng rỗng vĩnh viễn không xoá được.
func TestCountEmployeesIgnoresSoftDeleted(t *testing.T) {
	db := testDB(t)
	empRepo := NewEmployeeRepository(db)
	deptRepo := NewDepartmentRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	depts := chain(t, deptRepo, companyID, "PHONG-A")

	e := newEmployee(t, empRepo, companyID, "NV001", "a@abc.vn")
	e.DepartmentID = &depts[0]
	if err := empRepo.Update(ctx, e); err != nil {
		t.Fatalf("gán phòng ban: %v", err)
	}

	n, err := deptRepo.CountEmployees(ctx, depts[0])
	if err != nil {
		t.Fatalf("CountEmployees: %v", err)
	}
	if n != 1 {
		t.Fatalf("số nhân viên = %d, muốn 1", n)
	}

	if err := empRepo.SoftDelete(ctx, e.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}

	n, err = deptRepo.CountEmployees(ctx, depts[0])
	if err != nil {
		t.Fatalf("CountEmployees: %v", err)
	}
	if n != 0 {
		t.Errorf("số nhân viên sau khi nghỉ = %d, muốn 0", n)
	}
}
