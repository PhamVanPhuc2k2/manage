package project

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
)

// Bộ kiểm thử tạo, sửa và liệt kê công việc.
//
// Phần kéo-thả Kanban đã có bộ riêng trong task_test.go. Chỗ này lo hai thứ
// khác: PHẠM VI DỰ ÁN (middleware chỉ biết "người này có quyền task:update",
// nó không biết task đó thuộc dự án nào) và các luật hình thành nên một
// công việc hợp lệ.

// =========================================================================
// TẠO CÔNG VIỆC
// =========================================================================

func TestCreateTaskDefaults(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)

	got, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID,
		Title:     "  Viết tài liệu  ",
	})
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.Status != domainproject.TaskTodo {
		t.Errorf("trạng thái = %q, muốn %q", got.Status, domainproject.TaskTodo)
	}
	if got.Priority != domainproject.PriorityMedium {
		t.Errorf("độ ưu tiên = %q, muốn %q", got.Priority, domainproject.PriorityMedium)
	}
	if got.Title != "Viết tài liệu" {
		t.Errorf("tiêu đề = %q, khoảng trắng chưa được cắt", got.Title)
	}
	if got.ReporterID != owner {
		t.Errorf("người tạo = %v, muốn %v", got.ReporterID, owner)
	}
}

// TestCreateTaskAppendsToEndOfColumn: task mới rơi xuống cuối cột, không
// chen lên đầu chỗ người khác đang làm dở.
func TestCreateTaskAppendsToEndOfColumn(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)

	h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Có sẵn",
		Status: domainproject.TaskTodo, SortOrder: 5000,
	})

	got, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Mới",
	})
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.SortOrder != 5000+sortOrderStep {
		t.Errorf("sort_order = %v, muốn %v", got.SortOrder, 5000+sortOrderStep)
	}
}

// TestCreateTaskInProgressStampsStart: tạo thẳng vào cột "Đang làm" phải ghi
// mốc bắt đầu, nếu không mọi phép đo thời gian thực hiện đều thiếu.
func TestCreateTaskInProgressStampsStart(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)

	got, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Bắt tay vào làm",
		Status: domainproject.TaskInProgress,
	})
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.StartedAt == nil {
		t.Error("chưa ghi mốc bắt đầu cho task tạo thẳng vào cột Đang làm")
	}
}

func TestCreateTaskValidatesInput(t *testing.T) {
	owner := uuid.New()
	negative := -1.0

	cases := []struct {
		name   string
		mutate func(*TaskInput)
	}{
		{"tiêu đề rỗng", func(in *TaskInput) { in.Title = "   " }},
		{"tiêu đề quá dài", func(in *TaskInput) { in.Title = strings.Repeat("a", 501) }},
		{"trạng thái lạ", func(in *TaskInput) { in.Status = "dang-nghi" }},
		{"độ ưu tiên lạ", func(in *TaskInput) { in.Priority = "cuc-ky-gap" }},
		{"giờ ước lượng âm", func(in *TaskInput) { in.EstimateHours = &negative }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newProjectHarness()
			p := h.seedProject(owner)

			in := TaskInput{ProjectID: p.ID, Title: "Việc hợp lệ"}
			tc.mutate(&in)

			_, err := h.uc.CreateTask(context.Background(), actorIn(owner), in)
			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(h.tasks.created) != 0 {
				t.Error("đã tạo task dù đầu vào không hợp lệ")
			}
		})
	}
}

// TestAssigneeMustBeProjectMember.
//
// Không chặn thì task rơi vào tay người không thấy dự án đó — họ nhận thông
// báo về một việc mà mở ra chỉ thấy 404, và không hiểu chuyện gì xảy ra.
func TestAssigneeMustBeProjectMember(t *testing.T) {
	owner := uuid.New()
	outsider := uuid.New()

	h := newProjectHarness()
	p := h.seedProject(owner)

	_, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Việc", AssigneeID: &outsider,
	})

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestAssigneeMustExist(t *testing.T) {
	owner := uuid.New()
	resigned := uuid.New()

	h := newProjectHarness()
	p := h.seedProject(owner)
	h.members.join(p.ID, resigned, domainproject.RoleMember)
	h.employees.missing[resigned] = true

	_, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Việc", AssigneeID: &resigned,
	})

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestSubtaskNestingIsLimitedToTwoLevels.
//
// Chặn ở tầng nghiệp vụ vì database không diễn đạt được ràng buộc này.
// Không chặn thì cây task sâu tuỳ ý, và mọi chỗ hiển thị đệ quy phải tự lo
// việc giới hạn độ sâu — tức là lo ở nhiều nơi thay vì một nơi.
func TestSubtaskNestingIsLimitedToTwoLevels(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)

	parent := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc cha", Status: domainproject.TaskTodo,
	})
	child := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc con", Status: domainproject.TaskTodo,
		ParentTaskID: &parent.ID,
	})

	// Cấp hai thì được.
	if _, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Con khác", ParentTaskID: &parent.ID,
	}); err != nil {
		t.Fatalf("task con cấp hai phải tạo được: %v", err)
	}

	// Cấp ba thì không.
	_, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Cháu", ParentTaskID: &child.ID,
	})
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestSubtaskParentMustBeInSameProject(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	other := h.seedProject(owner)

	parent := h.tasks.add(&domainproject.Task{
		ProjectID: other.ID, Title: "Việc dự án khác",
		Status: domainproject.TaskTodo,
	})

	_, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Con", ParentTaskID: &parent.ID,
	})

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

func TestCreateTaskRejectsMissingParent(t *testing.T) {
	owner := uuid.New()
	ghost := uuid.New()

	h := newProjectHarness()
	p := h.seedProject(owner)

	_, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Con", ParentTaskID: &ghost,
	})

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestViewerCannotCreateTask: vai trò "người xem" chỉ đọc.
func TestViewerCannotCreateTask(t *testing.T) {
	owner := uuid.New()
	viewer := uuid.New()

	h := newProjectHarness()
	p := h.seedProject(owner)
	h.members.join(p.ID, viewer, domainproject.RoleViewer)

	_, err := h.uc.CreateTask(context.Background(), actorIn(viewer), TaskInput{
		ProjectID: p.ID, Title: "Việc",
	})

	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
}

// TestOutsiderGetsNotFoundNotForbidden: 403 xác nhận rằng dự án có tồn tại
// — với người ngoài, đó đã là rò rỉ.
func TestOutsiderGetsNotFoundNotForbidden(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)

	_, err := h.uc.CreateTask(context.Background(), actorIn(uuid.New()), TaskInput{
		ProjectID: p.ID, Title: "Việc",
	})

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestCannotCreateTaskInClosedProject(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	p.Status = domainproject.StatusCompleted
	h.projects.put(p)

	_, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Việc",
	})

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// TestCreateTaskNotifiesAssignee: người được giao việc phải biết.
func TestCreateTaskNotifiesAssignee(t *testing.T) {
	owner := uuid.New()
	member := uuid.New()

	h := newProjectHarness()
	p := h.seedProject(owner)
	h.members.join(p.ID, member, domainproject.RoleMember)
	h.employees.names[owner] = "Nguyễn Văn A"

	if _, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Việc", AssigneeID: &member,
	}); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.events.published) != 1 {
		t.Fatalf("số sự kiện = %d, muốn 1", len(h.events.published))
	}
	e := h.events.published[0]
	if e.Name != domainproject.JobTaskAssigned {
		t.Errorf("sự kiện = %q, muốn %q", e.Name, domainproject.JobTaskAssigned)
	}
	if e.ActorName != "Nguyễn Văn A" {
		t.Errorf("tên người gây sự kiện = %q — thiếu thì thông báo đọc thành "+
			"\"Một thành viên đã giao việc cho bạn\"", e.ActorName)
	}
}

// TestSelfAssignmentSendsNoNotification: tự giao việc cho mình thì không cần
// tự báo cho mình.
func TestSelfAssignmentSendsNoNotification(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)

	if _, err := h.uc.CreateTask(context.Background(), actorIn(owner), TaskInput{
		ProjectID: p.ID, Title: "Việc", AssigneeID: &owner,
	}); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.events.published) != 0 {
		t.Errorf("đã gửi %d thông báo cho chính người tự giao việc",
			len(h.events.published))
	}
}

// =========================================================================
// SỬA CÔNG VIỆC
// =========================================================================

func TestUpdateTaskEnforcesStatusTransitions(t *testing.T) {
	owner := uuid.New()

	cases := []struct {
		name string
		from domainproject.TaskStatus
		to   domainproject.TaskStatus
		want int
	}{
		{"todo → đang làm", domainproject.TaskTodo, domainproject.TaskInProgress, http.StatusOK},
		{"đang làm → chờ duyệt", domainproject.TaskInProgress, domainproject.TaskReview, http.StatusOK},
		{"chờ duyệt → đang làm (trả lại)", domainproject.TaskReview, domainproject.TaskInProgress, http.StatusOK},
		{"todo → hoàn thành", domainproject.TaskTodo, domainproject.TaskDone, http.StatusBadRequest},
		{"todo → chờ duyệt", domainproject.TaskTodo, domainproject.TaskReview, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newProjectHarness()
			p := h.seedProject(owner)
			task := h.tasks.add(&domainproject.Task{
				ProjectID: p.ID, Title: "Việc", Status: tc.from,
			})

			_, err := h.uc.UpdateTask(context.Background(), actorIn(owner),
				task.ID, TaskInput{Title: "Việc", Status: tc.to})

			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d (%v)", got, tc.want, err)
			}
		})
	}
}

// TestUpdateTaskKeepsStatusWhenOmitted: không gửi trạng thái nghĩa là "giữ
// nguyên", không phải "đặt về rỗng".
func TestUpdateTaskKeepsStatusWhenOmitted(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskInProgress,
	})

	got, err := h.uc.UpdateTask(context.Background(), actorIn(owner),
		task.ID, TaskInput{Title: "Việc đã đổi tên"})
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Status != domainproject.TaskInProgress {
		t.Errorf("trạng thái = %q, muốn giữ nguyên %q",
			got.Status, domainproject.TaskInProgress)
	}
}

func TestUpdateTaskLogsStatusChange(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
		ReporterID: owner,
	})

	if _, err := h.uc.UpdateTask(context.Background(), actorIn(owner),
		task.ID, TaskInput{Title: "Việc", Status: domainproject.TaskInProgress},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.activities.actions(task.ID), domainproject.ActionStatusChanged) {
		t.Errorf("nhật ký = %v, phải có %q",
			h.activities.actions(task.ID), domainproject.ActionStatusChanged)
	}
}

// TestStatusChangeNotifiesAssigneeAndReporter, trừ chính người vừa đổi.
func TestStatusChangeNotifiesAssigneeAndReporter(t *testing.T) {
	owner := uuid.New()
	assignee := uuid.New()
	reporter := uuid.New()

	h := newProjectHarness()
	p := h.seedProject(owner)
	h.members.join(p.ID, assignee, domainproject.RoleMember)

	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
		AssigneeID: &assignee, ReporterID: reporter,
	})

	if _, err := h.uc.UpdateTask(context.Background(), actorIn(owner),
		task.ID, TaskInput{
			Title: "Việc", Status: domainproject.TaskInProgress,
			AssigneeID: &assignee,
		},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.events.published) != 1 {
		t.Fatalf("số sự kiện = %d, muốn 1", len(h.events.published))
	}
	got := h.events.published[0].Recipients
	if len(got) != 2 {
		t.Errorf("người nhận = %v, muốn cả người thực hiện lẫn người tạo", got)
	}
}

// TestChangingOwnTaskStatusNotifiesNobody: người đổi trạng thái vừa là
// người thực hiện vừa là người tạo thì không có ai khác để báo.
func TestChangingOwnTaskStatusNotifiesNobody(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)

	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
		AssigneeID: &owner, ReporterID: owner,
	})

	if _, err := h.uc.UpdateTask(context.Background(), actorIn(owner),
		task.ID, TaskInput{
			Title: "Việc", Status: domainproject.TaskInProgress,
			AssigneeID: &owner,
		},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if len(h.events.published) != 0 {
		t.Errorf("đã gửi %d thông báo cho chính người vừa thao tác",
			len(h.events.published))
	}
}

func TestUpdateMissingTaskReturns404(t *testing.T) {
	h := newProjectHarness()

	_, err := h.uc.UpdateTask(context.Background(), actorIn(uuid.New()),
		uuid.New(), TaskInput{Title: "Việc"})

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestCannotUpdateTaskInClosedProject(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
	})
	p.Status = domainproject.StatusCompleted
	h.projects.put(p)

	_, err := h.uc.UpdateTask(context.Background(), actorIn(owner),
		task.ID, TaskInput{Title: "Việc"})

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// =========================================================================
// MỐC THỜI GIAN THEO TRẠNG THÁI
// =========================================================================

// TestApplyStatusTimestamps khoá lại luật: started_at chỉ ghi LẦN ĐẦU.
//
// Đưa task về "đang làm" rồi quay lại không được ghi đè mốc bắt đầu thật,
// nếu không mọi phép đo thời gian thực hiện sẽ ngắn hơn sự thật.
func TestApplyStatusTimestamps(t *testing.T) {
	old := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)

	t.Run("về todo thì xoá cả hai mốc", func(t *testing.T) {
		task := &domainproject.Task{StartedAt: &old, CompletedAt: &old}
		applyStatusTimestamps(task, domainproject.TaskTodo)

		if task.StartedAt != nil || task.CompletedAt != nil {
			t.Errorf("mốc = %v / %v, muốn cả hai nil",
				task.StartedAt, task.CompletedAt)
		}
	})

	t.Run("giữ mốc bắt đầu cũ", func(t *testing.T) {
		task := &domainproject.Task{StartedAt: &old}
		applyStatusTimestamps(task, domainproject.TaskInProgress)

		if task.StartedAt == nil || !task.StartedAt.Equal(old) {
			t.Errorf("mốc bắt đầu = %v, muốn giữ %v", task.StartedAt, old)
		}
	})

	t.Run("hoàn thành thì ghi cả hai nếu thiếu", func(t *testing.T) {
		task := &domainproject.Task{}
		applyStatusTimestamps(task, domainproject.TaskDone)

		if task.StartedAt == nil || task.CompletedAt == nil {
			t.Errorf("mốc = %v / %v, muốn cả hai có giá trị",
				task.StartedAt, task.CompletedAt)
		}
	})

	t.Run("rời khỏi hoàn thành thì xoá mốc hoàn thành", func(t *testing.T) {
		task := &domainproject.Task{StartedAt: &old, CompletedAt: &old}
		applyStatusTimestamps(task, domainproject.TaskReview)

		if task.CompletedAt != nil {
			t.Errorf("mốc hoàn thành = %v, muốn nil", task.CompletedAt)
		}
		if task.StartedAt == nil || !task.StartedAt.Equal(old) {
			t.Errorf("mốc bắt đầu = %v, muốn giữ %v", task.StartedAt, old)
		}
	})
}

// =========================================================================
// LIỆT KÊ VÀ PHẠM VI
// =========================================================================

// TestListTasksIgnoresScopeFromQueryString.
func TestListTasksIgnoresScopeFromQueryString(t *testing.T) {
	member := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(uuid.New())
	h.members.join(p.ID, member, domainproject.RoleMember)
	h.projects.memberOf = []uuid.UUID{p.ID}

	forged := domainproject.TaskFilter{
		VisibleProjectIDs: []uuid.UUID{uuid.New()},
		RestrictProjects:  false,
	}

	if _, err := h.uc.ListTasks(
		context.Background(), actorIn(member), forged,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.tasks.lastFilter
	if !f.RestrictProjects {
		t.Error("phạm vi giả mạo đã tắt được giới hạn")
	}
	if len(f.VisibleProjectIDs) != 1 || f.VisibleProjectIDs[0] != p.ID {
		t.Errorf("danh sách dự án = %v, muốn [%v]", f.VisibleProjectIDs, p.ID)
	}
}

// TestCompanyWideScopeSeesEveryProject.
func TestCompanyWideScopeSeesEveryProject(t *testing.T) {
	h := newProjectHarness()
	h.seedProject(uuid.New())

	if _, err := h.uc.ListTasks(context.Background(),
		actorCompanyWide(uuid.New()), domainproject.TaskFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if h.tasks.lastFilter.RestrictProjects {
		t.Error("phạm vi toàn công ty không được bị giới hạn")
	}
}

// TestMemberOfNoProjectsSeesNothing.
//
// restrict=true kèm danh sách RỖNG nghĩa là không thấy dự án nào. Gộp nó
// với restrict=false là nhân viên thường nhìn thấy toàn bộ task của công ty.
func TestMemberOfNoProjectsSeesNothing(t *testing.T) {
	h := newProjectHarness()

	if _, err := h.uc.ListTasks(context.Background(),
		actorIn(uuid.New()), domainproject.TaskFilter{},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.tasks.lastFilter
	if !f.RestrictProjects {
		t.Fatal("phải bị giới hạn")
	}
	if len(f.VisibleProjectIDs) != 0 {
		t.Errorf("danh sách dự án = %v, muốn rỗng", f.VisibleProjectIDs)
	}
}

func TestListTasksOfProjectYouCannotSeeReturns404(t *testing.T) {
	h := newProjectHarness()
	p := h.seedProject(uuid.New())

	_, err := h.uc.ListTasks(context.Background(), actorIn(uuid.New()),
		domainproject.TaskFilter{ProjectID: &p.ID})

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestMyTasksIncludesSubtasks.
//
// Task con cũng là việc phải làm, nên màn hình "Việc của tôi" KHÔNG lọc chỉ
// task gốc — khác với bảng Kanban, nơi task con hiển thị lồng trong task cha.
func TestMyTasksIncludesSubtasks(t *testing.T) {
	member := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(uuid.New())
	h.members.join(p.ID, member, domainproject.RoleMember)
	h.projects.memberOf = []uuid.UUID{p.ID}

	if _, err := h.uc.MyTasks(context.Background(), actorIn(member),
		domainproject.TaskFilter{OnlyRoots: true},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.tasks.lastFilter
	if f.OnlyRoots {
		t.Error("Việc của tôi không được lọc bỏ task con")
	}
	if f.AssigneeID == nil || *f.AssigneeID != member {
		t.Errorf("người thực hiện = %v, muốn %v", f.AssigneeID, member)
	}
}

func TestMyTasksRequiresActor(t *testing.T) {
	h := newProjectHarness()

	_, err := h.uc.MyTasks(context.Background(), nil, domainproject.TaskFilter{})
	if got := statusOf(err); got != http.StatusUnauthorized {
		t.Errorf("mã lỗi = %d, muốn 401", got)
	}
}

func TestListTasksNormalizesPageSize(t *testing.T) {
	h := newProjectHarness()

	if _, err := h.uc.ListTasks(context.Background(), actorIn(uuid.New()),
		domainproject.TaskFilter{Page: -2, PageSize: 1000000},
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.tasks.lastFilter
	if f.Page != 1 || f.PageSize != maxPageSize {
		t.Errorf("page/size = %d/%d, muốn 1/%d", f.Page, f.PageSize, maxPageSize)
	}
}

// =========================================================================
// BẢNG KANBAN
// =========================================================================

// TestBoardHasEveryColumnEvenWhenEmpty: giao diện cần vẽ cột trống để người
// dùng có chỗ thả task vào.
func TestBoardHasEveryColumnEvenWhenEmpty(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
	})

	got, err := h.uc.GetBoard(context.Background(), actorIn(owner), p.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	for _, col := range domainproject.BoardColumns() {
		if got.Columns[col] == nil {
			t.Errorf("thiếu cột %q", col)
		}
	}
	if len(got.Columns[domainproject.TaskTodo]) != 1 {
		t.Errorf("cột Cần làm có %d việc, muốn 1",
			len(got.Columns[domainproject.TaskTodo]))
	}
}

func TestBoardOfProjectYouCannotSeeReturns404(t *testing.T) {
	h := newProjectHarness()
	p := h.seedProject(uuid.New())

	_, err := h.uc.GetBoard(context.Background(), actorIn(uuid.New()), p.ID)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// TASK CON VÀ NHẬT KÝ
// =========================================================================

func TestListSubtasksAndActivities(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)

	parent := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Cha", Status: domainproject.TaskTodo,
	})
	h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Con", Status: domainproject.TaskTodo,
		ParentTaskID: &parent.ID,
	})
	h.activities.logged = append(h.activities.logged, &domainproject.Activity{
		TaskID: parent.ID, Action: domainproject.ActionCreated,
	})

	subs, err := h.uc.ListSubtasks(context.Background(), actorIn(owner), parent.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("số task con = %d, muốn 1", len(subs))
	}

	acts, err := h.uc.ListActivities(context.Background(), actorIn(owner), parent.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(acts) != 1 {
		t.Errorf("số dòng nhật ký = %d, muốn 1", len(acts))
	}
}

func TestSubtasksOfTaskYouCannotSeeReturns404(t *testing.T) {
	h := newProjectHarness()
	p := h.seedProject(uuid.New())
	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
	})

	_, err := h.uc.ListSubtasks(context.Background(), actorIn(uuid.New()), task.ID)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// TIỆN ÍCH
// =========================================================================

func TestSameUUIDPtr(t *testing.T) {
	a, b := uuid.New(), uuid.New()

	cases := []struct {
		name string
		x, y *uuid.UUID
		want bool
	}{
		{"cả hai nil", nil, nil, true},
		{"một nil", &a, nil, false},
		{"nil và một", nil, &a, false},
		{"cùng giá trị", &a, &a, true},
		{"khác giá trị", &a, &b, false},
	}

	for _, tc := range cases {
		if got := sameUUIDPtr(tc.x, tc.y); got != tc.want {
			t.Errorf("%s: = %v, muốn %v", tc.name, got, tc.want)
		}
	}
}

// TestStatusLabelCoversEveryColumn: nhãn thiếu làm thông báo lỗi chuyển
// trạng thái thành `Không chuyển trực tiếp từ "" sang ""`.
func TestStatusLabelCoversEveryColumn(t *testing.T) {
	for _, s := range domainproject.BoardColumns() {
		if statusLabel(s) == "" {
			t.Errorf("trạng thái %q không có nhãn tiếng Việt", s)
		}
	}
}
