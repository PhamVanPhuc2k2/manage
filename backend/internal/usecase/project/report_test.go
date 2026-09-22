package project

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
)

// Bộ kiểm thử ghi nhận thời gian và báo cáo.
//
// Ghi nhận thời gian là dữ liệu đầu vào của mọi báo cáo khối lượng công
// việc về sau, nên một con số sai ở đây lan ra khắp nơi mà không có chỗ nào
// báo lỗi.

// =========================================================================
// GHI NHẬN THỜI GIAN
// =========================================================================

func TestLogTime(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskInProgress,
	})

	got, err := h.uc.LogTime(context.Background(), actorIn(owner), task.ID,
		TimelogInput{SpentMinutes: 90, Note: "viết tài liệu"})
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.SpentMinutes != 90 {
		t.Errorf("số phút = %d, muốn 90", got.SpentMinutes)
	}
	if got.EmployeeID != owner {
		t.Errorf("người ghi = %v, muốn %v", got.EmployeeID, owner)
	}
	if !slices.Contains(h.activities.actions(task.ID), domainproject.ActionTimeLogged) {
		t.Errorf("nhật ký = %v, phải có %q",
			h.activities.actions(task.ID), domainproject.ActionTimeLogged)
	}
}

// TestLogTimeRejectsAbsurdValues.
//
// Chặn trên 24 giờ không phải để soi nhân viên mà để bắt lỗi nhập: gõ nhầm
// 480 thành 4800 phút là 80 giờ, và con số đó âm thầm làm hỏng mọi báo cáo
// sau này.
func TestLogTimeRejectsAbsurdValues(t *testing.T) {
	owner := uuid.New()
	future := time.Now().Add(48 * time.Hour)

	cases := []struct {
		name string
		in   TimelogInput
	}{
		{"bằng 0", TimelogInput{SpentMinutes: 0}},
		{"âm", TimelogInput{SpentMinutes: -30}},
		{"quá 24 giờ", TimelogInput{SpentMinutes: 24*60 + 1}},
		{"ngày trong tương lai", TimelogInput{SpentMinutes: 60, LoggedOn: future}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newProjectHarness()
			p := h.seedProject(owner)
			task := h.tasks.add(&domainproject.Task{
				ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
			})

			_, err := h.uc.LogTime(
				context.Background(), actorIn(owner), task.ID, tc.in)

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
			if len(h.timelogs.created) != 0 {
				t.Error("đã ghi dù đầu vào không hợp lệ")
			}
		})
	}
}

func TestLogTimeDefaultsToToday(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
	})

	got, err := h.uc.LogTime(context.Background(), actorIn(owner), task.ID,
		TimelogInput{SpentMinutes: 60})
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.LoggedOn.IsZero() {
		t.Error("ngày ghi nhận để trống — phải mặc định là hôm nay")
	}
}

func TestViewerCannotLogTime(t *testing.T) {
	owner := uuid.New()
	viewer := uuid.New()

	h := newProjectHarness()
	p := h.seedProject(owner)
	h.members.join(p.ID, viewer, domainproject.RoleViewer)
	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
	})

	_, err := h.uc.LogTime(context.Background(), actorIn(viewer), task.ID,
		TimelogInput{SpentMinutes: 60})

	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
}

func TestLogTimeOnTaskYouCannotSeeReturns404(t *testing.T) {
	h := newProjectHarness()
	p := h.seedProject(uuid.New())
	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
	})

	_, err := h.uc.LogTime(context.Background(), actorIn(uuid.New()), task.ID,
		TimelogInput{SpentMinutes: 60})

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestListTimelogs(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	task := h.tasks.add(&domainproject.Task{
		ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
	})
	h.timelogs.add(&domainproject.Timelog{
		TaskID: task.ID, EmployeeID: owner, SpentMinutes: 60,
	})

	got, err := h.uc.ListTimelogs(context.Background(), actorIn(owner), task.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("số bản ghi = %d, muốn 1", len(got))
	}
}

// TestOnlyAuthorOrOwnerCanDeleteTimelog: thành viên khác không được xoá giờ
// công của người ta.
func TestOnlyAuthorOrOwnerCanDeleteTimelog(t *testing.T) {
	owner := uuid.New()
	author := uuid.New()
	other := uuid.New()

	setup := func() (*projectHarness, uuid.UUID) {
		h := newProjectHarness()
		p := h.seedProject(owner)
		h.members.join(p.ID, author, domainproject.RoleMember)
		h.members.join(p.ID, other, domainproject.RoleMember)

		task := h.tasks.add(&domainproject.Task{
			ProjectID: p.ID, Title: "Việc", Status: domainproject.TaskTodo,
		})
		tl := h.timelogs.add(&domainproject.Timelog{
			TaskID: task.ID, EmployeeID: author, SpentMinutes: 60,
		})
		return h, tl.ID
	}

	t.Run("người ghi xoá được", func(t *testing.T) {
		h, id := setup()
		if err := h.uc.DeleteTimelog(
			context.Background(), actorIn(author), id,
		); err != nil {
			t.Fatalf("lỗi không mong đợi: %v", err)
		}
	})

	t.Run("chủ dự án xoá được", func(t *testing.T) {
		h, id := setup()
		if err := h.uc.DeleteTimelog(
			context.Background(), actorIn(owner), id,
		); err != nil {
			t.Fatalf("lỗi không mong đợi: %v", err)
		}
	})

	t.Run("thành viên khác không xoá được", func(t *testing.T) {
		h, id := setup()
		err := h.uc.DeleteTimelog(context.Background(), actorIn(other), id)

		if got := statusOf(err); got != http.StatusForbidden {
			t.Errorf("mã lỗi = %d, muốn 403", got)
		}
		if len(h.timelogs.deleted) != 0 {
			t.Error("đã xoá giờ công của người khác")
		}
	})
}

func TestDeleteMissingTimelogReturns404(t *testing.T) {
	h := newProjectHarness()

	err := h.uc.DeleteTimelog(
		context.Background(), actorIn(uuid.New()), uuid.New())

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// BÁO CÁO
// =========================================================================

func TestProjectProgress(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)
	h.reports.progress = &domainproject.ProjectProgress{
		TotalTasks: 10,
		ByStatus:   map[domainproject.TaskStatus]int{domainproject.TaskDone: 4},
	}

	got, err := h.uc.ProjectProgress(context.Background(), actorIn(owner), p.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.TotalTasks != 10 {
		t.Errorf("tổng số việc = %d, muốn 10", got.TotalTasks)
	}
	if got.Progress() != 40 {
		t.Errorf("phần trăm hoàn thành = %d, muốn 40", got.Progress())
	}
}

func TestProjectProgressOfProjectYouCannotSeeReturns404(t *testing.T) {
	h := newProjectHarness()
	p := h.seedProject(uuid.New())

	_, err := h.uc.ProjectProgress(
		context.Background(), actorIn(uuid.New()), p.ID)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestOverdueTasksExcludesFinishedWork.
//
// "Quá hạn" là danh sách cần xem mỗi sáng. Việc đã hoàn thành muộn không
// thuộc về đó — để lại thì danh sách chỉ dài thêm mà không ai hành động
// được, và rồi không ai mở nó nữa.
func TestOverdueTasksExcludesFinishedWork(t *testing.T) {
	member := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(uuid.New())
	h.members.join(p.ID, member, domainproject.RoleMember)
	h.projects.memberOf = []uuid.UUID{p.ID}

	if _, err := h.uc.OverdueTasks(
		context.Background(), actorIn(member), nil, 1, 20,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	f := h.tasks.lastFilter
	if !f.Unfinished {
		t.Error("bộ lọc phải loại bỏ việc đã hoàn thành")
	}
	if f.DueBefore == nil {
		t.Error("bộ lọc phải có mốc hạn chót")
	}
	if f.SortBy != "due_date" {
		t.Errorf("sắp xếp theo %q, muốn due_date — việc trễ nhất lên đầu", f.SortBy)
	}
}

// TestWorkloadFollowsProjectScope.
//
// Không giới hạn ở đây thì bất kỳ ai cũng đọc được bức tranh nhân sự của cả
// công ty: ai đang gánh bao nhiêu việc, ai đang rảnh.
func TestWorkloadFollowsProjectScope(t *testing.T) {
	member := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(uuid.New())
	h.members.join(p.ID, member, domainproject.RoleMember)
	h.projects.memberOf = []uuid.UUID{p.ID}

	if _, err := h.uc.Workload(
		context.Background(), actorIn(member), nil,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !h.reports.lastRestrict {
		t.Error("phải giới hạn phạm vi cho người không có quyền toàn công ty")
	}
	if len(h.reports.lastIDs) != 1 || h.reports.lastIDs[0] != p.ID {
		t.Errorf("phạm vi = %v, muốn [%v]", h.reports.lastIDs, p.ID)
	}
}

func TestWorkloadOfSingleProject(t *testing.T) {
	owner := uuid.New()
	h := newProjectHarness()
	p := h.seedProject(owner)

	if _, err := h.uc.Workload(
		context.Background(), actorIn(owner), &p.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !h.reports.lastRestrict || len(h.reports.lastIDs) != 1 ||
		h.reports.lastIDs[0] != p.ID {
		t.Errorf("phạm vi = %v (giới hạn %v), muốn chỉ [%v]",
			h.reports.lastIDs, h.reports.lastRestrict, p.ID)
	}
}

func TestWorkloadOfProjectYouCannotSeeReturns404(t *testing.T) {
	h := newProjectHarness()
	p := h.seedProject(uuid.New())

	_, err := h.uc.Workload(context.Background(), actorIn(uuid.New()), &p.ID)
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

func TestWorkloadCompanyWideIsUnrestricted(t *testing.T) {
	h := newProjectHarness()

	if _, err := h.uc.Workload(
		context.Background(), actorCompanyWide(uuid.New()), nil,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if h.reports.lastRestrict {
		t.Error("phạm vi toàn công ty không được bị giới hạn")
	}
}

// =========================================================================
// ĐỊNH DẠNG
// =========================================================================

// TestMinutesLabel: nhãn này đi thẳng vào dòng nhật ký mà người dùng đọc,
// nên "90 phút" phải hiện thành "1 giờ 30 phút".
func TestMinutesLabel(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0 phút"},
		{1, "1 phút"},
		{45, "45 phút"},
		{60, "1 giờ"},
		{90, "1 giờ 30 phút"},
		{120, "2 giờ"},
		{485, "8 giờ 5 phút"},
	}

	for _, tc := range cases {
		if got := minutesLabel(tc.in); got != tc.want {
			t.Errorf("minutesLabel(%d) = %q, muốn %q", tc.in, got, tc.want)
		}
	}
}

func TestProjectItoa(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{5, "5"},
		{10, "10"},
		{-42, "-42"},
		{987654321, "987654321"},
	}

	for _, tc := range cases {
		if got := itoa(tc.in); got != tc.want {
			t.Errorf("itoa(%d) = %q, muốn %q", tc.in, got, tc.want)
		}
	}
}
