package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type attendanceExportDTO struct {
	ID             string     `json:"id"`
	Year           int        `json:"year"`
	Month          int        `json:"month"`
	DepartmentID   *string    `json:"department_id,omitempty"`
	DepartmentName string     `json:"department_name,omitempty"`
	Status         string     `json:"status"`
	RequesterName  string     `json:"requester_name,omitempty"`
	FileName       string     `json:"file_name,omitempty"`
	HasFile        bool       `json:"has_file"`
	EmployeeCount  int        `json:"employee_count"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

func toAttendanceExportDTO(e *domainatt.Export) attendanceExportDTO {
	d := attendanceExportDTO{
		ID:             e.ID.String(),
		Year:           e.Year,
		Month:          e.Month,
		DepartmentName: e.DepartmentName,
		Status:         string(e.Status),
		RequesterName:  e.RequesterName,
		FileName:       e.FileName,
		HasFile:        e.HasFile,
		EmployeeCount:  e.EmployeeCount,
		Error:          e.Error,
		CreatedAt:      e.CreatedAt,
		FinishedAt:     e.FinishedAt,
	}
	if e.DepartmentID != nil {
		s := e.DepartmentID.String()
		d.DepartmentID = &s
	}
	return d
}

type exportRequest struct {
	Year         int     `json:"year"`
	Month        int     `json:"month"`
	DepartmentID *string `json:"department_id"`
}

func (h *AttendanceHandler) RequestExport(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req exportRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	deptID, err := parseUUIDPtr(req.DepartmentID)
	if err != nil {
		Error(w, apperror.Invalid("department_id không hợp lệ", nil), requestID)
		return
	}

	e, err := h.uc.RequestExport(r.Context(), appmw.ActorFrom(r.Context()), req.Year, req.Month, deptID)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	// 202: đã nhận, tệp chưa có.
	httpx.JSON(w, http.StatusAccepted, httpx.Envelope{Data: toAttendanceExportDTO(e)})
}

func (h *AttendanceHandler) ListExports(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	list, err := h.uc.ListExports(r.Context(), appmw.ActorFrom(r.Context()))
	if err != nil {
		Error(w, err, requestID)
		return
	}
	out := make([]attendanceExportDTO, 0, len(list))
	for _, e := range list {
		out = append(out, toAttendanceExportDTO(e))
	}
	httpx.OK(w, out)
}

// DownloadExport trả thẳng tệp .xlsx, không bọc JSON.
func (h *AttendanceHandler) DownloadExport(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "exportID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	name, data, err := h.uc.ExportFile(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	// filename* theo RFC 5987: tên tệp có thể có ký tự ngoài ASCII.
	w.Header().Set("Content-Disposition", fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`, name, url.PathEscape(name)))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	// Báo cáo chấm công của người khác: không để proxy hay trình duyệt giữ bản sao.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}
