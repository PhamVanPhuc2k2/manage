package project

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// Bước giữa hai task khi thêm vào cuối cột.
const sortOrderStep = 1000.0

// minSortGap là khoảng cách nhỏ nhất còn chèn được vào giữa.
//
// float64 có 52 bit phần định trị. Quanh giá trị ~1000, hai số cách nhau
// dưới khoảng 1e-9 sẽ cho trung bình cộng TRÙNG với một trong hai đầu, và
// task mới sẽ nằm chồng lên task cũ với thứ tự do database quyết định —
// nghĩa là ngẫu nhiên. Chạm ngưỡng này thì đánh số lại cả cột.
const minSortGap = 1e-6

type TaskInput struct {
	ProjectID     uuid.UUID
	ParentTaskID  *uuid.UUID
	Title         string
	Description   string
	Status        domainproject.TaskStatus
	Priority      domainproject.Priority
	AssigneeID    *uuid.UUID
	DueDate       *time.Time
	EstimateHours *float64
}

type ListTasksResult struct {
	Items      []*domainproject.Task
	Page       int
	PageSize   int
	TotalItems int
	TotalPages int
}

// Board là bảng Kanban của một dự án: task đã gom sẵn theo cột.
type Board struct {
	Project *domainproject.Project
	Columns map[domainproject.TaskStatus][]*domainproject.Task
}

func (u *Usecase) ListTasks(
	ctx context.Context,
	actor *domainauth.Actor,
	f domainproject.TaskFilter,
) (*ListTasksResult, error) {
	// Xoá sạch giá trị phạm vi có thể lọt vào từ query string.
	f.VisibleProjectIDs = nil
	f.RestrictProjects = false

	// Lọc theo một dự án cụ thể: kiểm tra quyền trên đúng dự án đó là đủ, và
	// rẻ hơn nhiều so với liệt kê mọi dự án actor tham gia.
	if f.ProjectID != nil {
		if _, err := u.loadAccess(ctx, actor, *f.ProjectID); err != nil {
			return nil, err
		}
	} else {
		ids, restrict, err := u.visibleProjectIDs(ctx, actor)
		if err != nil {
			return nil, err
		}
		f.VisibleProjectIDs, f.RestrictProjects = ids, restrict
	}

	f.Page, f.PageSize = normalizePage(f.Page, f.PageSize)

	items, total, err := u.tasks.List(ctx, f)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	totalPages := (total + f.PageSize - 1) / f.PageSize
	return &ListTasksResult{
		Items:      items,
		Page:       f.Page,
		PageSize:   f.PageSize,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

// MyTasks là việc được giao cho chính actor, xuyên mọi dự án.
func (u *Usecase) MyTasks(
	ctx context.Context,
	actor *domainauth.Actor,
	f domainproject.TaskFilter,
) (*ListTasksResult, error) {
	if actor == nil {
		return nil, apperror.New(apperror.KindUnauthorized, "Chưa xác thực")
	}
	id := actor.EmployeeID
	f.AssigneeID = &id
	// Task con cũng là việc phải làm, nên KHÔNG lọc OnlyRoots ở đây — khác
	// với bảng Kanban, nơi task con hiển thị lồng trong task cha.
	f.OnlyRoots = false
	return u.ListTasks(ctx, actor, f)
}

func (u *Usecase) GetBoard(
	ctx context.Context,
	actor *domainauth.Actor,
	projectID uuid.UUID,
) (*Board, error) {
	acc, err := u.loadAccess(ctx, actor, projectID)
	if err != nil {
		return nil, err
	}

	tasks, err := u.tasks.ListBoard(ctx, projectID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	// Khởi tạo đủ 4 cột kể cả cột rỗng: giao diện cần vẽ cột trống để người
	// dùng có chỗ thả task vào.
	columns := make(map[domainproject.TaskStatus][]*domainproject.Task, 4)
	for _, s := range domainproject.BoardColumns() {
		columns[s] = []*domainproject.Task{}
	}
	for _, t := range tasks {
		columns[t.Status] = append(columns[t.Status], t)
	}

	return &Board{Project: acc.project, Columns: columns}, nil
}

func (u *Usecase) GetTask(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) (*domainproject.Task, error) {
	t, err := u.tasks.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("công việc")
	}
	// Quyền nằm ở DỰ ÁN chứa task, không ở bản thân task. Bỏ bước này là
	// dính IDOR: ai biết id task là đọc được nội dung của mọi dự án.
	if _, err := u.loadAccess(ctx, actor, t.ProjectID); err != nil {
		return nil, apperror.NotFound("công việc")
	}
	return t, nil
}

func (u *Usecase) CreateTask(
	ctx context.Context,
	actor *domainauth.Actor,
	in TaskInput,
) (*domainproject.Task, error) {
	acc, err := u.loadAccess(ctx, actor, in.ProjectID)
	if err != nil {
		return nil, err
	}
	if !acc.canWrite() {
		return nil, apperror.Forbidden("Bạn chỉ có quyền xem dự án này")
	}
	if acc.project.Status.Closed() {
		return nil, apperror.Conflict("Dự án đã đóng, không thêm được công việc mới")
	}
	if err := u.validateTaskInput(ctx, in); err != nil {
		return nil, err
	}

	// Kiểm tra độ sâu lồng nhau. Quy tắc: tối đa 2 cấp.
	//
	// Chặn ở đây vì database không diễn đạt được ràng buộc này. Không chặn
	// thì cây task sâu tuỳ ý, và mọi chỗ hiển thị đệ quy sẽ phải tự lo việc
	// giới hạn độ sâu — tức là lo ở nhiều nơi thay vì một nơi.
	if in.ParentTaskID != nil {
		parent, err := u.tasks.GetByID(ctx, *in.ParentTaskID)
		if err != nil {
			return nil, apperror.Invalid("Công việc cha không tồn tại", nil)
		}
		if parent.ProjectID != in.ProjectID {
			return nil, apperror.Invalid("Công việc cha thuộc dự án khác", nil)
		}
		if parent.ParentTaskID != nil {
			return nil, apperror.Invalid(
				"Không lồng quá 2 cấp: công việc cha đã là công việc con của việc khác", nil)
		}
	}

	status := in.Status
	if status == "" {
		status = domainproject.TaskTodo
	}

	// Cấp số thứ tự trong dự án. Đây là thao tác ghi, nên nếu bước tạo task
	// ngay sau đó thất bại thì số này bị bỏ phí — chấp nhận được, mã task
	// không bắt buộc phải liên tục.
	seq, err := u.projects.NextTaskSeq(ctx, in.ProjectID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	maxOrder, err := u.tasks.MaxSortOrder(ctx, in.ProjectID, status)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	t := &domainproject.Task{
		ProjectID:     in.ProjectID,
		Seq:           seq,
		ParentTaskID:  in.ParentTaskID,
		Title:         strings.TrimSpace(in.Title),
		Description:   in.Description,
		Status:        status,
		Priority:      in.Priority,
		AssigneeID:    in.AssigneeID,
		ReporterID:    actor.EmployeeID,
		DueDate:       in.DueDate,
		EstimateHours: in.EstimateHours,
		SortOrder:     maxOrder + sortOrderStep,
	}
	if t.Priority == "" {
		t.Priority = domainproject.PriorityMedium
	}
	if status == domainproject.TaskInProgress {
		now := time.Now()
		t.StartedAt = &now
	}

	if err := u.tasks.Create(ctx, t); err != nil {
		return nil, apperror.Internal(err)
	}

	created, err := u.tasks.GetByID(ctx, t.ID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	u.logActivity(ctx, &domainproject.Activity{
		TaskID:  created.ID,
		ActorID: actorEmployeeID(actor),
		Action:  domainproject.ActionCreated,
	})
	u.notifyAssignment(ctx, actor, created, nil)

	return created, nil
}

func (u *Usecase) UpdateTask(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
	in TaskInput,
) (*domainproject.Task, error) {
	existing, err := u.tasks.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.NotFound("công việc")
	}
	acc, err := u.loadAccess(ctx, actor, existing.ProjectID)
	if err != nil {
		return nil, apperror.NotFound("công việc")
	}
	if !acc.canWrite() {
		return nil, apperror.Forbidden("Bạn chỉ có quyền xem dự án này")
	}
	if acc.project.Status.Closed() {
		return nil, apperror.Conflict("Dự án đã đóng, không sửa được công việc")
	}

	in.ProjectID = existing.ProjectID
	if err := u.validateTaskInput(ctx, in); err != nil {
		return nil, err
	}

	// Chuyển trạng thái phải hợp lệ. Đây là chỗ duy nhất kiểm tra luật này,
	// dùng chung với MoveTask qua domain.CanTransitionTo.
	newStatus := in.Status
	if newStatus == "" {
		newStatus = existing.Status
	}
	if !existing.Status.CanTransitionTo(newStatus) {
		return nil, apperror.Invalid(
			"Không chuyển trực tiếp từ \""+statusLabel(existing.Status)+
				"\" sang \""+statusLabel(newStatus)+"\". Đi lần lượt từng bước.", nil)
	}

	oldStatus := existing.Status
	oldAssignee := existing.AssigneeID

	existing.Title = strings.TrimSpace(in.Title)
	existing.Description = in.Description
	existing.Status = newStatus
	existing.Priority = in.Priority
	existing.AssigneeID = in.AssigneeID
	existing.DueDate = in.DueDate
	existing.EstimateHours = in.EstimateHours
	applyStatusTimestamps(existing, newStatus)

	if err := u.tasks.Update(ctx, existing); err != nil {
		return nil, apperror.Internal(err)
	}

	updated, err := u.tasks.GetByID(ctx, id)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if oldStatus != newStatus {
		u.recordStatusChange(ctx, actor, updated, oldStatus, newStatus)
	}
	if !sameUUIDPtr(oldAssignee, in.AssigneeID) {
		u.notifyAssignment(ctx, actor, updated, oldAssignee)
	}

	return updated, nil
}

// MoveTask là thao tác kéo-thả trên bảng Kanban: đổi cột và/hoặc đổi vị trí.
//
// afterTaskID là task mà task này sẽ nằm NGAY SAU. nil nghĩa là lên đầu cột.
// Dùng "nằm sau cái gì" thay vì "vị trí thứ mấy" vì chỉ số sẽ sai ngay khi
// có người khác chèn thêm task trong lúc người này đang kéo.
func (u *Usecase) MoveTask(
	ctx context.Context,
	actor *domainauth.Actor,
	taskID uuid.UUID,
	newStatus domainproject.TaskStatus,
	afterTaskID *uuid.UUID,
) (*domainproject.Task, error) {
	t, err := u.tasks.GetByID(ctx, taskID)
	if err != nil {
		return nil, apperror.NotFound("công việc")
	}
	acc, err := u.loadAccess(ctx, actor, t.ProjectID)
	if err != nil {
		return nil, apperror.NotFound("công việc")
	}
	if !acc.canWrite() {
		return nil, apperror.Forbidden("Bạn chỉ có quyền xem dự án này")
	}
	if acc.project.Status.Closed() {
		return nil, apperror.Conflict("Dự án đã đóng, không di chuyển được công việc")
	}
	if !newStatus.Valid() {
		return nil, apperror.Invalid("Trạng thái công việc không hợp lệ", nil)
	}
	if !t.Status.CanTransitionTo(newStatus) {
		return nil, apperror.Invalid(
			"Không chuyển trực tiếp từ \""+statusLabel(t.Status)+
				"\" sang \""+statusLabel(newStatus)+"\". Đi lần lượt từng bước.", nil)
	}

	order, err := u.computeSortOrder(ctx, t.ProjectID, newStatus, taskID, afterTaskID)
	if err != nil {
		return nil, err
	}

	if err := u.tasks.Reorder(ctx, taskID, newStatus, order); err != nil {
		return nil, apperror.Internal(err)
	}

	moved, err := u.tasks.GetByID(ctx, taskID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if t.Status != newStatus {
		u.recordStatusChange(ctx, actor, moved, t.Status, newStatus)
	} else {
		u.logActivity(ctx, &domainproject.Activity{
			TaskID:  taskID,
			ActorID: actorEmployeeID(actor),
			Action:  domainproject.ActionReordered,
		})
	}
	return moved, nil
}

// computeSortOrder tính vị trí mới cho task, chèn vào giữa hai hàng xóm.
//
// Khi khoảng hở đã quá hẹp để chèn, đánh số lại cả cột rồi tính lại. Việc
// đánh số lại hiếm khi xảy ra (cần hàng chục lần chèn vào đúng một chỗ) nên
// không tối ưu thêm.
func (u *Usecase) computeSortOrder(
	ctx context.Context,
	projectID uuid.UUID,
	status domainproject.TaskStatus,
	taskID uuid.UUID,
	afterTaskID *uuid.UUID,
) (float64, error) {
	// Thả lên đầu cột: lấy nửa khoảng từ 0 tới task đầu tiên.
	if afterTaskID == nil {
		first, err := u.firstSortOrder(ctx, projectID, status, taskID)
		if err != nil {
			return 0, err
		}
		if first == nil {
			return sortOrderStep, nil // cột rỗng
		}
		if *first <= minSortGap {
			if err := u.tasks.RenumberColumn(ctx, projectID, status); err != nil {
				return 0, apperror.Internal(err)
			}
			return sortOrderStep / 2, nil
		}
		return *first / 2, nil
	}

	if *afterTaskID == taskID {
		return 0, apperror.Invalid("Không thể thả công việc ngay sau chính nó", nil)
	}

	prev, err := u.tasks.SortOrderOf(ctx, *afterTaskID)
	if err != nil {
		return 0, apperror.Invalid("Công việc đứng trước không tồn tại", nil)
	}

	next, err := u.nextSortOrder(ctx, projectID, status, prev, taskID)
	if err != nil {
		return 0, err
	}
	if next == nil {
		return prev + sortOrderStep, nil // thả xuống cuối cột
	}

	if math.Abs(*next-prev) <= minSortGap {
		// Hết chỗ chèn. Đánh số lại rồi tính lại một lần — sau khi đánh số
		// bước là 1000 nên chắc chắn có chỗ.
		if err := u.tasks.RenumberColumn(ctx, projectID, status); err != nil {
			return 0, apperror.Internal(err)
		}
		prev, err = u.tasks.SortOrderOf(ctx, *afterTaskID)
		if err != nil {
			return 0, apperror.Internal(err)
		}
		next, err = u.nextSortOrder(ctx, projectID, status, prev, taskID)
		if err != nil {
			return 0, err
		}
		if next == nil {
			return prev + sortOrderStep, nil
		}
	}
	return (prev + *next) / 2, nil
}

// firstSortOrder trả về vị trí của task đầu tiên trong cột, bỏ qua chính
// task đang di chuyển. nil khi cột rỗng.
func (u *Usecase) firstSortOrder(
	ctx context.Context,
	projectID uuid.UUID,
	status domainproject.TaskStatus,
	excludeID uuid.UUID,
) (*float64, error) {
	list, err := u.tasks.ListBoard(ctx, projectID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	for _, t := range list {
		if t.Status == status && t.ID != excludeID {
			v := t.SortOrder
			return &v, nil
		}
	}
	return nil, nil
}

// nextSortOrder trả về vị trí của task đứng ngay sau mốc `after` trong cột.
// nil khi `after` đã là task cuối.
func (u *Usecase) nextSortOrder(
	ctx context.Context,
	projectID uuid.UUID,
	status domainproject.TaskStatus,
	after float64,
	excludeID uuid.UUID,
) (*float64, error) {
	list, err := u.tasks.ListBoard(ctx, projectID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	for _, t := range list {
		if t.Status == status && t.ID != excludeID && t.SortOrder > after {
			v := t.SortOrder
			return &v, nil
		}
	}
	return nil, nil
}

func (u *Usecase) DeleteTask(
	ctx context.Context,
	actor *domainauth.Actor,
	id uuid.UUID,
) error {
	t, err := u.tasks.GetByID(ctx, id)
	if err != nil {
		return apperror.NotFound("công việc")
	}
	acc, err := u.loadAccess(ctx, actor, t.ProjectID)
	if err != nil {
		return apperror.NotFound("công việc")
	}

	// Xoá task là thao tác của quản lý dự án, không phải của người thực hiện.
	// Người được giao việc mà tự xoá việc thì khối lượng công việc của cả dự
	// án biến mất khỏi báo cáo mà không ai hay.
	if !acc.canManage() && t.ReporterID != actor.EmployeeID {
		return apperror.Forbidden(
			"Chỉ chủ dự án hoặc người tạo công việc mới được xoá công việc này")
	}

	if err := u.tasks.SoftDelete(ctx, id); err != nil {
		return apperror.Internal(err)
	}
	u.logActivity(ctx, &domainproject.Activity{
		TaskID:  id,
		ActorID: actorEmployeeID(actor),
		Action:  domainproject.ActionDeleted,
	})
	return nil
}

func (u *Usecase) ListSubtasks(
	ctx context.Context,
	actor *domainauth.Actor,
	parentID uuid.UUID,
) ([]*domainproject.Task, error) {
	if _, err := u.GetTask(ctx, actor, parentID); err != nil {
		return nil, err
	}
	list, err := u.tasks.ListSubtasks(ctx, parentID)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

func (u *Usecase) ListActivities(
	ctx context.Context,
	actor *domainauth.Actor,
	taskID uuid.UUID,
) ([]*domainproject.Activity, error) {
	if _, err := u.GetTask(ctx, actor, taskID); err != nil {
		return nil, err
	}
	list, err := u.activities.ListByTask(ctx, taskID, maxActivityRows)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return list, nil
}

// --------------------------------------------------------------- tiện ích

func (u *Usecase) validateTaskInput(ctx context.Context, in TaskInput) error {
	if strings.TrimSpace(in.Title) == "" {
		return apperror.Invalid("Tiêu đề công việc không được để trống", nil)
	}
	if len(in.Title) > 500 {
		return apperror.Invalid("Tiêu đề công việc không quá 500 ký tự", nil)
	}
	if in.Status != "" && !in.Status.Valid() {
		return apperror.Invalid("Trạng thái công việc không hợp lệ", nil)
	}
	if in.Priority != "" && !in.Priority.Valid() {
		return apperror.Invalid("Độ ưu tiên không hợp lệ", nil)
	}
	if in.EstimateHours != nil && *in.EstimateHours < 0 {
		return apperror.Invalid("Số giờ ước lượng không được âm", nil)
	}

	if in.AssigneeID != nil {
		ok, err := u.employees.Exists(ctx, *in.AssigneeID)
		if err != nil {
			return apperror.Internal(err)
		}
		if !ok {
			return apperror.Invalid("Người thực hiện không tồn tại hoặc đã nghỉ việc", nil)
		}

		// Người thực hiện phải là thành viên dự án.
		//
		// Không chặn thì task rơi vào tay người không thấy dự án đó — họ nhận
		// thông báo về một việc mà mở ra chỉ thấy 404.
		if _, err := u.members.Get(ctx, in.ProjectID, *in.AssigneeID); err != nil {
			return apperror.Invalid(
				"Người thực hiện chưa phải thành viên dự án. Thêm họ vào dự án trước.", nil)
		}
	}
	return nil
}

// applyStatusTimestamps đặt started_at và completed_at theo trạng thái mới.
//
// started_at chỉ ghi LẦN ĐẦU: đưa task về todo rồi làm lại không được xoá
// mốc bắt đầu thật, nếu không thì mọi phép đo thời gian thực hiện sẽ sai.
// Riêng khi về lại todo thì xoá hẳn, vì lúc đó task coi như chưa bắt đầu.
func applyStatusTimestamps(t *domainproject.Task, newStatus domainproject.TaskStatus) {
	now := time.Now()
	switch newStatus {
	case domainproject.TaskTodo:
		t.StartedAt = nil
		t.CompletedAt = nil
	case domainproject.TaskDone:
		if t.StartedAt == nil {
			t.StartedAt = &now
		}
		if t.CompletedAt == nil {
			t.CompletedAt = &now
		}
	default:
		if t.StartedAt == nil {
			t.StartedAt = &now
		}
		t.CompletedAt = nil
	}
}

func (u *Usecase) recordStatusChange(
	ctx context.Context,
	actor *domainauth.Actor,
	t *domainproject.Task,
	from, to domainproject.TaskStatus,
) {
	u.logActivity(ctx, &domainproject.Activity{
		TaskID:   t.ID,
		ActorID:  actorEmployeeID(actor),
		Action:   domainproject.ActionStatusChanged,
		Field:    "status",
		OldValue: string(from),
		NewValue: string(to),
	})

	// Báo cho người thực hiện và người tạo task, trừ chính người vừa đổi.
	recipients := dedupeExcept(
		[]uuid.UUID{ptrOrNil(t.AssigneeID), t.ReporterID}, actorID(actor))

	u.publish(ctx, domainproject.Event{
		Name:       domainproject.JobTaskStatusChanged,
		TaskID:     t.ID,
		TaskCode:   t.Code(),
		TaskTitle:  t.Title,
		ProjectID:  t.ProjectID,
		ActorID:    actorID(actor),
		Recipients: recipients,
		OldValue:   string(from),
		NewValue:   string(to),
	})
}

// notifyAssignment ghi nhật ký và báo cho người vừa được giao việc.
func (u *Usecase) notifyAssignment(
	ctx context.Context,
	actor *domainauth.Actor,
	t *domainproject.Task,
	oldAssignee *uuid.UUID,
) {
	if t.AssigneeID == nil {
		if oldAssignee != nil {
			u.logActivity(ctx, &domainproject.Activity{
				TaskID:   t.ID,
				ActorID:  actorEmployeeID(actor),
				Action:   domainproject.ActionAssigned,
				Field:    "assignee",
				OldValue: oldAssignee.String(),
			})
		}
		return
	}

	oldVal := ""
	if oldAssignee != nil {
		oldVal = oldAssignee.String()
	}
	u.logActivity(ctx, &domainproject.Activity{
		TaskID:   t.ID,
		ActorID:  actorEmployeeID(actor),
		Action:   domainproject.ActionAssigned,
		Field:    "assignee",
		OldValue: oldVal,
		NewValue: t.AssigneeID.String(),
	})

	// Tự giao việc cho mình thì không cần thông báo cho chính mình.
	recipients := dedupeExcept([]uuid.UUID{*t.AssigneeID}, actorID(actor))

	u.publish(ctx, domainproject.Event{
		Name:       domainproject.JobTaskAssigned,
		TaskID:     t.ID,
		TaskCode:   t.Code(),
		TaskTitle:  t.Title,
		ProjectID:  t.ProjectID,
		ActorID:    actorID(actor),
		Recipients: recipients,
		NewValue:   t.AssigneeID.String(),
	})
}

func statusLabel(s domainproject.TaskStatus) string {
	return map[domainproject.TaskStatus]string{
		domainproject.TaskTodo:       "Cần làm",
		domainproject.TaskInProgress: "Đang làm",
		domainproject.TaskReview:     "Chờ duyệt",
		domainproject.TaskDone:       "Hoàn thành",
	}[s]
}

func actorID(actor *domainauth.Actor) uuid.UUID {
	if actor == nil {
		return uuid.Nil
	}
	return actor.EmployeeID
}

func ptrOrNil(p *uuid.UUID) uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return *p
}

// dedupeExcept lọc danh sách người nhận: bỏ id rỗng, bỏ trùng, bỏ chính
// người vừa gây ra sự kiện.
func dedupeExcept(ids []uuid.UUID, except uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil || id == except {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func sameUUIDPtr(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
