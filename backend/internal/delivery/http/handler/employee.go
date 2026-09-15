package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type EmployeeHandler struct {
	uc *uchr.Usecase
}

func NewEmployeeHandler(uc *uchr.Usecase) *EmployeeHandler {
	return &EmployeeHandler{uc: uc}
}

type employeeDTO struct {
	ID             string     `json:"id"`
	EmployeeCode   string     `json:"employee_code"`
	FullName       string     `json:"full_name"`
	Email          string     `json:"email"`
	Phone          string     `json:"phone,omitempty"`
	DateOfBirth    *time.Time `json:"date_of_birth,omitempty"`
	Gender         string     `json:"gender,omitempty"`
	Address        string     `json:"address,omitempty"`
	DepartmentID   *string    `json:"department_id,omitempty"`
	DepartmentName string     `json:"department_name,omitempty"`
	PositionID     *string    `json:"position_id,omitempty"`
	PositionName   string     `json:"position_name,omitempty"`
	ManagerID      *string    `json:"manager_id,omitempty"`
	ManagerName    string     `json:"manager_name,omitempty"`
	WorkMode       string     `json:"work_mode"`
	Status         string     `json:"status"`
	JoinedAt       time.Time  `json:"joined_at"`
	ResignedAt     *time.Time `json:"resigned_at,omitempty"`
	HasAccount     bool       `json:"has_account"`
	CreatedAt      time.Time  `json:"created_at"`
}

func toEmployeeDTO(e *domainhr.Employee) employeeDTO {
	return employeeDTO{
		ID:             e.ID.String(),
		EmployeeCode:   e.EmployeeCode,
		FullName:       e.FullName,
		Email:          e.Email,
		Phone:          e.Phone,
		DateOfBirth:    e.DateOfBirth,
		Gender:         e.Gender,
		Address:        e.Address,
		DepartmentID:   uuidPtrToString(e.DepartmentID),
		DepartmentName: e.DepartmentName,
		PositionID:     uuidPtrToString(e.PositionID),
		PositionName:   e.PositionName,
		ManagerID:      uuidPtrToString(e.ManagerID),
		ManagerName:    e.ManagerName,
		WorkMode:       string(e.WorkMode),
		Status:         string(e.Status),
		JoinedAt:       e.JoinedAt,
		ResignedAt:     e.ResignedAt,
		HasAccount:     e.HasAccount,
		CreatedAt:      e.CreatedAt,
	}
}

func (h *EmployeeHandler) List(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	actor := appmw.ActorFrom(r.Context())
	q := r.URL.Query()

	f := domainhr.EmployeeFilter{
		Search:   q.Get("search"),
		Page:     atoiDefault(q.Get("page"), 1),
		PageSize: atoiDefault(q.Get("page_size"), 20),
		SortBy:   q.Get("sort_by"),
		SortDesc: q.Get("sort_order") == "desc",
	}

	// Lưu ý: KHÔNG bind ScopedEmployeeID / ScopedDepartmentIDs từ query.
	// Hai trường đó chỉ được usecase đặt dựa trên phạm vi của actor —
	// cho client đặt chúng là mở toang lỗ hổng phân quyền.
	if v := q.Get("department_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("department_id không hợp lệ", nil), requestID)
			return
		}
		f.DepartmentID = &id
	}
	if v := q.Get("position_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("position_id không hợp lệ", nil), requestID)
			return
		}
		f.PositionID = &id
	}
	if v := q.Get("status"); v != "" {
		s := domainhr.EmployeeStatus(v)
		if !s.Valid() {
			Error(w, apperror.Invalid("status không hợp lệ", nil), requestID)
			return
		}
		f.Status = &s
	}
	if v := q.Get("work_mode"); v != "" {
		m := domainhr.WorkMode(v)
		if !m.Valid() {
			Error(w, apperror.Invalid("work_mode không hợp lệ", nil), requestID)
			return
		}
		f.WorkMode = &m
	}

	res, err := h.uc.ListEmployees(r.Context(), actor, f)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	items := make([]employeeDTO, 0, len(res.Items))
	for _, e := range res.Items {
		items = append(items, toEmployeeDTO(e))
	}

	httpx.Paginated(w, items, map[string]int{
		"page":        res.Page,
		"page_size":   res.PageSize,
		"total_items": res.TotalItems,
		"total_pages": res.TotalPages,
	})
}

func (h *EmployeeHandler) Get(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	e, err := h.uc.GetEmployee(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toEmployeeDTO(e))
}

type employeeRequest struct {
	EmployeeCode string  `json:"employee_code"`
	FullName     string  `json:"full_name"`
	Email        string  `json:"email"`
	Phone        string  `json:"phone"`
	DateOfBirth  *string `json:"date_of_birth"`
	Gender       string  `json:"gender"`
	Address      string  `json:"address"`
	DepartmentID *string `json:"department_id"`
	PositionID   *string `json:"position_id"`
	ManagerID    *string `json:"manager_id"`
	WorkMode     string  `json:"work_mode"`
	Status       string  `json:"status"`
	JoinedAt     string  `json:"joined_at"`
	ResignedAt   *string `json:"resigned_at"`
}

func (req employeeRequest) toInput() (uchr.EmployeeInput, error) {
	in := uchr.EmployeeInput{
		EmployeeCode: req.EmployeeCode,
		FullName:     req.FullName,
		Email:        req.Email,
		Phone:        req.Phone,
		Gender:       req.Gender,
		Address:      req.Address,
		WorkMode:     domainhr.WorkMode(req.WorkMode),
		Status:       domainhr.EmployeeStatus(req.Status),
	}
	if in.WorkMode == "" {
		in.WorkMode = domainhr.WorkModeOnsite
	}
	if in.Status == "" {
		in.Status = domainhr.StatusProbation
	}

	joined, err := parseDate(req.JoinedAt)
	if err != nil {
		return in, apperror.Invalid("Ngày vào làm không hợp lệ (định dạng YYYY-MM-DD)", nil)
	}
	if joined == nil {
		return in, apperror.Invalid("Ngày vào làm không được để trống", nil)
	}
	in.JoinedAt = *joined

	if in.DateOfBirth, err = parseDatePtr(req.DateOfBirth); err != nil {
		return in, apperror.Invalid("Ngày sinh không hợp lệ (định dạng YYYY-MM-DD)", nil)
	}
	if in.ResignedAt, err = parseDatePtr(req.ResignedAt); err != nil {
		return in, apperror.Invalid("Ngày nghỉ việc không hợp lệ (định dạng YYYY-MM-DD)", nil)
	}

	if in.DepartmentID, err = parseUUIDPtr(req.DepartmentID); err != nil {
		return in, apperror.Invalid("department_id không hợp lệ", nil)
	}
	if in.PositionID, err = parseUUIDPtr(req.PositionID); err != nil {
		return in, apperror.Invalid("position_id không hợp lệ", nil)
	}
	if in.ManagerID, err = parseUUIDPtr(req.ManagerID); err != nil {
		return in, apperror.Invalid("manager_id không hợp lệ", nil)
	}
	return in, nil
}

func (h *EmployeeHandler) Create(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req employeeRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}

	e, err := h.uc.CreateEmployee(r.Context(), appmw.ActorFrom(r.Context()), in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toEmployeeDTO(e))
}

func (h *EmployeeHandler) Update(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req employeeRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}

	e, err := h.uc.UpdateEmployee(r.Context(), appmw.ActorFrom(r.Context()), id, in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toEmployeeDTO(e))
}

func (h *EmployeeHandler) Deactivate(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.DeactivateEmployee(r.Context(), appmw.ActorFrom(r.Context()), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã vô hiệu hoá nhân viên"})
}

// --------------------------------------------------------------- tiện ích

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, apperror.Invalid("Mã định danh không hợp lệ", nil)
	}
	return id, nil
}

func parseUUIDPtr(s *string) (*uuid.UUID, error) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil, nil
	}
	id, err := uuid.Parse(strings.TrimSpace(*s))
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func parseDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func parseDatePtr(s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}
	return parseDate(*s)
}

func uuidPtrToString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
