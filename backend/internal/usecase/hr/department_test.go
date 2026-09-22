package hr

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// Bộ kiểm thử cây phòng ban.
//
// Trọng tâm là phát hiện VÒNG LẶP. Vòng lặp trong cây phòng ban không chỉ là
// dữ liệu xấu: mọi truy vấn WITH RECURSIVE sẽ chạy vô hạn và treo cả connection
// pool. Database không tự chặn được, nên tầng ghi là hàng rào duy nhất.

func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	code, _ := apperror.HTTPStatus(err)
	return code
}

// =========================================================================
// GIẢ LẬP
// =========================================================================

type fakeCompanies struct{ company *domainhr.Company }

func (f *fakeCompanies) Create(context.Context, *domainhr.Company) error { return nil }

func (f *fakeCompanies) GetFirst(context.Context) (*domainhr.Company, error) {
	return f.company, nil
}

func (f *fakeCompanies) GetByID(
	context.Context, uuid.UUID,
) (*domainhr.Company, error) {
	return f.company, nil
}

// fakeDepartments giữ một cây phòng ban trong bộ nhớ và tự tính tổ tiên/con
// cháu từ chính cây đó.
//
// Tính thật chứ không trả dữ liệu dựng sẵn: phép thử chống vòng lặp chỉ có
// nghĩa khi ListAncestorIDs phản ánh đúng cấu trúc hiện tại. Trả danh sách cố
// định sẽ khiến phép thử đạt dù thuật toán sai.
type fakeDepartments struct {
	byID  map[uuid.UUID]*domainhr.Department
	codes map[string]bool

	counts  map[uuid.UUID]int
	created []*domainhr.Department
	updated []*domainhr.Department
	deleted []uuid.UUID
}

func newFakeDepartments() *fakeDepartments {
	return &fakeDepartments{
		byID:   map[uuid.UUID]*domainhr.Department{},
		codes:  map[string]bool{},
		counts: map[uuid.UUID]int{},
	}
}

func (f *fakeDepartments) add(d *domainhr.Department) *domainhr.Department {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	f.byID[d.ID] = d
	return d
}

func (f *fakeDepartments) Create(_ context.Context, d *domainhr.Department) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	f.created = append(f.created, d)
	f.byID[d.ID] = d
	return nil
}

func (f *fakeDepartments) Update(_ context.Context, d *domainhr.Department) error {
	if f.byID[d.ID] == nil {
		return domainhr.ErrNotFound
	}
	f.updated = append(f.updated, d)
	f.byID[d.ID] = d
	return nil
}

func (f *fakeDepartments) SoftDelete(_ context.Context, id uuid.UUID) error {
	if f.byID[id] == nil {
		return domainhr.ErrNotFound
	}
	f.deleted = append(f.deleted, id)
	delete(f.byID, id)
	return nil
}

func (f *fakeDepartments) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainhr.Department, error) {
	if d := f.byID[id]; d != nil {
		copied := *d
		return &copied, nil
	}
	return nil, domainhr.ErrNotFound
}

func (f *fakeDepartments) List(
	context.Context, uuid.UUID,
) ([]*domainhr.Department, error) {
	out := make([]*domainhr.Department, 0, len(f.byID))
	for _, d := range f.byID {
		copied := *d
		out = append(out, &copied)
	}
	return out, nil
}

func (f *fakeDepartments) ListSubtreeIDs(
	_ context.Context, rootID uuid.UUID,
) ([]uuid.UUID, error) {
	out := []uuid.UUID{rootID}
	// Duyệt theo lớp. Không dùng đệ quy để một cây đã hỏng (có vòng lặp) làm
	// chính bản giả lập treo theo — thứ ta đang kiểm thử là việc NGĂN cây hỏng.
	for i := 0; i < len(out) && i < 1000; i++ {
		for _, d := range f.byID {
			if d.ParentID != nil && *d.ParentID == out[i] {
				out = append(out, d.ID)
			}
		}
	}
	return out, nil
}

func (f *fakeDepartments) ListAncestorIDs(
	_ context.Context, id uuid.UUID,
) ([]uuid.UUID, error) {
	var out []uuid.UUID
	seen := map[uuid.UUID]bool{}

	cur := f.byID[id]
	// Chặn trên 1000 bước: nếu cây đã có vòng lặp thì vòng while này không
	// bao giờ dừng, và bài kiểm thử sẽ treo thay vì báo hỏng.
	for i := 0; cur != nil && cur.ParentID != nil && i < 1000; i++ {
		p := *cur.ParentID
		if seen[p] {
			break
		}
		seen[p] = true
		out = append(out, p)
		cur = f.byID[p]
	}
	return out, nil
}

func (f *fakeDepartments) CountEmployees(
	_ context.Context, id uuid.UUID,
) (int, error) {
	return f.counts[id], nil
}

func (f *fakeDepartments) ExistsCode(
	_ context.Context, _ uuid.UUID, code string, _ *uuid.UUID,
) (bool, error) {
	return f.codes[code], nil
}

// hrHarness gom usecase và bản giả lập phòng ban.
type hrHarness struct {
	uc          *Usecase
	departments *fakeDepartments
	company     *domainhr.Company
}

func newHRHarness() *hrHarness {
	company := &domainhr.Company{ID: uuid.New(), Name: "Công ty kiểm thử"}
	depts := newFakeDepartments()

	return &hrHarness{
		uc: NewUsecase(
			&fakeCompanies{company: company},
			depts,
			nil, nil, nil, nil, nil,
		),
		departments: depts,
		company:     company,
	}
}

// chain dựng một chuỗi phòng ban cha–con: a → b → c → ...
func (h *hrHarness) chain(names ...string) []*domainhr.Department {
	out := make([]*domainhr.Department, 0, len(names))
	var parent *uuid.UUID

	for _, name := range names {
		d := h.departments.add(&domainhr.Department{
			CompanyID: h.company.ID,
			Code:      name,
			Name:      name,
			ParentID:  parent,
		})
		out = append(out, d)
		id := d.ID
		parent = &id
	}
	return out
}

// =========================================================================
// CHỐNG VÒNG LẶP
// =========================================================================

// TestCannotBeItsOwnParent là trường hợp vòng lặp đơn giản nhất.
func TestCannotBeItsOwnParent(t *testing.T) {
	h := newHRHarness()
	d := h.chain("A")[0]

	err := h.uc.validateNoCycle(context.Background(), d.ID, &d.ID)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestCannotMoveIntoOwnDescendant là trường hợp thật sự nguy hiểm.
//
// Chuyển phòng cha vào trong phòng con của chính nó tạo ra một vòng khép kín.
// Nó KHÔNG nhìn thấy được khi đọc từng bản ghi một — chỉ lộ ra khi đi hết
// chuỗi tổ tiên.
func TestCannotMoveIntoOwnDescendant(t *testing.T) {
	h := newHRHarness()
	// A → B → C → D
	chain := h.chain("A", "B", "C", "D")
	a, d := chain[0], chain[3]

	// Đặt D (cháu chắt) làm cha của A (cụ tổ).
	err := h.uc.validateNoCycle(context.Background(), a.ID, &d.ID)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("chuyển A vào dưới D: mã lỗi = %d, muốn 400", got)
	}

	// Cả con trực tiếp cũng phải chặn.
	b := chain[1]
	if err := h.uc.validateNoCycle(
		context.Background(), a.ID, &b.ID); statusOf(err) != http.StatusBadRequest {
		t.Errorf("chuyển A vào dưới B: mã lỗi = %d, muốn 400", statusOf(err))
	}
}

// TestCanMoveIntoUnrelatedBranch là mặt còn lại: chỉ chặn đúng vòng lặp, không
// chặn thao tác hợp lệ.
//
// Thiếu phép thử này thì một bản sửa "cho chắc" có thể chặn mọi lần chuyển
// phòng ban, và không ai nhận ra vì phép thử vòng lặp vẫn xanh.
func TestCanMoveIntoUnrelatedBranch(t *testing.T) {
	h := newHRHarness()
	// A → B, và X → Y (hai nhánh riêng)
	ab := h.chain("A", "B")
	xy := h.chain("X", "Y")

	// Chuyển B sang dưới Y: hợp lệ, không tạo vòng nào.
	if err := h.uc.validateNoCycle(
		context.Background(), ab[1].ID, &xy[1].ID); err != nil {
		t.Errorf("chuyển sang nhánh không liên quan phải được: %v", err)
	}

	// Chuyển A (gốc nhánh 1) sang dưới X: cũng hợp lệ.
	if err := h.uc.validateNoCycle(
		context.Background(), ab[0].ID, &xy[0].ID); err != nil {
		t.Errorf("chuyển gốc sang nhánh khác phải được: %v", err)
	}
}

// TestBecomingRootIsAlwaysSafe: bỏ cha (thành phòng gốc) không bao giờ tạo
// vòng lặp.
func TestBecomingRootIsAlwaysSafe(t *testing.T) {
	h := newHRHarness()
	chain := h.chain("A", "B", "C")

	if err := h.uc.validateNoCycle(
		context.Background(), chain[2].ID, nil); err != nil {
		t.Errorf("thành phòng gốc phải luôn an toàn: %v", err)
	}
}

// TestUpdateDepartmentRejectsCycle kiểm tra hàng rào ở ĐÚNG đường ghi thật,
// không chỉ ở hàm kiểm tra nội bộ.
//
// Quan trọng vì một bản sửa có thể vô tình bỏ lời gọi validateNoCycle khỏi
// UpdateDepartment mà chính hàm đó vẫn đúng — và phép thử chỉ gọi hàm nội bộ
// sẽ vẫn xanh.
func TestUpdateDepartmentRejectsCycle(t *testing.T) {
	h := newHRHarness()
	chain := h.chain("A", "B", "C")
	a, c := chain[0], chain[2]

	_, err := h.uc.UpdateDepartment(context.Background(), a.ID, DepartmentInput{
		Code:     "A",
		Name:     "Phòng A",
		ParentID: &c.ID,
	})
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
	if len(h.departments.updated) != 0 {
		t.Error("không được ghi gì khi phát hiện vòng lặp")
	}
}

// =========================================================================
// DỰNG CÂY
// =========================================================================

func TestDepartmentTreeNests(t *testing.T) {
	h := newHRHarness()
	chain := h.chain("A", "B", "C")

	roots, err := h.uc.DepartmentTree(context.Background())
	if err != nil {
		t.Fatalf("DepartmentTree lỗi: %v", err)
	}

	if len(roots) != 1 {
		t.Fatalf("có %d phòng gốc, muốn 1", len(roots))
	}
	if roots[0].ID != chain[0].ID {
		t.Error("gốc sai")
	}
	if len(roots[0].Children) != 1 || roots[0].Children[0].ID != chain[1].ID {
		t.Fatal("cấp hai sai")
	}
	if len(roots[0].Children[0].Children) != 1 {
		t.Fatal("cấp ba sai")
	}
}

// TestDepartmentTreeKeepsOrphans: phòng ban có cha đã bị xoá mềm phải hiện
// như phòng GỐC, không được biến mất.
//
// Làm mất nó khỏi cây nghĩa là nhân viên trong phòng đó cũng không thấy trên
// giao diện, và không ai biết dữ liệu đã mất cho tới khi đối chiếu số lượng.
func TestDepartmentTreeKeepsOrphans(t *testing.T) {
	h := newHRHarness()

	ghost := uuid.New() // id của một phòng không còn tồn tại
	h.departments.add(&domainhr.Department{
		CompanyID: h.company.ID,
		Code:      "MOCOI",
		Name:      "Phòng mồ côi",
		ParentID:  &ghost,
	})

	roots, err := h.uc.DepartmentTree(context.Background())
	if err != nil {
		t.Fatalf("DepartmentTree lỗi: %v", err)
	}
	if len(roots) != 1 {
		t.Fatalf("có %d phòng gốc, muốn 1 — phòng mồ côi đã biến mất khỏi cây", len(roots))
	}
}

func TestDepartmentTreeEmpty(t *testing.T) {
	h := newHRHarness()

	roots, err := h.uc.DepartmentTree(context.Background())
	if err != nil {
		t.Fatalf("DepartmentTree lỗi: %v", err)
	}
	// Phải là slice RỖNG chứ không nil: giá trị này đi thẳng vào JSON, và nil
	// thành `null` — client lặp qua `null` thì lỗi lúc chạy.
	if roots == nil {
		t.Error("phải trả slice rỗng, không phải nil")
	}
	if len(roots) != 0 {
		t.Errorf("có %d phòng gốc, muốn 0", len(roots))
	}
}

// =========================================================================
// XOÁ PHÒNG BAN
// =========================================================================

// TestCannotDeleteDepartmentWithEmployees: xoá phòng còn người sẽ để lại nhân
// viên trỏ tới một phòng không tồn tại.
func TestCannotDeleteDepartmentWithEmployees(t *testing.T) {
	h := newHRHarness()
	d := h.chain("A")[0]
	h.departments.counts[d.ID] = 3

	err := h.uc.DeleteDepartment(context.Background(), d.ID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.departments.deleted) != 0 {
		t.Error("không được xoá")
	}
}

// TestCannotDeleteDepartmentWithChildren: xoá phòng cha sẽ để lại cả nhánh
// dưới mồ côi.
func TestCannotDeleteDepartmentWithChildren(t *testing.T) {
	h := newHRHarness()
	chain := h.chain("A", "B")

	err := h.uc.DeleteDepartment(context.Background(), chain[0].ID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

func TestCanDeleteEmptyLeafDepartment(t *testing.T) {
	h := newHRHarness()
	d := h.chain("A")[0]

	if err := h.uc.DeleteDepartment(context.Background(), d.ID); err != nil {
		t.Fatalf("phòng lá không có người phải xoá được: %v", err)
	}
	if len(h.departments.deleted) != 1 {
		t.Error("chưa xoá")
	}
}

// =========================================================================
// KIỂM TRA ĐẦU VÀO
// =========================================================================

func TestCreateDepartmentRejectsDuplicateCode(t *testing.T) {
	h := newHRHarness()
	h.departments.codes["KT"] = true

	_, err := h.uc.CreateDepartment(context.Background(), DepartmentInput{
		Code: "KT", Name: "Phòng Kỹ thuật",
	})
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

func TestCreateDepartmentRejectsUnknownParent(t *testing.T) {
	h := newHRHarness()
	ghost := uuid.New()

	_, err := h.uc.CreateDepartment(context.Background(), DepartmentInput{
		Code: "KT", Name: "Phòng Kỹ thuật", ParentID: &ghost,
	})
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestCreateDepartmentRejectsBlankFields(t *testing.T) {
	h := newHRHarness()

	cases := []DepartmentInput{
		{Code: "", Name: "Phòng Kỹ thuật"},
		{Code: "   ", Name: "Phòng Kỹ thuật"},
		{Code: "KT", Name: ""},
		{Code: "KT", Name: "   "},
	}

	for _, in := range cases {
		_, err := h.uc.CreateDepartment(context.Background(), in)
		if got := statusOf(err); got != http.StatusBadRequest {
			t.Errorf("mã=%q tên=%q: mã lỗi = %d, muốn 400", in.Code, in.Name, got)
		}
	}
}
