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

type ProjectHandler struct {
	uc *ucproject.Usecase
}

func NewProjectHandler(uc *ucproject.Usecase) *ProjectHandler {
	return &ProjectHandler{uc: uc}
}

type projectDTO struct {
	ID             string     `json:"id"`
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	Description    string     `json:"description,omitempty"`
	Status         string     `json:"status"`
	OwnerID        string     `json:"owner_id"`
	OwnerName      string     `json:"owner_name,omitempty"`
	DepartmentID   *string    `json:"department_id,omitempty"`
	DepartmentName string     `json:"department_name,omitempty"`
	StartDate      *time.Time `json:"start_date,omitempty"`
	DueDate        *time.Time `json:"due_date,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	MemberCount    int        `json:"member_count"`
	TaskCount      int        `json:"task_count"`
	DoneTaskCount  int        `json:"done_task_count"`
	Progress       int        `json:"progress"`
	ViewerRole     string     `json:"viewer_role,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

func toProjectDTO(p *domainproject.Project) projectDTO {
	return projectDTO{
		ID:             p.ID.String(),
		Code:           p.Code,
		Name:           p.Name,
		Description:    p.Description,
		Status:         string(p.Status),
		OwnerID:        p.OwnerID.String(),
		OwnerName:      p.OwnerName,
		DepartmentID:   uuidPtrToString(p.DepartmentID),
		DepartmentName: p.DepartmentName,
		StartDate:      p.StartDate,
		DueDate:        p.DueDate,
		CompletedAt:    p.CompletedAt,
		MemberCount:    p.MemberCount,
		TaskCount:      p.TaskCount,
		DoneTaskCount:  p.DoneTaskCount,
		Progress:       p.Progress(),
		ViewerRole:     string(p.ViewerRole),
		CreatedAt:      p.CreatedAt,
	}
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()

	f := domainproject.ProjectFilter{
		Search:   q.Get("search"),
		Page:     atoiDefault(q.Get("page"), 1),
		PageSize: atoiDefault(q.Get("page_size"), 20),
		SortBy:   q.Get("sort_by"),
		SortDesc: q.Get("sort_order") == "desc",
	}

	// KHÔNG bind MemberEmployeeID từ query — usecase đặt nó theo phạm vi
	// của actor. Cho client đặt là mở toang lỗ hổng phân quyền.
	if v := q.Get("status"); v != "" {
		s := domainproject.Status(v)
		if !s.Valid() {
			Error(w, apperror.Invalid("status không hợp lệ", nil), requestID)
			return
		}
		f.Status = &s
	}
	if v := q.Get("owner_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("owner_id không hợp lệ", nil), requestID)
			return
		}
		f.OwnerID = &id
	}
	if v := q.Get("department_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("department_id không hợp lệ", nil), requestID)
			return
		}
		f.DepartmentID = &id
	}

	res, err := h.uc.ListProjects(r.Context(), appmw.ActorFrom(r.Context()), f)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	items := make([]projectDTO, 0, len(res.Items))
	for _, p := range res.Items {
		items = append(items, toProjectDTO(p))
	}

	httpx.Paginated(w, items, map[string]int{
		"page":        res.Page,
		"page_size":   res.PageSize,
		"total_items": res.TotalItems,
		"total_pages": res.TotalPages,
	})
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	p, err := h.uc.GetProject(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toProjectDTO(p))
}

type projectRequest struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Status       string  `json:"status"`
	OwnerID      *string `json:"owner_id"`
	DepartmentID *string `json:"department_id"`
	StartDate    *string `json:"start_date"`
	DueDate      *string `json:"due_date"`
}

func (req projectRequest) toInput() (ucproject.ProjectInput, error) {
	in := ucproject.ProjectInput{
		Code:        req.Code,
		Name:        req.Name,
		Description: req.Description,
		Status:      domainproject.Status(req.Status),
	}
	var err error

	// Giữ nguyên con trỏ: nil nghĩa là client KHÔNG gửi owner_id, khác hẳn
	// với việc gửi một id không tồn tại.
	in.OwnerID, err = parseUUIDPtr(req.OwnerID)
	if err != nil {
		return in, apperror.Invalid("owner_id không hợp lệ", nil)
	}

	if in.DepartmentID, err = parseUUIDPtr(req.DepartmentID); err != nil {
		return in, apperror.Invalid("department_id không hợp lệ", nil)
	}
	if in.StartDate, err = parseDatePtr(req.StartDate); err != nil {
		return in, apperror.Invalid("start_date phải có dạng YYYY-MM-DD", nil)
	}
	if in.DueDate, err = parseDatePtr(req.DueDate); err != nil {
		return in, apperror.Invalid("due_date phải có dạng YYYY-MM-DD", nil)
	}
	return in, nil
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req projectRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}

	p, err := h.uc.CreateProject(r.Context(), appmw.ActorFrom(r.Context()), in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toProjectDTO(p))
}

func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req projectRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}

	p, err := h.uc.UpdateProject(r.Context(), appmw.ActorFrom(r.Context()), id, in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toProjectDTO(p))
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.DeleteProject(r.Context(), appmw.ActorFrom(r.Context()), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã xoá dự án"})
}

// ---------------------------------------------------------- thành viên

type memberDTO struct {
	EmployeeID     string    `json:"employee_id"`
	EmployeeName   string    `json:"employee_name"`
	EmployeeCode   string    `json:"employee_code,omitempty"`
	Email          string    `json:"email,omitempty"`
	PositionName   string    `json:"position_name,omitempty"`
	DepartmentName string    `json:"department_name,omitempty"`
	Role           string    `json:"role"`
	AddedAt        time.Time `json:"added_at"`
}

func toMemberDTO(m *domainproject.Member) memberDTO {
	return memberDTO{
		EmployeeID:     m.EmployeeID.String(),
		EmployeeName:   m.EmployeeName,
		EmployeeCode:   m.EmployeeCode,
		Email:          m.Email,
		PositionName:   m.PositionName,
		DepartmentName: m.DepartmentName,
		Role:           string(m.Role),
		AddedAt:        m.AddedAt,
	}
}

func (h *ProjectHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	list, err := h.uc.ListMembers(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]memberDTO, 0, len(list))
	for _, m := range list {
		out = append(out, toMemberDTO(m))
	}
	httpx.OK(w, out)
}

type memberRequest struct {
	EmployeeID string `json:"employee_id"`
	Role       string `json:"role"`
}

func (h *ProjectHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	projectID, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req memberRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	employeeID, err := uuid.Parse(req.EmployeeID)
	if err != nil {
		Error(w, apperror.Invalid("employee_id không hợp lệ", nil), requestID)
		return
	}

	m, err := h.uc.AddMember(r.Context(), appmw.ActorFrom(r.Context()),
		projectID, employeeID, domainproject.Role(req.Role))
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toMemberDTO(m))
}

func (h *ProjectHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	projectID, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	employeeID, err := parseUUIDParam(r, "employeeID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req memberRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	m, err := h.uc.UpdateMemberRole(r.Context(), appmw.ActorFrom(r.Context()),
		projectID, employeeID, domainproject.Role(req.Role))
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toMemberDTO(m))
}

func (h *ProjectHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	projectID, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	employeeID, err := parseUUIDParam(r, "employeeID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.RemoveMember(r.Context(), appmw.ActorFrom(r.Context()),
		projectID, employeeID); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã gỡ thành viên"})
}

// ------------------------------------------------------------- báo cáo

func (h *ProjectHandler) Progress(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	p, err := h.uc.ProjectProgress(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	byStatus := make(map[string]int, len(p.ByStatus))
	for k, v := range p.ByStatus {
		byStatus[string(k)] = v
	}

	httpx.OK(w, map[string]any{
		"project_id":     p.ProjectID.String(),
		"project_code":   p.ProjectCode,
		"project_name":   p.ProjectName,
		"status":         string(p.Status),
		"due_date":       p.DueDate,
		"total_tasks":    p.TotalTasks,
		"by_status":      byStatus,
		"overdue_tasks":  p.OverdueTasks,
		"spent_minutes":  p.SpentMinutes,
		"estimate_hours": p.EstimateHrs,
		"progress":       p.Progress(),
	})
}

func (h *ProjectHandler) Workload(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var projectID *uuid.UUID
	if v := r.URL.Query().Get("project_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("project_id không hợp lệ", nil), requestID)
			return
		}
		projectID = &id
	}

	list, err := h.uc.Workload(r.Context(), appmw.ActorFrom(r.Context()), projectID)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, wl := range list {
		out = append(out, map[string]any{
			"employee_id":    wl.EmployeeID.String(),
			"employee_name":  wl.EmployeeName,
			"open_tasks":     wl.OpenTasks,
			"overdue_tasks":  wl.OverdueTasks,
			"done_tasks":     wl.DoneTasks,
			"estimate_hours": wl.EstimateHrs,
			"spent_minutes":  wl.SpentMinutes,
		})
	}
	httpx.OK(w, out)
}
