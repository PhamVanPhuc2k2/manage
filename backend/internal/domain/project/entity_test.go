package project

import (
	"testing"
	"time"
)

// =========================================================================
// LUỒNG TRẠNG THÁI CÔNG VIỆC
// =========================================================================

// TestCanTransitionToIsExhaustive kiểm tra TOÀN BỘ bảng chuyển trạng thái, cả
// ô được phép lẫn ô bị cấm.
//
// Liệt kê đủ 16 ô thay vì chỉ thử vài trường hợp: bảng này là luật nghiệp vụ,
// và một ô bị nới lỏng vô tình sẽ không ai thấy. Ví dụ cho "todo → done" thì
// started_at và người kiểm đều rỗng, và mọi báo cáo dựa trên chúng đều sai mà
// không có lỗi nào được báo.
func TestCanTransitionToIsExhaustive(t *testing.T) {
	all := BoardColumns()

	// want[từ][sang] = có được phép không.
	want := map[TaskStatus]map[TaskStatus]bool{
		TaskTodo: {
			TaskTodo: true, TaskInProgress: true, TaskReview: false, TaskDone: false,
		},
		TaskInProgress: {
			TaskTodo: true, TaskInProgress: true, TaskReview: true, TaskDone: false,
		},
		TaskReview: {
			TaskTodo: false, TaskInProgress: true, TaskReview: true, TaskDone: true,
		},
		TaskDone: {
			TaskTodo: false, TaskInProgress: false, TaskReview: true, TaskDone: true,
		},
	}

	for _, from := range all {
		for _, to := range all {
			got := from.CanTransitionTo(to)
			if got != want[from][to] {
				t.Errorf("%s → %s = %v, muốn %v", from, to, got, want[from][to])
			}
		}
	}
}

// TestTransitionsAreReversibleOneStep: lùi một bước phải được, vì "chờ duyệt
// trả về đang làm" là chuyện xảy ra hằng ngày.
func TestTransitionsAreReversibleOneStep(t *testing.T) {
	pairs := [][2]TaskStatus{
		{TaskInProgress, TaskTodo},
		{TaskReview, TaskInProgress},
		{TaskDone, TaskReview},
	}
	for _, p := range pairs {
		if !p[0].CanTransitionTo(p[1]) {
			t.Errorf("phải lùi được %s → %s", p[0], p[1])
		}
	}
}

func TestTransitionFromUnknownStatusIsDenied(t *testing.T) {
	// Trạng thái lạ (dữ liệu hỏng) không được chuyển đi đâu. Bảng
	// allowedTransitions không có khoá đó nên vòng lặp không chạy lần nào —
	// phép thử này khoá lại hành vi đó thay vì để nó là tình cờ.
	bad := TaskStatus("khong_co_that")
	for _, to := range BoardColumns() {
		if bad.CanTransitionTo(to) {
			t.Errorf("trạng thái lạ không được chuyển sang %s", to)
		}
	}
}

func TestBoardColumnsOrder(t *testing.T) {
	// Thứ tự cột là thứ tự hiển thị từ trái sang phải trên bảng Kanban.
	// Đảo nó là đảo giao diện của mọi người dùng.
	want := []TaskStatus{TaskTodo, TaskInProgress, TaskReview, TaskDone}
	got := BoardColumns()

	if len(got) != len(want) {
		t.Fatalf("có %d cột, muốn %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("cột thứ %d = %s, muốn %s", i, got[i], want[i])
		}
	}
}

// TestBoardColumnsIsACopy: trả thẳng slice nội bộ ra ngoài thì một chỗ gọi lỡ
// tay sắp xếp lại là đảo bảng Kanban cho mọi người.
func TestBoardColumnsIsACopy(t *testing.T) {
	first := BoardColumns()
	first[0] = TaskDone

	if BoardColumns()[0] != TaskTodo {
		t.Fatal("BoardColumns trả về slice dùng chung, sửa được từ bên ngoài")
	}
}

// =========================================================================
// VAI TRÒ TRONG DỰ ÁN
// =========================================================================

// TestRoleCapabilities khoá lại ba mức quyền trong một dự án.
//
// Viết thành bảng đầy đủ vì đây là điểm dễ nới lỏng nhất: thêm RoleViewer vào
// CanWrite là cho người chỉ được xem sửa công việc, và không có phép thử nào
// khác bắt được.
func TestRoleCapabilities(t *testing.T) {
	cases := []struct {
		role      Role
		canWrite  bool
		canManage bool
	}{
		{RoleOwner, true, true},
		{RoleMember, true, false},
		{RoleViewer, false, false},
		// Vai trò rỗng là "không phải thành viên" — không được gì.
		{Role(""), false, false},
		{Role("vai_tro_la"), false, false},
	}

	for _, c := range cases {
		if got := c.role.CanWrite(); got != c.canWrite {
			t.Errorf("%q.CanWrite() = %v, muốn %v", c.role, got, c.canWrite)
		}
		if got := c.role.CanManage(); got != c.canManage {
			t.Errorf("%q.CanManage() = %v, muốn %v", c.role, got, c.canManage)
		}
	}
}

func TestRoleValid(t *testing.T) {
	for _, r := range []Role{RoleOwner, RoleMember, RoleViewer} {
		if !r.Valid() {
			t.Errorf("%q phải hợp lệ", r)
		}
	}
	for _, r := range []Role{"", "admin", "OWNER"} {
		if Role(r).Valid() {
			t.Errorf("%q không được coi là hợp lệ", r)
		}
	}
}

// =========================================================================
// TRẠNG THÁI DỰ ÁN
// =========================================================================

func TestProjectStatusClosed(t *testing.T) {
	// Closed() quyết định dự án còn sửa được hay không. Thiếu một trạng thái
	// ở đây nghĩa là dự án đã huỷ vẫn nhận được công việc mới.
	cases := map[Status]bool{
		StatusPlanning:  false,
		StatusActive:    false,
		StatusOnHold:    false,
		StatusCompleted: true,
		StatusCancelled: true,
	}

	for s, want := range cases {
		if got := s.Closed(); got != want {
			t.Errorf("%q.Closed() = %v, muốn %v", s, got, want)
		}
	}
}

func TestProjectStatusValid(t *testing.T) {
	for _, s := range []Status{
		StatusPlanning, StatusActive, StatusOnHold, StatusCompleted, StatusCancelled,
	} {
		if !s.Valid() {
			t.Errorf("%q phải hợp lệ", s)
		}
	}
	if Status("dang_lam").Valid() {
		t.Error("trạng thái lạ không được coi là hợp lệ")
	}
}

// =========================================================================
// TIẾN ĐỘ
// =========================================================================

func TestProjectProgress(t *testing.T) {
	cases := []struct {
		name  string
		total int
		done  int
		want  int
	}{
		{"chưa có việc nào", 0, 0, 0},
		{"chưa làm gì", 10, 0, 0},
		{"một nửa", 10, 5, 50},
		{"xong hết", 10, 10, 100},
		{"làm tròn xuống", 3, 1, 33},
		{"làm tròn xuống, gần tròn", 3, 2, 66},
	}

	for _, c := range cases {
		p := &Project{TaskCount: c.total, DoneTaskCount: c.done}
		if got := p.Progress(); got != c.want {
			t.Errorf("%s: Progress() = %d, muốn %d", c.name, got, c.want)
		}
	}
}

// TestProjectProgressNoDivideByZero: dự án chưa có công việc nào là trạng thái
// bình thường của một dự án mới tạo, và chia cho 0 sẽ làm panic cả request.
func TestProjectProgressNoDivideByZero(t *testing.T) {
	p := &Project{TaskCount: 0, DoneTaskCount: 0}
	if got := p.Progress(); got != 0 {
		t.Errorf("Progress() = %d, muốn 0", got)
	}
}

// =========================================================================
// MÃ VÀ HẠN CÔNG VIỆC
// =========================================================================

func TestTaskCode(t *testing.T) {
	// Mã hiển thị ghép mã dự án với số thứ tự trong dự án: "WEB-42".
	// Đây là thứ người dùng đọc cho nhau, nên nó phải ổn định.
	t1 := &Task{ProjectCode: "WEB", Seq: 42}
	if got := t1.Code(); got != "WEB-42" {
		t.Errorf("Code() = %q, muốn \"WEB-42\"", got)
	}

	// Thiếu mã dự án (bản ghi đọc lên không JOIN) thì trả rỗng, không trả
	// "-42" — một mã như thế trông như dữ liệu thật và sẽ lọt vào email.
	t2 := &Task{Seq: 42}
	if got := t2.Code(); got != "" {
		t.Errorf("Code() = %q, muốn chuỗi rỗng", got)
	}
}

func TestTaskOverdue(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	past := now.AddDate(0, 0, -1)
	future := now.AddDate(0, 0, 1)

	cases := []struct {
		name string
		task *Task
		want bool
	}{
		{"quá hạn và chưa xong", &Task{DueDate: &past, Status: TaskInProgress}, true},
		{"chưa tới hạn", &Task{DueDate: &future, Status: TaskInProgress}, false},
		{"không có hạn", &Task{Status: TaskInProgress}, false},

		// Việc ĐÃ XONG không bao giờ là quá hạn, dù xong muộn.
		//
		// Phân biệt này quan trọng: danh sách "việc quá hạn" là danh sách việc
		// cần làm gì đó. Để việc đã xong trong đó sẽ khiến danh sách dài ra
		// mãi và không ai đọc nữa.
		{"đã xong dù muộn", &Task{DueDate: &past, Status: TaskDone}, false},
	}

	for _, c := range cases {
		if got := c.task.Overdue(now); got != c.want {
			t.Errorf("%s: Overdue() = %v, muốn %v", c.name, got, c.want)
		}
	}
}

// TestTaskOverdueAtExactDeadline: đúng thời điểm hạn thì CHƯA quá hạn.
//
// Dùng now.After(due) chứ không !now.Before(due) là có ý — đúng giây hạn vẫn
// còn kịp. Phép thử này khoá lại ranh giới đó.
func TestTaskOverdueAtExactDeadline(t *testing.T) {
	due := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	task := &Task{DueDate: &due, Status: TaskInProgress}

	if task.Overdue(due) {
		t.Error("đúng thời điểm hạn thì chưa quá hạn")
	}
	if !task.Overdue(due.Add(time.Second)) {
		t.Error("một giây sau hạn thì đã quá hạn")
	}
}

func TestPriorityValid(t *testing.T) {
	for _, p := range []Priority{
		PriorityLow, PriorityMedium, PriorityHigh, PriorityUrgent,
	} {
		if !p.Valid() {
			t.Errorf("%q phải hợp lệ", p)
		}
	}
	for _, p := range []string{"", "critical", "LOW"} {
		if Priority(p).Valid() {
			t.Errorf("%q không được coi là hợp lệ", p)
		}
	}
}

func TestTaskStatusValid(t *testing.T) {
	for _, s := range BoardColumns() {
		if !s.Valid() {
			t.Errorf("%q phải hợp lệ", s)
		}
	}
	if TaskStatus("blocked").Valid() {
		t.Error("trạng thái lạ không được coi là hợp lệ")
	}
}
