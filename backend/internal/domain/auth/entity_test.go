package auth

import (
	"testing"

	"github.com/google/uuid"
)

// Đây là bộ kiểm thử chống IDOR.
//
// CanSeeEmployee là hàng rào duy nhất giữa "có quyền employee:read" và "được
// đọc hồ sơ của NGƯỜI NÀY". Middleware không thấy bản ghi cụ thể nên không
// làm được việc đó; nếu hàm này sai thì mọi endpoint nhân sự đều hở.

func TestCanSeeEmployeeNilActor(t *testing.T) {
	// Actor nil nghĩa là context không có danh tính — phải là TỪ CHỐI, không
	// phải cho qua. Mặc định "mở" ở đây là lỗ hổng nghiêm trọng nhất có thể.
	var a *Actor
	if a.CanSeeEmployee(uuid.New(), nil) {
		t.Fatal("actor nil phải bị từ chối")
	}
}

func TestCanSeeEmployeeScopeAll(t *testing.T) {
	a := &Actor{EmployeeID: uuid.New(), Scope: ScopeAll}

	if !a.CanSeeEmployee(uuid.New(), nil) {
		t.Error("phạm vi toàn công ty phải thấy được mọi người")
	}
	deptID := uuid.New()
	if !a.CanSeeEmployee(uuid.New(), &deptID) {
		t.Error("phạm vi toàn công ty phải thấy được người ở phòng bất kỳ")
	}
}

func TestCanSeeEmployeeScopeSelf(t *testing.T) {
	me := uuid.New()
	a := &Actor{EmployeeID: me, Scope: ScopeSelf}

	if !a.CanSeeEmployee(me, nil) {
		t.Error("phải thấy được chính mình")
	}
	if a.CanSeeEmployee(uuid.New(), nil) {
		t.Error("phạm vi bản thân KHÔNG được thấy người khác")
	}

	// Ngay cả khi người kia ở cùng phòng: phạm vi "self" nghĩa là chỉ mình.
	deptID := uuid.New()
	if a.CanSeeEmployee(uuid.New(), &deptID) {
		t.Error("phạm vi bản thân không được thấy người cùng phòng")
	}
}

func TestCanSeeEmployeeScopeDepartment(t *testing.T) {
	me := uuid.New()
	managed, other := uuid.New(), uuid.New()

	a := &Actor{
		EmployeeID:           me,
		Scope:                ScopeDepartment,
		ManagedDepartmentIDs: []uuid.UUID{managed},
	}

	if !a.CanSeeEmployee(me, nil) {
		t.Error("trưởng phòng phải thấy được chính mình dù không truyền phòng ban")
	}
	if !a.CanSeeEmployee(uuid.New(), &managed) {
		t.Error("phải thấy được người trong phòng mình quản lý")
	}
	if a.CanSeeEmployee(uuid.New(), &other) {
		t.Error("KHÔNG được thấy người ở phòng khác")
	}

	// departmentID nil với người KHÁC mình: không biết họ ở phòng nào thì
	// phải từ chối. Cho qua ở đây là hở toàn bộ hồ sơ chưa gán phòng ban.
	if a.CanSeeEmployee(uuid.New(), nil) {
		t.Error("không biết phòng ban của người khác thì phải từ chối")
	}
}

// TestCanSeeEmployeeManagesMultipleDepartments: trưởng phòng có phòng con thì
// ManagedDepartmentIDs gồm cả cây con, và mọi phòng trong đó đều phải thấy được.
func TestCanSeeEmployeeManagesMultipleDepartments(t *testing.T) {
	parent, child, grandchild := uuid.New(), uuid.New(), uuid.New()

	a := &Actor{
		EmployeeID:           uuid.New(),
		Scope:                ScopeDepartment,
		ManagedDepartmentIDs: []uuid.UUID{parent, child, grandchild},
	}

	for _, dept := range []uuid.UUID{parent, child, grandchild} {
		if !a.CanSeeEmployee(uuid.New(), &dept) {
			t.Errorf("phải thấy được người ở phòng %s", dept)
		}
	}
}

// TestCanSeeEmployeeUnknownScope: phạm vi lạ (dữ liệu hỏng, hoặc thêm phạm vi
// mới mà quên xử lý) phải TỪ CHỐI.
//
// Nhánh default mở sẽ biến một lỗi chính tả trong bảng roles thành lỗ hổng.
func TestCanSeeEmployeeUnknownScope(t *testing.T) {
	a := &Actor{EmployeeID: uuid.New(), Scope: Scope("phạm-vi-lạ")}

	if a.CanSeeEmployee(a.EmployeeID, nil) {
		t.Error("phạm vi không nhận ra phải từ chối, kể cả với chính mình")
	}
}

// TestCanSeeEmployeeEmptyScopeDenies: chuỗi rỗng là giá trị zero của Scope —
// đúng thứ ta nhận được nếu token thiếu trường scope.
func TestCanSeeEmployeeEmptyScopeDenies(t *testing.T) {
	a := &Actor{EmployeeID: uuid.New(), Scope: ""}

	if a.CanSeeEmployee(a.EmployeeID, nil) {
		t.Error("phạm vi rỗng phải từ chối")
	}
}

func TestCan(t *testing.T) {
	a := &Actor{Permissions: map[string]struct{}{PermEmployeeRead: {}}}

	if !a.Can(PermEmployeeRead) {
		t.Error("phải có quyền employee:read")
	}
	if a.Can(PermEmployeeDelete) {
		t.Error("không được có quyền chưa cấp")
	}

	var nilActor *Actor
	if nilActor.Can(PermEmployeeRead) {
		t.Error("actor nil phải không có quyền nào")
	}

	// Actor có map quyền rỗng (không nil) cũng phải không có quyền nào.
	if (&Actor{Permissions: map[string]struct{}{}}).Can(PermEmployeeRead) {
		t.Error("map quyền rỗng phải không có quyền nào")
	}
}

// TestPermissionCodesAreUnique: hai hằng quyền trùng chuỗi nghĩa là cấp một
// cái là cấp luôn cái kia — đúng loại lỗi không ai phát hiện cho tới khi rò dữ
// liệu, vì mã quyền chỉ là chuỗi và trình biên dịch không nói gì.
func TestPermissionCodesAreUnique(t *testing.T) {
	all := []string{
		PermEmployeeRead, PermEmployeeCreate, PermEmployeeUpdate, PermEmployeeDelete,
		PermDepartmentRead, PermDepartmentCreate, PermDepartmentUpdate, PermDepartmentDelete,
		PermPositionRead, PermPositionManage,
		PermRoleRead, PermRoleAssign,
		PermProjectRead, PermProjectCreate, PermProjectUpdate, PermProjectDelete,
		PermTaskRead, PermTaskCreate, PermTaskUpdate, PermTaskDelete,
		PermAttendanceRead, PermAttendanceReadAll, PermAttendanceManage,
		PermLeaveRead, PermLeaveCreate, PermLeaveApprove, PermLeaveManage,
		PermScheduleManage,
		PermPayrollReadOwn, PermPayrollReadAll, PermPayrollManage, PermPayrollApprove,
		PermSalaryRead, PermSalaryManage,
		PermAuditRead,
		PermChatRead, PermChatCreate,
	}

	seen := make(map[string]int, len(all))
	for i, code := range all {
		if code == "" {
			t.Errorf("mã quyền thứ %d là chuỗi rỗng", i)
			continue
		}
		if prev, dup := seen[code]; dup {
			t.Errorf("mã quyền %q trùng (vị trí %d và %d)", code, prev, i)
		}
		seen[code] = i
	}
}
