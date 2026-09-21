package handler

import (
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
	ucatt "github.com/PhamVanPhuc2k2/manage/internal/usecase/attendance"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type AttendanceHandler struct {
	uc *ucatt.Usecase
}

func NewAttendanceHandler(uc *ucatt.Usecase) *AttendanceHandler {
	return &AttendanceHandler{uc: uc}
}

// --------------------------------------------------------------- DTO

type dayDTO struct {
	EmployeeID     string     `json:"employee_id"`
	EmployeeName   string     `json:"employee_name,omitempty"`
	DepartmentName string     `json:"department_name,omitempty"`
	WorkDate       string     `json:"work_date"`
	OnlineMinutes  int        `json:"online_minutes"`
	ActiveMinutes  int        `json:"active_minutes"`
	FirstSeenAt    *time.Time `json:"first_seen_at,omitempty"`
	LastSeenAt     *time.Time `json:"last_seen_at,omitempty"`
	LateMinutes    int        `json:"late_minutes"`
	EarlyLeaveMins int        `json:"early_leave_minutes"`
	ShortfallMins  int        `json:"shortfall_minutes"`
	Status         string     `json:"status"`
	IsLocked       bool       `json:"is_locked"`
}

func toDayDTO(d *domainatt.Day) dayDTO {
	return dayDTO{
		EmployeeID:     d.EmployeeID.String(),
		EmployeeName:   d.EmployeeName,
		DepartmentName: d.DepartmentName,
		WorkDate:       d.WorkDate.Format("2006-01-02"),
		OnlineMinutes:  d.OnlineMinutes,
		ActiveMinutes:  d.ActiveMinutes,
		FirstSeenAt:    d.FirstSeenAt,
		LastSeenAt:     d.LastSeenAt,
		LateMinutes:    d.LateMinutes,
		EarlyLeaveMins: d.EarlyLeaveMinutes,
		ShortfallMins:  d.ShortfallMinutes,
		Status:         string(d.Status),
		IsLocked:       d.IsLocked,
	}
}

type sessionDTO struct {
	ID            string    `json:"id"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	Minutes       int       `json:"minutes"`
	ActiveMinutes int       `json:"active_minutes"`
	Source        string    `json:"source"`
	Note          string    `json:"note,omitempty"`
}

func toSessionDTO(s *domainatt.Session) sessionDTO {
	return sessionDTO{
		ID:            s.ID.String(),
		StartedAt:     s.StartedAt,
		EndedAt:       s.EndedAt,
		Minutes:       s.Minutes(),
		ActiveMinutes: s.ActiveMinutes,
		Source:        string(s.Source),
		Note:          s.Note,
	}
}

type leaveDTO struct {
	ID           string     `json:"id"`
	EmployeeID   string     `json:"employee_id"`
	EmployeeName string     `json:"employee_name,omitempty"`
	Type         string     `json:"leave_type"`
	StartDate    string     `json:"start_date"`
	EndDate      string     `json:"end_date"`
	DayPart      string     `json:"day_part"`
	Days         float64    `json:"days"`
	Reason       string     `json:"reason,omitempty"`
	Status       string     `json:"status"`
	ApproverName string     `json:"approver_name,omitempty"`
	DecidedAt    *time.Time `json:"decided_at,omitempty"`
	DecisionNote string     `json:"decision_note,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func toLeaveDTO(l *domainatt.LeaveRequest) leaveDTO {
	return leaveDTO{
		ID:           l.ID.String(),
		EmployeeID:   l.EmployeeID.String(),
		EmployeeName: l.EmployeeName,
		Type:         string(l.Type),
		StartDate:    l.StartDate.Format("2006-01-02"),
		EndDate:      l.EndDate.Format("2006-01-02"),
		DayPart:      string(l.DayPart),
		Days:         l.Days,
		Reason:       l.Reason,
		Status:       string(l.Status),
		ApproverName: l.ApproverName,
		DecidedAt:    l.DecidedAt,
		DecisionNote: l.DecisionNote,
		CreatedAt:    l.CreatedAt,
	}
}

// --------------------------------------------------- bảng công cá nhân

func (h *AttendanceHandler) Today(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	t, err := h.uc.Today(r.Context(), appmw.ActorFrom(r.Context()))
	if err != nil {
		Error(w, err, requestID)
		return
	}

	httpx.OK(w, map[string]any{
		"work_date":        t.WorkDate.Format("2006-01-02"),
		"status":           t.Status,
		"online_minutes":   t.OnlineMinutes,
		"active_minutes":   t.ActiveMinutes,
		"first_seen_at":    t.FirstSeenAt,
		"expected_minutes": t.ExpectedMins,
		"schedule_name":    t.ScheduleName,
	})
}

// parseRange đọc from/to từ query, mặc định là 30 ngày gần nhất.
func parseRange(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	now := time.Now()

	from := now.AddDate(0, 0, -30)
	to := now

	if v := q.Get("from"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return from, to, apperror.Invalid("from phải có dạng YYYY-MM-DD", nil)
		}
		from = t
	}
	if v := q.Get("to"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			return from, to, apperror.Invalid("to phải có dạng YYYY-MM-DD", nil)
		}
		to = t
	}
	return from, to, nil
}

func (h *AttendanceHandler) ListDays(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()

	from, to, err := parseRange(r)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	f := domainatt.DayFilter{From: from, To: to}

	// KHÔNG bind ScopedEmployeeIDs từ query — usecase đặt theo phạm vi của
	// actor. Cho client đặt là mở toang dữ liệu chấm công của cả công ty.
	if v := q.Get("employee_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("employee_id không hợp lệ", nil), requestID)
			return
		}
		f.EmployeeID = &id
	}
	if v := q.Get("department_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("department_id không hợp lệ", nil), requestID)
			return
		}
		f.DepartmentID = &id
	}

	list, err := h.uc.ListDays(r.Context(), appmw.ActorFrom(r.Context()), f)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]dayDTO, 0, len(list))
	for _, d := range list {
		out = append(out, toDayDTO(d))
	}
	httpx.OK(w, out)
}

func (h *AttendanceHandler) GetDay(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()

	actor := appmw.ActorFrom(r.Context())
	employeeID := actor.EmployeeID
	if v := q.Get("employee_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("employee_id không hợp lệ", nil), requestID)
			return
		}
		employeeID = id
	}

	day := time.Now()
	if v := q.Get("date"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			Error(w, apperror.Invalid("date phải có dạng YYYY-MM-DD", nil), requestID)
			return
		}
		day = t
	}

	detail, err := h.uc.GetDay(r.Context(), actor, employeeID, day)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	sessions := make([]sessionDTO, 0, len(detail.Sessions))
	for _, s := range detail.Sessions {
		sessions = append(sessions, toSessionDTO(s))
	}

	httpx.OK(w, map[string]any{
		"day":      toDayDTO(detail.Day),
		"sessions": sessions,
	})
}

func (h *AttendanceHandler) MonthSummary(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()

	now := time.Now()
	year := atoiDefault(q.Get("year"), now.Year())
	month := atoiDefault(q.Get("month"), int(now.Month()))

	var employeeID, departmentID *uuid.UUID
	if v := q.Get("employee_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("employee_id không hợp lệ", nil), requestID)
			return
		}
		employeeID = &id
	}
	if v := q.Get("department_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("department_id không hợp lệ", nil), requestID)
			return
		}
		departmentID = &id
	}

	list, err := h.uc.MonthSummary(r.Context(), appmw.ActorFrom(r.Context()),
		year, month, employeeID, departmentID)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, m := range list {
		out = append(out, map[string]any{
			"employee_id":       m.EmployeeID.String(),
			"employee_name":     m.EmployeeName,
			"year":              m.Year,
			"month":             m.Month,
			"workday_count":     m.WorkdayCount,
			"present_days":      m.PresentDays,
			"absent_days":       m.AbsentDays,
			"leave_days":        m.LeaveDays,
			"online_minutes":    m.OnlineMinutes,
			"active_minutes":    m.ActiveMinutes,
			"late_minutes":      m.LateMinutes,
			"late_days":         m.LateDays,
			"shortfall_minutes": m.ShortfallMins,
		})
	}
	httpx.OK(w, out)
}

func (h *AttendanceHandler) TeamPresence(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	list, err := h.uc.TeamPresence(r.Context(), appmw.ActorFrom(r.Context()))
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]string, 0, len(list))
	for _, m := range list {
		out = append(out, map[string]string{
			"employee_id": m.EmployeeID.String(),
			"status":      m.Status,
		})
	}
	httpx.OK(w, out)
}

type checkInRequest struct {
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	Note      string `json:"note"`
}

func (h *AttendanceHandler) CheckIn(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req checkInRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	start, err := time.Parse(time.RFC3339, req.StartedAt)
	if err != nil {
		Error(w, apperror.Invalid("started_at phải có dạng RFC3339", nil), requestID)
		return
	}
	end, err := time.Parse(time.RFC3339, req.EndedAt)
	if err != nil {
		Error(w, apperror.Invalid("ended_at phải có dạng RFC3339", nil), requestID)
		return
	}

	s, err := h.uc.CheckIn(r.Context(), appmw.ActorFrom(r.Context()), start, end, req.Note)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toSessionDTO(s))
}

// ------------------------------------------------- điều chỉnh công

type adjustmentRequest struct {
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
	Reason    string `json:"reason"`
}

func (h *AttendanceHandler) CreateAdjustment(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req adjustmentRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	start, err := time.Parse(time.RFC3339, req.StartedAt)
	if err != nil {
		Error(w, apperror.Invalid("started_at phải có dạng RFC3339", nil), requestID)
		return
	}
	end, err := time.Parse(time.RFC3339, req.EndedAt)
	if err != nil {
		Error(w, apperror.Invalid("ended_at phải có dạng RFC3339", nil), requestID)
		return
	}

	a, err := h.uc.CreateAdjustment(r.Context(), appmw.ActorFrom(r.Context()),
		start, end, req.Reason)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toAdjustmentDTO(a))
}

func toAdjustmentDTO(a *domainatt.Adjustment) map[string]any {
	return map[string]any{
		"id":              a.ID.String(),
		"employee_id":     a.EmployeeID.String(),
		"employee_name":   a.EmployeeName,
		"work_date":       a.WorkDate.Format("2006-01-02"),
		"requested_start": a.RequestedStart,
		"requested_end":   a.RequestedEnd,
		"reason":          a.Reason,
		"status":          string(a.Status),
		"approver_name":   a.ApproverName,
		"decided_at":      a.DecidedAt,
		"decision_note":   a.DecisionNote,
		"created_at":      a.CreatedAt,
	}
}

func (h *AttendanceHandler) ListAdjustments(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var status *domainatt.ApprovalStatus
	if v := r.URL.Query().Get("status"); v != "" {
		s := domainatt.ApprovalStatus(v)
		if !s.Valid() {
			Error(w, apperror.Invalid("status không hợp lệ", nil), requestID)
			return
		}
		status = &s
	}

	list, err := h.uc.ListAdjustments(r.Context(), appmw.ActorFrom(r.Context()), status)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, a := range list {
		out = append(out, toAdjustmentDTO(a))
	}
	httpx.OK(w, out)
}

type decisionRequest struct {
	Approve bool   `json:"approve"`
	Note    string `json:"note"`
}

func (h *AttendanceHandler) DecideAdjustment(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req decisionRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	a, err := h.uc.DecideAdjustment(r.Context(), appmw.ActorFrom(r.Context()),
		id, req.Approve, req.Note)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toAdjustmentDTO(a))
}

// ------------------------------------------------------- nghỉ phép

type leaveRequestBody struct {
	Type      string `json:"leave_type"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
	DayPart   string `json:"day_part"`
	Reason    string `json:"reason"`
}

func (h *AttendanceHandler) CreateLeave(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req leaveRequestBody
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	start, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		Error(w, apperror.Invalid("start_date phải có dạng YYYY-MM-DD", nil), requestID)
		return
	}
	end, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		Error(w, apperror.Invalid("end_date phải có dạng YYYY-MM-DD", nil), requestID)
		return
	}

	l, err := h.uc.CreateLeave(r.Context(), appmw.ActorFrom(r.Context()), ucatt.LeaveInput{
		Type:      domainatt.LeaveType(req.Type),
		StartDate: start,
		EndDate:   end,
		DayPart:   domainatt.DayPart(req.DayPart),
		Reason:    req.Reason,
	})
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toLeaveDTO(l))
}

func (h *AttendanceHandler) ListLeaves(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()

	f := domainatt.LeaveFilter{
		Page:     atoiDefault(q.Get("page"), 1),
		PageSize: atoiDefault(q.Get("page_size"), 20),
	}

	if v := q.Get("employee_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("employee_id không hợp lệ", nil), requestID)
			return
		}
		f.EmployeeID = &id
	}
	if v := q.Get("status"); v != "" {
		s := domainatt.ApprovalStatus(v)
		if !s.Valid() {
			Error(w, apperror.Invalid("status không hợp lệ", nil), requestID)
			return
		}
		f.Status = &s
	}
	if v := q.Get("leave_type"); v != "" {
		t := domainatt.LeaveType(v)
		if !t.Valid() {
			Error(w, apperror.Invalid("leave_type không hợp lệ", nil), requestID)
			return
		}
		f.Type = &t
	}

	res, err := h.uc.ListLeaves(r.Context(), appmw.ActorFrom(r.Context()), f)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	items := make([]leaveDTO, 0, len(res.Items))
	for _, l := range res.Items {
		items = append(items, toLeaveDTO(l))
	}

	httpx.Paginated(w, items, map[string]int{
		"page":        res.Page,
		"page_size":   res.PageSize,
		"total_items": res.TotalItems,
		"total_pages": res.TotalPages,
	})
}

func (h *AttendanceHandler) DecideLeave(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req decisionRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	l, err := h.uc.DecideLeave(r.Context(), appmw.ActorFrom(r.Context()),
		id, req.Approve, req.Note)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toLeaveDTO(l))
}

func (h *AttendanceHandler) CancelLeave(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.CancelLeave(r.Context(), appmw.ActorFrom(r.Context()), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã huỷ đơn nghỉ phép"})
}

// ------------------------------------------------------- quỹ phép

func toBalanceDTO(b *domainatt.Balance) map[string]any {
	return map[string]any{
		"employee_id":       b.EmployeeID.String(),
		"employee_name":     b.EmployeeName,
		"year":              b.Year,
		"entitled_days":     b.EntitledDays,
		"carried_over_days": b.CarriedOverDays,
		"used_days":         b.UsedDays,
		"remaining_days":    b.Remaining(),
	}
}

func (h *AttendanceHandler) MyBalance(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	actor := appmw.ActorFrom(r.Context())

	year := atoiDefault(r.URL.Query().Get("year"), time.Now().Year())

	employeeID := actor.EmployeeID
	if v := r.URL.Query().Get("employee_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("employee_id không hợp lệ", nil), requestID)
			return
		}
		employeeID = id
	}

	b, err := h.uc.GetBalance(r.Context(), actor, employeeID, year)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toBalanceDTO(b))
}

func (h *AttendanceHandler) ListBalances(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	year := atoiDefault(r.URL.Query().Get("year"), time.Now().Year())

	list, err := h.uc.ListBalances(r.Context(), appmw.ActorFrom(r.Context()), year)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, b := range list {
		out = append(out, toBalanceDTO(b))
	}
	httpx.OK(w, out)
}

type balanceRequest struct {
	EmployeeID      string  `json:"employee_id"`
	Year            int     `json:"year"`
	EntitledDays    float64 `json:"entitled_days"`
	CarriedOverDays float64 `json:"carried_over_days"`
}

func (h *AttendanceHandler) SetBalance(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req balanceRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	employeeID, err := uuid.Parse(req.EmployeeID)
	if err != nil {
		Error(w, apperror.Invalid("employee_id không hợp lệ", nil), requestID)
		return
	}
	if req.Year == 0 {
		req.Year = time.Now().Year()
	}

	b, err := h.uc.SetBalance(r.Context(), employeeID, req.Year,
		req.EntitledDays, req.CarriedOverDays)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toBalanceDTO(b))
}

// -------------------------------------------- khung giờ, ngày lễ, khoá kỳ

func toScheduleDTO(s *domainatt.Schedule) map[string]any {
	return map[string]any{
		"id":               s.ID.String(),
		"scope":            s.Scope(),
		"department_id":    uuidPtrToString(s.DepartmentID),
		"employee_id":      uuidPtrToString(s.EmployeeID),
		"name":             s.Name,
		"work_start":       s.WorkStart,
		"work_end":         s.WorkEnd,
		"workdays":         s.Workdays,
		"break_minutes":    s.BreakMinutes,
		"grace_minutes":    s.GraceMinutes,
		"expected_minutes": s.ExpectedMinutes(),
		"effective_from":   s.EffectiveFrom.Format("2006-01-02"),
	}
}

func (h *AttendanceHandler) ListSchedules(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	list, err := h.uc.ListSchedules(r.Context())
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, toScheduleDTO(s))
	}
	httpx.OK(w, out)
}

type scheduleRequestBody struct {
	DepartmentID  *string `json:"department_id"`
	EmployeeID    *string `json:"employee_id"`
	Name          string  `json:"name"`
	WorkStart     string  `json:"work_start"`
	WorkEnd       string  `json:"work_end"`
	Workdays      []int16 `json:"workdays"`
	BreakMinutes  int     `json:"break_minutes"`
	GraceMinutes  int     `json:"grace_minutes"`
	EffectiveFrom *string `json:"effective_from"`
}

func (req scheduleRequestBody) toInput() (ucatt.ScheduleInput, error) {
	in := ucatt.ScheduleInput{
		Name:         req.Name,
		WorkStart:    req.WorkStart,
		WorkEnd:      req.WorkEnd,
		Workdays:     req.Workdays,
		BreakMinutes: req.BreakMinutes,
		GraceMinutes: req.GraceMinutes,
	}
	var err error
	if in.DepartmentID, err = parseUUIDPtr(req.DepartmentID); err != nil {
		return in, apperror.Invalid("department_id không hợp lệ", nil)
	}
	if in.EmployeeID, err = parseUUIDPtr(req.EmployeeID); err != nil {
		return in, apperror.Invalid("employee_id không hợp lệ", nil)
	}
	if req.EffectiveFrom != nil && *req.EffectiveFrom != "" {
		t, err := time.Parse("2006-01-02", *req.EffectiveFrom)
		if err != nil {
			return in, apperror.Invalid("effective_from phải có dạng YYYY-MM-DD", nil)
		}
		in.EffectiveFrom = t
	}
	return in, nil
}

func (h *AttendanceHandler) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req scheduleRequestBody
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}

	s, err := h.uc.CreateSchedule(r.Context(), in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toScheduleDTO(s))
}

func (h *AttendanceHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req scheduleRequestBody
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	in, err := req.toInput()
	if err != nil {
		Error(w, err, requestID)
		return
	}

	s, err := h.uc.UpdateSchedule(r.Context(), id, in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toScheduleDTO(s))
}

func (h *AttendanceHandler) DeleteSchedule(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.DeleteSchedule(r.Context(), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã xoá khung giờ làm việc"})
}

func (h *AttendanceHandler) ListHolidays(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	year := atoiDefault(r.URL.Query().Get("year"), time.Now().Year())

	list, err := h.uc.ListHolidays(r.Context(), year)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, hd := range list {
		out = append(out, map[string]any{
			"id":      hd.ID.String(),
			"date":    hd.Date.Format("2006-01-02"),
			"name":    hd.Name,
			"is_paid": hd.IsPaid,
		})
	}
	httpx.OK(w, out)
}

type holidayRequest struct {
	Date   string `json:"date"`
	Name   string `json:"name"`
	IsPaid *bool  `json:"is_paid"`
}

func (h *AttendanceHandler) CreateHoliday(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req holidayRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		Error(w, apperror.Invalid("date phải có dạng YYYY-MM-DD", nil), requestID)
		return
	}
	isPaid := true
	if req.IsPaid != nil {
		isPaid = *req.IsPaid
	}

	hd, err := h.uc.CreateHoliday(r.Context(), date, req.Name, isPaid)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, map[string]any{
		"id":      hd.ID.String(),
		"date":    hd.Date.Format("2006-01-02"),
		"name":    hd.Name,
		"is_paid": hd.IsPaid,
	})
}

func (h *AttendanceHandler) DeleteHoliday(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.DeleteHoliday(r.Context(), id); err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]string{"status": "đã xoá ngày lễ"})
}

type lockRequest struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Locked bool   `json:"locked"`
}

func (h *AttendanceHandler) LockPeriod(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	var req lockRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}
	from, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		Error(w, apperror.Invalid("from phải có dạng YYYY-MM-DD", nil), requestID)
		return
	}
	to, err := time.Parse("2006-01-02", req.To)
	if err != nil {
		Error(w, apperror.Invalid("to phải có dạng YYYY-MM-DD", nil), requestID)
		return
	}

	n, err := h.uc.LockPeriod(r.Context(), from, to, req.Locked)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, map[string]any{"affected_days": n, "locked": req.Locked})
}
