package handler

import (
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
	ucproject "github.com/PhamVanPhuc2k2/manage/internal/usecase/project"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type TaskHandler struct {
	uc *ucproject.Usecase
}

func NewTaskHandler(uc *ucproject.Usecase) *TaskHandler {
	return &TaskHandler{uc: uc}
}

type taskDTO struct {
	ID           string  `json:"id"`
	Code         string  `json:"code"`
	Seq          int     `json:"seq"`
	ProjectID    string  `json:"project_id"`
	ProjectCode  string  `json:"project_code,omitempty"`
	ProjectName  string  `json:"project_name,omitempty"`
	ParentTaskID *string `json:"parent_task_id,omitempty"`

	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	Status        string     `json:"status"`
	Priority      string     `json:"priority"`
	AssigneeID    *string    `json:"assignee_id,omitempty"`
	AssigneeName  string     `json:"assignee_name,omitempty"`
	ReporterID    string     `json:"reporter_id"`
	ReporterName  string     `json:"reporter_name,omitempty"`
	DueDate       *time.Time `json:"due_date,omitempty"`
	EstimateHours *float64   `json:"estimate_hours,omitempty"`
	SortOrder     float64    `json:"sort_order"`

	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Overdue     bool       `json:"overdue"`

	CommentCount    int `json:"comment_count"`
	AttachmentCount int `json:"attachment_count"`
	SubtaskCount    int `json:"subtask_count"`
	DoneSubtasks    int `json:"done_subtasks"`
	SpentMinutes    int `json:"spent_minutes"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toTaskDTO(t *domainproject.Task) taskDTO {
	return taskDTO{
		ID:           t.ID.String(),
		Code:         t.Code(),
		Seq:          t.Seq,
		ProjectID:    t.ProjectID.String(),
		ProjectCode:  t.ProjectCode,
		ProjectName:  t.ProjectName,
		ParentTaskID: uuidPtrToString(t.ParentTaskID),

		Title:         t.Title,
		Description:   t.Description,
		Status:        string(t.Status),
		Priority:      string(t.Priority),
		AssigneeID:    uuidPtrToString(t.AssigneeID),
		AssigneeName:  t.AssigneeName,
		ReporterID:    t.ReporterID.String(),
		ReporterName:  t.ReporterName,
		DueDate:       t.DueDate,
		EstimateHours: t.EstimateHours,
		SortOrder:     t.SortOrder,

		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
		Overdue:     t.Overdue(time.Now()),

		CommentCount:    t.CommentCount,
		AttachmentCount: t.AttachmentCount,
		SubtaskCount:    t.SubtaskCount,
		DoneSubtasks:    t.DoneSubtasks,
		SpentMinutes:    t.SpentMinutes,

		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

func toTaskDTOs(list []*domainproject.Task) []taskDTO {
	out := make([]taskDTO, 0, len(list))
	for _, t := range list {
		out = append(out, toTaskDTO(t))
	}
	return out
}

// buildTaskFilter đọc bộ lọc từ query string.
//
// KHÔNG đọc VisibleProjectIDs hay RestrictProjects — hai trường đó do usecase
// đặt theo phạm vi của actor. Cho client đặt là mở toang phân quyền.
func buildTaskFilter(r *http.Request) (domainproject.TaskFilter, error) {
	q := r.URL.Query()

	f := domainproject.TaskFilter{
		Search:    q.Get("search"),
		OnlyRoots: q.Get("only_roots") == "true",
		Page:      atoiDefault(q.Get("page"), 1),
		PageSize:  atoiDefault(q.Get("page_size"), 20),
		SortBy:    q.Get("sort_by"),
		SortDesc:  q.Get("sort_order") == "desc",
	}

	if v := q.Get("project_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return f, apperror.Invalid("project_id không hợp lệ", nil)
		}
		f.ProjectID = &id
	}
	if v := q.Get("status"); v != "" {
		s := domainproject.TaskStatus(v)
		if !s.Valid() {
			return f, apperror.Invalid("status không hợp lệ", nil)
		}
		f.Status = &s
	}
	if v := q.Get("priority"); v != "" {
		p := domainproject.Priority(v)
		if !p.Valid() {
			return f, apperror.Invalid("priority không hợp lệ", nil)
		}
		f.Priority = &p
	}
	if v := q.Get("assignee_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			return f, apperror.Invalid("assignee_id không hợp lệ", nil)
		}
		f.AssigneeID = &id
	}
	if q.Get("unfinished") == "true" {
		f.Unfinished = true
	}
	return f, nil
}

func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	f, err := buildTaskFilter(r)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	res, err := h.uc.ListTasks(r.Context(), appmw.ActorFrom(r.Context()), f)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	writeTaskPage(w, res)
}

func (h *TaskHandler) MyTasks(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	f, err := buildTaskFilter(r)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	res, err := h.uc.MyTasks(r.Context(), appmw.ActorFrom(r.Context()), f)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	writeTaskPage(w, res)
}

func (h *TaskHandler) Overdue(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()

	var projectID *uuid.UUID
	if v := q.Get("project_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("project_id không hợp lệ", nil), requestID)
			return
		}
		projectID = &id
	}

	res, err := h.uc.OverdueTasks(r.Context(), appmw.ActorFrom(r.Context()), projectID,
		atoiDefault(q.Get("page"), 1), atoiDefault(q.Get("page_size"), 20))
	if err != nil {
		Error(w, err, requestID)
		return
	}
	writeTaskPage(w, res)
}

func writeTaskPage(w http.ResponseWriter, res *ucproject.ListTasksResult) {
	httpx.Paginated(w, toTaskDTOs(res.Items), map[string]int{
		"page":        res.Page,
		"page_size":   res.PageSize,
		"total_items": res.TotalItems,
		"total_pages": res.TotalPages,
	})
}

// Board trả về bảng Kanban đã gom sẵn theo cột.
//
// Gom ở server chứ không để frontend tự nhóm: thứ tự cột là luật nghiệp vụ
// (xem BoardColumns), và cột rỗng phải xuất hiện để người dùng có chỗ thả.
// Frontend tự nhóm thì cột rỗng biến mất.
func (h *TaskHandler) Board(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	projectID, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	board, err := h.uc.GetBoard(r.Context(), appmw.ActorFrom(r.Context()), projectID)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	columns := make([]map[string]any, 0, len(domainproject.BoardColumns()))
	for _, s := range domainproject.BoardColumns() {
		columns = append(columns, map[string]any{
			"status": string(s),
			"tasks":  toTaskDTOs(board.Columns[s]),
		})
	}

	httpx.OK(w, map[string]any{
		"project": toProjectDTO(board.Project),
		"columns": columns,
	})
}

func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	t, err := h.uc.GetTask(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toTaskDTO(t))
}

func (h *TaskHandler) ListSubtasks(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	list, err := h.uc.ListSubtasks(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toTaskDTOs(list))
}

type taskRequest struct {
	ProjectID     *string  `json:"project_id"`
	ParentTaskID  *string  `json:"parent_task_id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	Status        string   `json:"status"`
	Priority      string   `json:"priority"`
	AssigneeID    *string  `json:"assignee_id"`
	DueDate       *string  `json:"due_date"`
	EstimateHours *float64 `json:"estimate_hours"`
}

func (req taskRequest) toInput() (ucproject.TaskInput, error) {
	in := ucproject.TaskInput{
		Title:         req.Title,
		Description:   req.Description,
		Status:        domainproject.TaskStatus(req.Status),
		Priority:      domainproject.Priority(req.Priority),
		EstimateHours: req.EstimateHours,
	}

	projectID, err := parseUUIDPtr(req.ProjectID)
	if err != nil {
		return in, apperror.Invalid("project_id không hợp lệ", nil)
	}
	if projectID != nil {
		in.ProjectID = *projectID
	}

	if in.ParentTaskID, err = parseUUIDPtr(req.ParentTaskID); err != nil {
		return in, apperror.Invalid("parent_task_id không hợp lệ", nil)
	}
	if in.AssigneeID, err = parseUUIDPtr(req.AssigneeID); err != nil {
		return in, apperror.Invalid("assignee_id không hợp lệ", nil)
	}

	// Hạn công việc nhận cả ngày lẫn giờ: "2026-09-30" hoặc dạng RFC3339
	// đầy đủ. Kanban thường chỉ cần ngày, nhưng việc gấp thì cần đến giờ.
	if in.DueDate, err = parseDateTimePtr(req.DueDate); err != nil {
		return in, apperror.Invalid(
			"due_date phải có dạng YYYY-MM-DD hoặc YYYY-MM-DDTHH:MM:SSZ", nil)
	}
	return in, nil
}

func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req taskRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if in.ProjectID == uuid.Nil {
		Error(w, apperror.Invalid("Thiếu project_id", nil), requestID)
		return
	}

	t, err := h.uc.CreateTask(r.Context(), appmw.ActorFrom(r.Context()), in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toTaskDTO(t))
}

func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req taskRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}

	t, err := h.uc.UpdateTask(r.Context(), appmw.ActorFrom(r.Context()), id, in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toTaskDTO(t))
}

type moveTaskRequest struct {
	Status string `json:"status"`
	// AfterTaskID là task mà task này sẽ nằm NGAY SAU. Bỏ trống/null nghĩa
	// là thả lên đầu cột.
	AfterTaskID *string `json:"after_task_id"`
}

// Move xử lý kéo-thả trên bảng Kanban.
func (h *TaskHandler) Move(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req moveTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	after, err := parseUUIDPtr(req.AfterTaskID)
	if err != nil {
		Error(w, apperror.Invalid("after_task_id không hợp lệ", nil), requestID)
		return
	}

	t, err := h.uc.MoveTask(r.Context(), appmw.ActorFrom(r.Context()), id,
		domainproject.TaskStatus(req.Status), after)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toTaskDTO(t))
}

func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.DeleteTask(r.Context(), appmw.ActorFrom(r.Context()), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã xoá công việc"})
}

// ------------------------------------------------------------ bình luận

type commentDTO struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"task_id"`
	AuthorID     string     `json:"author_id"`
	AuthorName   string     `json:"author_name,omitempty"`
	Content      string     `json:"content"`
	MentionedIDs []string   `json:"mentioned_ids"`
	EditedAt     *time.Time `json:"edited_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func toCommentDTO(c *domainproject.Comment) commentDTO {
	mentions := make([]string, 0, len(c.MentionedIDs))
	for _, id := range c.MentionedIDs {
		mentions = append(mentions, id.String())
	}
	return commentDTO{
		ID:           c.ID.String(),
		TaskID:       c.TaskID.String(),
		AuthorID:     c.AuthorID.String(),
		AuthorName:   c.AuthorName,
		Content:      c.Content,
		MentionedIDs: mentions,
		EditedAt:     c.EditedAt,
		CreatedAt:    c.CreatedAt,
	}
}

func (h *TaskHandler) ListComments(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	list, err := h.uc.ListComments(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]commentDTO, 0, len(list))
	for _, c := range list {
		out = append(out, toCommentDTO(c))
	}
	httpx.OK(w, out)
}

type commentRequest struct {
	Content string `json:"content"`
}

func (h *TaskHandler) CreateComment(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	taskID, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req commentRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	c, err := h.uc.CreateComment(r.Context(), appmw.ActorFrom(r.Context()),
		taskID, req.Content)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toCommentDTO(c))
}

func (h *TaskHandler) UpdateComment(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	commentID, err := parseUUIDParam(r, "commentID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req commentRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	c, err := h.uc.UpdateComment(r.Context(), appmw.ActorFrom(r.Context()),
		commentID, req.Content)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toCommentDTO(c))
}

func (h *TaskHandler) DeleteComment(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	commentID, err := parseUUIDParam(r, "commentID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.DeleteComment(r.Context(), appmw.ActorFrom(r.Context()),
		commentID); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã xoá bình luận"})
}

// -------------------------------------------------------- tệp đính kèm

type attachmentDTO struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	FileName     string    `json:"file_name"`
	ContentType  string    `json:"content_type"`
	SizeBytes    int64     `json:"size_bytes"`
	UploaderName string    `json:"uploader_name,omitempty"`
	DownloadURL  string    `json:"download_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (h *TaskHandler) toAttachmentDTO(
	r *http.Request,
	a *domainproject.Attachment,
) attachmentDTO {
	return attachmentDTO{
		ID:           a.ID.String(),
		TaskID:       a.TaskID.String(),
		FileName:     a.FileName,
		ContentType:  a.ContentType,
		SizeBytes:    a.SizeBytes,
		UploaderName: a.UploaderName,
		DownloadURL:  h.uc.AttachmentURL(r.Context(), a.StorageKey),
		CreatedAt:    a.CreatedAt,
	}
}

func (h *TaskHandler) ListAttachments(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	list, err := h.uc.ListAttachments(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]attachmentDTO, 0, len(list))
	for _, a := range list {
		out = append(out, h.toAttachmentDTO(r, a))
	}
	httpx.OK(w, out)
}

type attachmentUploadRequest struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
}

func (h *TaskHandler) RequestAttachmentUpload(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	taskID, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req attachmentUploadRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	ticket, err := h.uc.RequestAttachmentUpload(r.Context(), appmw.ActorFrom(r.Context()),
		taskID, req.FileName, req.ContentType)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, ticket)
}

type attachmentConfirmRequest struct {
	Key      string `json:"key"`
	FileName string `json:"file_name"`
}

func (h *TaskHandler) ConfirmAttachment(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	taskID, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req attachmentConfirmRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	a, err := h.uc.ConfirmAttachment(r.Context(), appmw.ActorFrom(r.Context()),
		taskID, req.Key, req.FileName)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, h.toAttachmentDTO(r, a))
}

func (h *TaskHandler) DeleteAttachment(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "attachmentID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.DeleteAttachment(r.Context(), appmw.ActorFrom(r.Context()),
		id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã xoá tệp đính kèm"})
}

// ---------------------------------------------------- nhật ký, thời gian

func (h *TaskHandler) ListActivities(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	list, err := h.uc.ListActivities(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, map[string]any{
			"id":         a.ID.String(),
			"actor_name": a.ActorName,
			"action":     a.Action,
			"field":      a.Field,
			"old_value":  a.OldValue,
			"new_value":  a.NewValue,
			"created_at": a.CreatedAt,
		})
	}
	httpx.OK(w, out)
}

type timelogDTO struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id"`
	EmployeeID   string    `json:"employee_id"`
	EmployeeName string    `json:"employee_name,omitempty"`
	SpentMinutes int       `json:"spent_minutes"`
	Note         string    `json:"note,omitempty"`
	LoggedOn     time.Time `json:"logged_on"`
	CreatedAt    time.Time `json:"created_at"`
}

func toTimelogDTO(t *domainproject.Timelog) timelogDTO {
	return timelogDTO{
		ID:           t.ID.String(),
		TaskID:       t.TaskID.String(),
		EmployeeID:   t.EmployeeID.String(),
		EmployeeName: t.EmployeeName,
		SpentMinutes: t.SpentMinutes,
		Note:         t.Note,
		LoggedOn:     t.LoggedOn,
		CreatedAt:    t.CreatedAt,
	}
}

func (h *TaskHandler) ListTimelogs(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	list, err := h.uc.ListTimelogs(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]timelogDTO, 0, len(list))
	for _, t := range list {
		out = append(out, toTimelogDTO(t))
	}
	httpx.OK(w, out)
}

type timelogRequest struct {
	SpentMinutes int     `json:"spent_minutes"`
	Note         string  `json:"note"`
	LoggedOn     *string `json:"logged_on"`
}

func (h *TaskHandler) LogTime(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	taskID, err := parseUUIDParam(r, "taskID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req timelogRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	loggedOn, err := parseDatePtr(req.LoggedOn)
	if err != nil {
		Error(w, apperror.Invalid("logged_on phải có dạng YYYY-MM-DD", nil), requestID)
		return
	}

	in := ucproject.TimelogInput{
		SpentMinutes: req.SpentMinutes,
		Note:         req.Note,
	}
	if loggedOn != nil {
		in.LoggedOn = *loggedOn
	}

	t, err := h.uc.LogTime(r.Context(), appmw.ActorFrom(r.Context()), taskID, in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toTimelogDTO(t))
}

func (h *TaskHandler) DeleteTimelog(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "timelogID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.DeleteTimelog(r.Context(), appmw.ActorFrom(r.Context()),
		id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã xoá bản ghi thời gian"})
}
