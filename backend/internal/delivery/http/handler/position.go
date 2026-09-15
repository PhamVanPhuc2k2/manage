package handler

import (
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type PositionHandler struct {
	uc *uchr.Usecase
}

func NewPositionHandler(uc *uchr.Usecase) *PositionHandler {
	return &PositionHandler{uc: uc}
}

type positionDTO struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	SalaryMin *float64  `json:"salary_min,omitempty"`
	SalaryMax *float64  `json:"salary_max,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func toPositionDTO(p *domainhr.Position) positionDTO {
	return positionDTO{
		ID:        p.ID.String(),
		Code:      p.Code,
		Name:      p.Name,
		SalaryMin: p.SalaryMin,
		SalaryMax: p.SalaryMax,
		CreatedAt: p.CreatedAt,
	}
}

func (h *PositionHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.uc.ListPositions(r.Context())
	if err != nil {
		Error(w, err, chimw.GetReqID(r.Context()))
		return
	}

	out := make([]positionDTO, 0, len(list))
	for _, p := range list {
		out = append(out, toPositionDTO(p))
	}
	httpx.OK(w, out)
}

type positionRequest struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	SalaryMin *float64 `json:"salary_min"`
	SalaryMax *float64 `json:"salary_max"`
}

func (h *PositionHandler) Create(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req positionRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	p, err := h.uc.CreatePosition(r.Context(), uchr.PositionInput{
		Code: req.Code, Name: req.Name,
		SalaryMin: req.SalaryMin, SalaryMax: req.SalaryMax,
	})
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toPositionDTO(p))
}

func (h *PositionHandler) Update(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req positionRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	p, err := h.uc.UpdatePosition(r.Context(), id, uchr.PositionInput{
		Code: req.Code, Name: req.Name,
		SalaryMin: req.SalaryMin, SalaryMax: req.SalaryMax,
	})
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toPositionDTO(p))
}

func (h *PositionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.DeletePosition(r.Context(), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã xoá chức vụ"})
}
