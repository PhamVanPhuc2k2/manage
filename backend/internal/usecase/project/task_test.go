package project

import (
	"context"
	"math"
	"net/http"
	"testing"

	"github.com/google/uuid"

	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// statusOf trả mã HTTP mà một lỗi nghiệp vụ sẽ biến thành.
//
// Kiểm tra mã HTTP chứ không kiểm loại lỗi cụ thể: điều quan trọng với người
// gọi API là họ nhận 404 hay 403, và đó cũng là thứ dễ hồi quy nhất.
//
// err nil trả 200. Phải xử lý riêng vì apperror.HTTPStatus(nil) cho 500, và
// khi đó một lời gọi THÀNH CÔNG lại trông y như lỗi máy chủ — bài kiểm thử sẽ
// đạt hoặc hỏng vì lý do hoàn toàn khác thứ nó định kiểm.
func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	code, _ := apperror.HTTPStatus(err)
	return code
}

// column dựng một cột Kanban với các vị trí cho trước.
func (h *projectHarness) column(
	projectID uuid.UUID,
	status domainproject.TaskStatus,
	orders ...float64,
) []*domainproject.Task {
	out := make([]*domainproject.Task, 0, len(orders))
	for i, o := range orders {
		out = append(out, h.tasks.add(&domainproject.Task{
			ProjectID: projectID,
			Seq:       i + 1,
			Title:     "Việc",
			Status:    status,
			SortOrder: o,
		}))
	}
	return out
}

// =========================================================================
// SẮP THỨ TỰ KANBAN
// =========================================================================

// TestMoveTaskToEmptyColumn: cột rỗng thì lấy đúng một bước.
func TestMoveTaskToEmptyColumn(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	task := h.column(p.ID, domainproject.TaskTodo, sortOrderStep)[0]

	moved, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskInProgress, nil)
	if err != nil {
		t.Fatalf("MoveTask lỗi: %v", err)
	}

	if moved.Status != domainproject.TaskInProgress {
		t.Errorf("trạng thái = %q, muốn %q", moved.Status, domainproject.TaskInProgress)
	}
	if moved.SortOrder != sortOrderStep {
		t.Errorf("vị trí = %v, muốn %v", moved.SortOrder, sortOrderStep)
	}
}

// TestMoveTaskToTopOfColumn: thả lên đầu cột thì lấy nửa khoảng từ 0 tới task
// đầu tiên. Nhờ vậy chèn lên đầu không phải cập nhật lại mọi dòng phía sau.
func TestMoveTaskToTopOfColumn(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	// Cột đích đã có hai task ở 1000 và 2000.
	h.column(p.ID, domainproject.TaskInProgress, 1000, 2000)
	task := h.column(p.ID, domainproject.TaskTodo, 500)[0]

	moved, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskInProgress, nil)
	if err != nil {
		t.Fatalf("MoveTask lỗi: %v", err)
	}

	if moved.SortOrder != 500 {
		t.Errorf("vị trí = %v, muốn 500 (nửa của 1000)", moved.SortOrder)
	}
	if h.tasks.renumbered != 0 {
		t.Error("không cần đánh số lại khi còn chỗ chèn")
	}
}

// TestMoveTaskBetweenTwo: thả vào giữa thì lấy trung bình hai vị trí.
func TestMoveTaskBetweenTwo(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	col := h.column(p.ID, domainproject.TaskInProgress, 1000, 2000)
	task := h.column(p.ID, domainproject.TaskTodo, 500)[0]

	moved, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskInProgress, &col[0].ID)
	if err != nil {
		t.Fatalf("MoveTask lỗi: %v", err)
	}

	if moved.SortOrder != 1500 {
		t.Errorf("vị trí = %v, muốn 1500 (trung bình 1000 và 2000)", moved.SortOrder)
	}
}

// TestMoveTaskToBottom: thả sau task cuối thì cộng thêm một bước.
func TestMoveTaskToBottom(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	col := h.column(p.ID, domainproject.TaskInProgress, 1000, 2000)
	task := h.column(p.ID, domainproject.TaskTodo, 500)[0]

	moved, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskInProgress, &col[1].ID)
	if err != nil {
		t.Fatalf("MoveTask lỗi: %v", err)
	}

	if moved.SortOrder != 2000+sortOrderStep {
		t.Errorf("vị trí = %v, muốn %v", moved.SortOrder, 2000+sortOrderStep)
	}
}

// TestMoveTaskRenumbersWhenNoGapLeft là phép thử quan trọng nhất của thuật
// toán sắp thứ tự phân số.
//
// Chèn vào giữa liên tục làm khoảng cách giảm một nửa mỗi lần. Đến lúc nó nhỏ
// hơn minSortGap thì float64 không còn phân biệt được hai vị trí, và mọi lần
// chèn tiếp theo sẽ cho ra ĐÚNG một trong hai giá trị đã có — thứ tự bảng
// Kanban trở nên ngẫu nhiên. Đánh số lại là cách thoát duy nhất.
func TestMoveTaskRenumbersWhenNoGapLeft(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	// Hai task sát nhau tới mức không chèn được nữa.
	col := h.column(p.ID, domainproject.TaskInProgress, 1000, 1000+minSortGap/2)
	task := h.column(p.ID, domainproject.TaskTodo, 500)[0]

	moved, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskInProgress, &col[0].ID)
	if err != nil {
		t.Fatalf("MoveTask lỗi: %v", err)
	}

	if h.tasks.renumbered != 1 {
		t.Fatalf("gọi đánh số lại %d lần, muốn 1", h.tasks.renumbered)
	}

	// Sau khi đánh số lại, vị trí mới phải nằm GIỮA hai task kia một cách
	// phân biệt được.
	board, _ := h.tasks.ListBoard(context.Background(), p.ID)
	var inProgress []*domainproject.Task
	for _, b := range board {
		if b.Status == domainproject.TaskInProgress {
			inProgress = append(inProgress, b)
		}
	}
	if len(inProgress) != 3 {
		t.Fatalf("cột có %d task, muốn 3", len(inProgress))
	}
	for i := 1; i < len(inProgress); i++ {
		gap := inProgress[i].SortOrder - inProgress[i-1].SortOrder
		if gap <= minSortGap {
			t.Errorf("sau khi đánh số lại vẫn còn khoảng cách %v <= %v", gap, minSortGap)
		}
	}
	if moved.SortOrder == col[0].SortOrder {
		t.Error("task vừa di chuyển trùng vị trí với task đứng trước")
	}
}

// TestMoveTaskRenumbersWhenTopTooSmall: thả lên đầu cột mà task đầu tiên đã ở
// một vị trí quá nhỏ thì cũng phải đánh số lại — chia đôi tiếp là hết chỗ.
func TestMoveTaskRenumbersWhenTopTooSmall(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	h.column(p.ID, domainproject.TaskInProgress, minSortGap/2)
	task := h.column(p.ID, domainproject.TaskTodo, 500)[0]

	moved, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskInProgress, nil)
	if err != nil {
		t.Fatalf("MoveTask lỗi: %v", err)
	}
	if h.tasks.renumbered != 1 {
		t.Errorf("gọi đánh số lại %d lần, muốn 1", h.tasks.renumbered)
	}
	if moved.SortOrder <= 0 {
		t.Errorf("vị trí = %v, phải lớn hơn 0", moved.SortOrder)
	}
}

// TestMoveTaskRejectsDropAfterItself: thả một task ngay sau chính nó là thao
// tác vô nghĩa, và nếu không chặn thì computeSortOrder sẽ tính vị trí dựa trên
// chính nó — cho ra kết quả không xác định.
func TestMoveTaskRejectsDropAfterItself(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

	_, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskTodo, &task.ID)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestMoveTaskExcludesItselfFromNeighbourSearch: khi di chuyển trong CÙNG một
// cột, task đang di chuyển phải bị loại khỏi phép tìm láng giềng.
//
// Không loại thì nó tự làm láng giềng của chính mình và vị trí mới tính ra
// bằng vị trí cũ — kéo-thả trông như không có tác dụng gì.
func TestMoveTaskExcludesItselfFromNeighbourSearch(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	// Ba task trong cùng một cột; di chuyển task ĐẦU xuống giữa.
	col := h.column(p.ID, domainproject.TaskTodo, 1000, 2000, 3000)

	moved, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), col[0].ID,
		domainproject.TaskTodo, &col[1].ID)
	if err != nil {
		t.Fatalf("MoveTask lỗi: %v", err)
	}

	// Phải nằm giữa 2000 và 3000, không phải giữa 1000 và gì cả.
	if moved.SortOrder != 2500 {
		t.Errorf("vị trí = %v, muốn 2500 (giữa 2000 và 3000)", moved.SortOrder)
	}
}

func TestMoveTaskRejectsUnknownAfterTask(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]
	ghost := uuid.New()

	_, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskInProgress, &ghost)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestSortOrderStaysFiniteAfterManyInserts là phép thử theo TÍNH CHẤT.
//
// Chèn 60 lần vào cùng một chỗ. Nếu thuật toán không đánh số lại thì khoảng
// cách giảm một nửa mỗi lần và sau khoảng 50 lần float64 hết độ phân giải:
// hai vị trí trở thành bằng nhau và thứ tự bảng Kanban thành ngẫu nhiên.
//
// Phép thử này bắt được lỗi mà các phép thử giá trị cụ thể ở trên không bắt
// được, vì nó chỉ lộ ra sau nhiều lần lặp.
func TestSortOrderStaysFiniteAfterManyInserts(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	anchor := h.column(p.ID, domainproject.TaskTodo, 1000, 2000)

	for i := 0; i < 60; i++ {
		// Tạo SẴN ở cột đích: phép thử này đo thuật toán sắp thứ tự, không đo
		// luật chuyển trạng thái. Tạo ở cột khác rồi chuyển sang sẽ vướng luật
		// "đi lần lượt từng bước" và bài kiểm thử hỏng vì lý do không liên quan.
		fresh := h.tasks.add(&domainproject.Task{
			ProjectID: p.ID,
			Seq:       100 + i,
			Title:     "Việc chèn",
			Status:    domainproject.TaskTodo,
			SortOrder: 10000 + float64(i),
		})

		moved, err := h.uc.MoveTask(
			context.Background(), actorIn(owner), fresh.ID,
			domainproject.TaskTodo, &anchor[0].ID)
		if err != nil {
			t.Fatalf("lần chèn %d lỗi: %v", i, err)
		}
		if math.IsNaN(moved.SortOrder) || math.IsInf(moved.SortOrder, 0) {
			t.Fatalf("lần chèn %d cho vị trí không hợp lệ: %v", i, moved.SortOrder)
		}
	}

	// Mọi vị trí trong cột phải PHÂN BIỆT được với nhau.
	board, _ := h.tasks.ListBoard(context.Background(), p.ID)
	seen := map[float64]bool{}
	for _, b := range board {
		if b.Status != domainproject.TaskTodo {
			continue
		}
		if seen[b.SortOrder] {
			t.Fatalf("hai task trùng vị trí %v — thứ tự bảng Kanban đã thành ngẫu nhiên",
				b.SortOrder)
		}
		seen[b.SortOrder] = true
	}

	if h.tasks.renumbered == 0 {
		t.Error("chèn 60 lần mà không đánh số lại lần nào — thuật toán đang dựa vào may mắn")
	}
	t.Logf("đã đánh số lại %d lần trong 60 lần chèn", h.tasks.renumbered)
}

// =========================================================================
// CHUYỂN TRẠNG THÁI
// =========================================================================

// TestMoveTaskRejectsSkippingStatus: luồng trạng thái phải đi lần lượt.
//
// Cho nhảy từ "Cần làm" thẳng sang "Hoàn thành" thì cột "Chờ duyệt" mất ý
// nghĩa, và không ai biết một việc đã được ai xem chưa.
func TestMoveTaskRejectsSkippingStatus(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

	_, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskDone, nil)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("nhảy cột: mã lỗi = %d, muốn 400", got)
	}
}

func TestMoveTaskRejectsInvalidStatus(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

	_, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskStatus("khong_co_that"), nil)
	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestMoveTaskWithinSameColumnLogsReorder: di chuyển trong cùng cột là "sắp
// lại", không phải "đổi trạng thái". Ghi sai loại nhật ký sẽ làm dòng thời
// gian của công việc đầy những lần "đổi trạng thái" giả.
func TestMoveTaskWithinSameColumnLogsReorder(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)

	col := h.column(p.ID, domainproject.TaskTodo, 1000, 2000)

	if _, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), col[0].ID,
		domainproject.TaskTodo, &col[1].ID); err != nil {
		t.Fatalf("MoveTask lỗi: %v", err)
	}

	if len(h.activities.logged) != 1 {
		t.Fatalf("ghi %d dòng nhật ký, muốn 1", len(h.activities.logged))
	}
	if got := h.activities.logged[0].Action; got != domainproject.ActionReordered {
		t.Errorf("loại nhật ký = %q, muốn %q", got, domainproject.ActionReordered)
	}

	// Và KHÔNG phát sự kiện đổi trạng thái.
	for _, e := range h.events.published {
		if e.Name == domainproject.JobTaskStatusChanged {
			t.Error("sắp lại trong cùng cột không được phát sự kiện đổi trạng thái")
		}
	}
}

func TestMoveTaskAcrossColumnsPublishesEvent(t *testing.T) {
	h := newProjectHarness()
	owner, assignee := uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.members.join(p.ID, assignee, domainproject.RoleMember)
	h.employees.names[owner] = "Trần Văn Chủ"

	task := h.tasks.add(&domainproject.Task{
		ProjectID:  p.ID,
		Seq:        1,
		Title:      "Việc có người nhận",
		Status:     domainproject.TaskTodo,
		SortOrder:  1000,
		AssigneeID: &assignee,
	})

	if _, err := h.uc.MoveTask(
		context.Background(), actorIn(owner), task.ID,
		domainproject.TaskInProgress, nil); err != nil {
		t.Fatalf("MoveTask lỗi: %v", err)
	}

	found := false
	for _, e := range h.events.published {
		if e.Name == domainproject.JobTaskStatusChanged {
			found = true
			if len(e.Recipients) == 0 {
				t.Error("sự kiện đổi trạng thái không có người nhận nào")
			}
			// Tên người thực hiện phải được điền — module thông báo dựng câu
			// tiêu đề từ nó, và thiếu thì thành "Một thành viên đã...".
			if e.ActorName == "" {
				t.Error("sự kiện thiếu tên người thực hiện")
			}
		}
	}
	if !found {
		t.Error("không phát sự kiện đổi trạng thái")
	}
}

// TestMoveTaskRejectedOnClosedProject: dự án đã đóng thì không di chuyển được
// công việc. Cho sửa sẽ làm báo cáo của một dự án đã kết thúc đổi số sau khi
// đã chốt.
func TestMoveTaskRejectedOnClosedProject(t *testing.T) {
	for _, status := range []domainproject.Status{
		domainproject.StatusCompleted, domainproject.StatusCancelled,
	} {
		t.Run(string(status), func(t *testing.T) {
			h := newProjectHarness()
			owner := uuid.New()
			p := h.seedProject(owner)
			p.Status = status
			h.projects.put(p)

			task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

			_, err := h.uc.MoveTask(
				context.Background(), actorIn(owner), task.ID,
				domainproject.TaskInProgress, nil)
			if got := statusOf(err); got != http.StatusConflict {
				t.Errorf("mã lỗi = %d, muốn 409", got)
			}
		})
	}
}

// =========================================================================
// KIỂM SOÁT TRUY CẬP
// =========================================================================

// TestNonMemberGets404 khoá lại quyết định chống IDOR của module dự án.
//
// Trả 404 chứ không 403: 403 xác nhận rằng dự án đó CÓ TỒN TẠI, và với người
// ngoài thì đó đã là rò rỉ thông tin.
func TestNonMemberGets404(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)
	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

	outsider := actorIn(uuid.New())

	t.Run("đọc dự án", func(t *testing.T) {
		_, err := h.uc.GetProject(context.Background(), outsider, p.ID)
		if got := statusOf(err); got != http.StatusNotFound {
			t.Errorf("mã lỗi = %d, muốn 404", got)
		}
	})

	t.Run("đọc công việc", func(t *testing.T) {
		_, err := h.uc.GetTask(context.Background(), outsider, task.ID)
		if got := statusOf(err); got != http.StatusNotFound {
			t.Errorf("mã lỗi = %d, muốn 404", got)
		}
	})

	t.Run("di chuyển công việc", func(t *testing.T) {
		_, err := h.uc.MoveTask(context.Background(), outsider, task.ID,
			domainproject.TaskInProgress, nil)
		if got := statusOf(err); got != http.StatusNotFound {
			t.Errorf("mã lỗi = %d, muốn 404", got)
		}
	})

	t.Run("xoá công việc", func(t *testing.T) {
		err := h.uc.DeleteTask(context.Background(), outsider, task.ID)
		if got := statusOf(err); got != http.StatusNotFound {
			t.Errorf("mã lỗi = %d, muốn 404", got)
		}
	})
}

// TestViewerCannotWrite: vai trò "viewer" chỉ được xem. Đây là 403, không phải
// 404 — họ ĐƯỢC biết dự án tồn tại, chỉ không được sửa.
func TestViewerCannotWrite(t *testing.T) {
	h := newProjectHarness()
	owner, viewer := uuid.New(), uuid.New()
	p := h.seedProject(owner)
	h.members.join(p.ID, viewer, domainproject.RoleViewer)

	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

	// Xem thì được.
	if _, err := h.uc.GetTask(context.Background(), actorIn(viewer), task.ID); err != nil {
		t.Errorf("viewer phải đọc được công việc: %v", err)
	}

	// Sửa thì không.
	_, err := h.uc.MoveTask(context.Background(), actorIn(viewer), task.ID,
		domainproject.TaskInProgress, nil)
	if got := statusOf(err); got != http.StatusForbidden {
		t.Errorf("mã lỗi = %d, muốn 403", got)
	}
}

// TestCompanyWideScopeSeesEverything: người có phạm vi toàn công ty (giám đốc)
// xem được mọi dự án mà không phải là thành viên dự án nào.
func TestCompanyWideScopeSeesEverything(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()
	p := h.seedProject(owner)
	task := h.column(p.ID, domainproject.TaskTodo, 1000)[0]

	director := actorCompanyWide(uuid.New())

	if _, err := h.uc.GetProject(context.Background(), director, p.ID); err != nil {
		t.Errorf("phạm vi toàn công ty phải xem được dự án: %v", err)
	}
	if _, err := h.uc.GetTask(context.Background(), director, task.ID); err != nil {
		t.Errorf("phạm vi toàn công ty phải xem được công việc: %v", err)
	}
	if _, err := h.uc.MoveTask(context.Background(), director, task.ID,
		domainproject.TaskInProgress, nil); err != nil {
		t.Errorf("phạm vi toàn công ty phải sửa được công việc: %v", err)
	}
}

// TestOwnerWithoutMemberRowStillHasOwnerRole: chủ dự án luôn có quyền owner
// kể cả khi dòng project_members bị thiếu vì dữ liệu sửa tay.
//
// Không có nhánh này thì một lần sửa dữ liệu trực tiếp sẽ khoá chính chủ dự án
// ra khỏi dự án của họ, và không ai gỡ được vì việc gỡ cũng cần quyền.
func TestOwnerWithoutMemberRowStillHasOwnerRole(t *testing.T) {
	h := newProjectHarness()
	owner := uuid.New()

	p := &domainproject.Project{
		ID:        uuid.New(),
		CompanyID: h.company.id,
		Code:      "NOMEM",
		Name:      "Dự án thiếu dòng thành viên",
		OwnerID:   owner,
		Status:    domainproject.StatusActive,
	}
	h.projects.put(p) // CỐ Ý không gọi members.join

	got, err := h.uc.GetProject(context.Background(), actorIn(owner), p.ID)
	if err != nil {
		t.Fatalf("chủ dự án phải xem được dự án của mình: %v", err)
	}
	if got.ViewerRole != domainproject.RoleOwner {
		t.Errorf("vai trò = %q, muốn %q", got.ViewerRole, domainproject.RoleOwner)
	}
}
