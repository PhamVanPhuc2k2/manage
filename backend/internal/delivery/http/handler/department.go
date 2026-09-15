package handler

import (
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type DepartmentHandler struct {
	uc *uchr.Usecase
}

func NewDepartmentHandler(uc *uchr.Usecase) *DepartmentHandler {
	return &DepartmentHandler{uc: uc}
}

type departmentDTO struct {
	ID            string          `json:"id"`
	ParentID      *string         `json:"parent_id,omitempty"`
	Code          string          `json:"code"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	ManagerID     *string         `json:"manager_id,omitempty"`
	ManagerName   string          `json:"manager_name,omitempty"`
	EmployeeCount int             `json:"employee_count"`
	Children      []departmentDTO `json:"children,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
}

func toDepartmentDTO(d *domainhr.Department) departmentDTO {
	dto := departmentDTO{
		ID:            d.ID.String(),
		ParentID:      uuidPtrToString(d.ParentID),
		Code:          d.Code,
		Name:          d.Name,
		Description:   d.Description,
		ManagerID:     uuidPtrToString(d.ManagerID),
		ManagerName:   d.ManagerName,
		EmployeeCount: d.EmployeeCount,
		CreatedAt:     d.CreatedAt,
	}
	for _, c := range d.Children {
		dto.Children = append(dto.Children, toDepartmentDTO(c))
	}
	return dto
}

func (h *DepartmentHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.uc.ListDepartments(r.Context())
	if err != nil {
		Error(w, err, chimw.GetReqID(r.Context()))
		return
	}

	out := make([]departmentDTO, 0, len(list))
	for _, d := range list {
		out = append(out, toDepartmentDTO(d))
	}
	httpx.OK(w, out)
}

func (h *DepartmentHandler) Tree(w http.ResponseWriter, r *http.Request) {
	roots, err := h.uc.DepartmentTree(r.Context())
	if err != nil {
		Error(w, err, chimw.GetReqID(r.Context()))
		return
	}

	out := make([]departmentDTO, 0, len(roots))
	for _, d := range roots {
		out = append(out, toDepartmentDTO(d))
	}
	httpx.OK(w, out)
}

func (h *DepartmentHandler) Get(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	d, err := h.uc.GetDepartment(r.Context(), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toDepartmentDTO(d))
}

type departmentRequest struct {
	ParentID    *string `json:"parent_id"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	ManagerID   *string `json:"manager_id"`
}

func (req departmentRequest) toInput() (uchr.DepartmentInput, error) {
	in := uchr.DepartmentInput{
		Code:        req.Code,
		Name:        req.Name,
		Description: req.Description,
	}
	var err error
	if in.ParentID, err = parseUUIDPtr(req.ParentID); err != nil {
		return in, err
	}
	if in.ManagerID, err = parseUUIDPtr(req.ManagerID); err != nil {
		return in, err
	}
	return in, nil
}

func (h *DepartmentHandler) Create(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req departmentRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}

	d, err := h.uc.CreateDepartment(r.Context(), in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toDepartmentDTO(d))
}

func (h *DepartmentHandler) Update(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req departmentRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}

	d, err := h.uc.UpdateDepartment(r.Context(), id, in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toDepartmentDTO(d))
}

func (h *DepartmentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.DeleteDepartment(r.Context(), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã xoá phòng ban"})
}
