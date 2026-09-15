package handler

import (
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type RoleHandler struct {
	uc *uchr.Usecase
}

func NewRoleHandler(uc *uchr.Usecase) *RoleHandler {
	return &RoleHandler{uc: uc}
}

type roleDTO struct {
	Code        string   `json:"code"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Scope       string   `json:"scope"`
	IsSystem    bool     `json:"is_system"`
	Permissions []string `json:"permissions"`
}

func toRoleDTO(r *domainhr.Role) roleDTO {
	perms := r.Permissions
	if perms == nil {
		perms = []string{}
	}
	return roleDTO{
		Code:        r.Code,
		Name:        r.Name,
		Description: r.Description,
		Scope:       r.Scope,
		IsSystem:    r.IsSystem,
		Permissions: perms,
	}
}

func (h *RoleHandler) List(w http.ResponseWriter, r *http.Request) {
	roles, err := h.uc.ListRoles(r.Context())
	if err != nil {
		Error(w, err, chimw.GetReqID(r.Context()))
		return
	}

	out := make([]roleDTO, 0, len(roles))
	for _, role := range roles {
		out = append(out, toRoleDTO(role))
	}
	httpx.OK(w, out)
}

// --------------------------------------------------- tài khoản của nhân viên

// CreateAccount tạo tài khoản đăng nhập cho một nhân viên đã có hồ sơ.
func (h *EmployeeHandler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	res, err := h.uc.CreateAccount(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, res)
}

type setActiveRequest struct {
	Active bool `json:"active"`
}

func (h *EmployeeHandler) SetAccountActive(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req setActiveRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	if err := h.uc.SetAccountActive(r.Context(), appmw.ActorFrom(r.Context()), id, req.Active); err != nil {
		Error(w, err, requestID)
		return
	}

	status := "đã vô hiệu hoá tài khoản"
	if req.Active {
		status = "đã kích hoạt tài khoản"
	}
	httpx.OK(w, map[string]string{"status": status})
}

// ------------------------------------------------------- vai trò của nhân viên

func (h *EmployeeHandler) GetRoles(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	res, err := h.uc.GetEmployeeRoles(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, res)
}

type setRolesRequest struct {
	Roles []string `json:"roles"`
}

func (h *EmployeeHandler) SetRoles(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req setRolesRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	res, err := h.uc.SetEmployeeRoles(r.Context(), appmw.ActorFrom(r.Context()), id, req.Roles)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, res)
}
