package handler

import (
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	ucpay "github.com/PhamVanPhuc2k2/manage/internal/usecase/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

type PayrollHandler struct {
	uc *ucpay.Usecase
}

func NewPayrollHandler(uc *ucpay.Usecase) *PayrollHandler {
	return &PayrollHandler{uc: uc}
}

// withAudit gắn IP và request id vào context để usecase ghi nhật ký.
//
// Làm ở tầng delivery vì đó là nơi duy nhất biết về HTTP. Usecase chỉ nhận
// một context đã đủ thông tin, và chữ ký hàm của nó không có chữ "IP".
func withAudit(r *http.Request) *http.Request {
	ctx := ucpay.WithAuditMeta(r.Context(), clientIP(r), chimw.GetReqID(r.Context()))
	return r.WithContext(ctx)
}

// --------------------------------------------------------------- DTO

func toPeriodDTO(p *domainpay.Period) map[string]any {
	return map[string]any{
		"id":              p.ID.String(),
		"year":            p.Year,
		"month":           p.Month,
		"name":            p.Name,
		"period_start":    p.PeriodStart.Format("2006-01-02"),
		"period_end":      p.PeriodEnd.Format("2006-01-02"),
		"status":          string(p.Status),
		"total_gross":     int64(p.TotalGross),
		"total_net":       int64(p.TotalNet),
		"total_tax":       int64(p.TotalTax),
		"total_insurance": int64(p.TotalInsurance),
		"employee_count":  p.EmployeeCount,
		"calculated_at":   p.CalculatedAt,
		"locked_at":       p.LockedAt,
		"paid_at":         p.PaidAt,
		"created_at":      p.CreatedAt,
	}
}

func toPayslipDTO(s *domainpay.Payslip) map[string]any {
	items := make([]map[string]any, 0, len(s.Items))
	for _, it := range s.Items {
		items = append(items, map[string]any{
			"kind":    string(it.Kind),
			"code":    it.Code,
			"name":    it.Name,
			"amount":  int64(it.Amount),
			"taxable": it.Taxable,
		})
	}

	return map[string]any{
		"id":              s.ID.String(),
		"period_id":       s.PeriodID.String(),
		"period_name":     s.PeriodName,
		"period_year":     s.PeriodYear,
		"period_month":    s.PeriodMonth,
		"employee_id":     s.EmployeeID.String(),
		"employee_name":   s.EmployeeName,
		"employee_code":   s.EmployeeCode,
		"department_name": s.DepartmentName,
		"position_name":   s.PositionName,

		"standard_workdays": s.StandardWorkdays,
		"actual_workdays":   s.ActualWorkdays,
		"leave_days":        s.LeaveDays,
		"absent_days":       s.AbsentDays,

		"base_salary":  int64(s.BaseSalary),
		"allowances":   int64(s.Allowances),
		"bonuses":      int64(s.Bonuses),
		"gross_salary": int64(s.GrossSalary),

		"insurance_base":     int64(s.InsuranceBase),
		"insurance_employee": int64(s.InsuranceEmployee),
		"insurance_employer": int64(s.InsuranceEmployer),

		"taxable_income":      int64(s.TaxableIncome),
		"personal_deduction":  int64(s.PersonalDeduction),
		"dependent_deduction": int64(s.DependentDeduction),
		"assessable_income":   int64(s.AssessableIncome),
		"income_tax":          int64(s.IncomeTax),

		"other_deductions": int64(s.OtherDeductions),
		"net_salary":       int64(s.NetSalary),
		"dependents":       s.Dependents,
		"note":             s.Note,

		// Số tài khoản đã che từ tầng repository — số đầy đủ không bao giờ
		// đi vào entity nên không có đường lọt ra đây.
		"bank_account": s.BankAccountMasked,
		"bank_name":    s.BankName,

		"items": items,
	}
}

func toStructureDTO(s *domainpay.Structure) map[string]any {
	items := make([]map[string]any, 0, len(s.Components))
	for _, c := range s.Components {
		items = append(items, map[string]any{
			"kind":     string(c.Kind),
			"code":     c.Code,
			"name":     c.Name,
			"amount":   int64(c.Amount),
			"taxable":  c.Taxable,
			"prorated": c.Prorated,
		})
	}

	var insurance *int64
	if s.InsuranceSalary != nil {
		v := int64(*s.InsuranceSalary)
		insurance = &v
	}

	return map[string]any{
		"id":               s.ID.String(),
		"employee_id":      s.EmployeeID.String(),
		"employee_name":    s.EmployeeName,
		"employee_code":    s.EmployeeCode,
		"department_name":  s.DepartmentName,
		"base_salary":      int64(s.BaseSalary),
		"insurance_salary": insurance,
		"dependents":       s.Dependents,
		"bank_account":     maskBankAccount(s.BankAccount),
		"bank_name":        s.BankName,
		"effective_from":   s.EffectiveFrom.Format("2006-01-02"),
		"effective_to":     formatDatePtr(s.EffectiveTo),
		"note":             s.Note,
		"components":       items,
	}
}

// maskBankAccount che số tài khoản ở tầng delivery.
//
// Lớp che THỨ HAI, sau lớp ở repository. Cấu hình lương đọc số đầy đủ (kế
// toán cần nó để chuyển khoản), nên API phải tự che — dữ liệu nhạy cảm nên
// có nhiều hơn một lớp chặn.
func maskBankAccount(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 4 {
		return "****"
	}
	return "****" + v[len(v)-4:]
}

func formatDatePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("2006-01-02")
	return &s
}

// ----------------------------------------------------------- kỳ lương

func (h *PayrollHandler) ListPeriods(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	list, err := h.uc.ListPeriods(r.Context())
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, toPeriodDTO(p))
	}
	httpx.OK(w, out)
}

func (h *PayrollHandler) GetPeriod(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	p, err := h.uc.GetPeriod(r.Context(), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toPeriodDTO(p))
}

type periodRequest struct {
	Year  int    `json:"year"`
	Month int    `json:"month"`
	Name  string `json:"name"`
}

func (h *PayrollHandler) CreatePeriod(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	var req periodRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	p, err := h.uc.CreatePeriod(r.Context(), appmw.ActorFrom(r.Context()),
		req.Year, req.Month, req.Name)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toPeriodDTO(p))
}

func (h *PayrollHandler) Calculate(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	if err := h.uc.RequestCalculation(r.Context(), appmw.ActorFrom(r.Context()), id); err != nil {
		Error(w, err, requestID)
		return
	}

	// 202 Accepted, không phải 200: việc đã được NHẬN chứ chưa xong. Trả 200
	// sẽ khiến giao diện tưởng bảng lương đã sẵn sàng và hiện ra con số cũ.
	httpx.JSON(w, http.StatusAccepted, httpx.Envelope{Data: map[string]string{
		"status": "đã nhận yêu cầu tính lương, đang xử lý nền",
	}})
}

type statusRequest struct {
	Status string `json:"status"`
}

func (h *PayrollHandler) ChangeStatus(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req statusRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	p, err := h.uc.ChangeStatus(r.Context(), appmw.ActorFrom(r.Context()),
		id, domainpay.Status(req.Status))
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toPeriodDTO(p))
}

// -------------------------------------------------------- phiếu lương

func (h *PayrollHandler) ListPayslips(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()

	f := domainpay.PayslipFilter{
		Page:     atoiDefault(q.Get("page"), 1),
		PageSize: atoiDefault(q.Get("page_size"), 50),
	}

	// KHÔNG bind ScopedEmployeeIDs từ query — usecase đặt theo quyền actor.
	if v := q.Get("period_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("period_id không hợp lệ", nil), requestID)
			return
		}
		f.PeriodID = &id
	}
	if v := q.Get("department_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("department_id không hợp lệ", nil), requestID)
			return
		}
		f.DepartmentID = &id
	}

	res, err := h.uc.ListPayslips(r.Context(), appmw.ActorFrom(r.Context()), f)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	items := make([]map[string]any, 0, len(res.Items))
	for _, s := range res.Items {
		items = append(items, toPayslipDTO(s))
	}

	httpx.Paginated(w, items, map[string]int{
		"page":        res.Page,
		"page_size":   res.PageSize,
		"total_items": res.TotalItems,
		"total_pages": res.TotalPages,
	})
}

func (h *PayrollHandler) MyPayslips(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	list, err := h.uc.MyPayslips(r.Context(), appmw.ActorFrom(r.Context()))
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, toPayslipDTO(s))
	}
	httpx.OK(w, out)
}

func (h *PayrollHandler) GetPayslip(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	s, err := h.uc.GetPayslip(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toPayslipDTO(s))
}

// Document trả về phiếu lương ở dạng HTML in được.
//
// Trả HTML thẳng chứ không bọc trong JSON: trình duyệt mở nó như một trang
// và người dùng bấm "In / Lưu thành PDF" ngay tại đó.
func (h *PayrollHandler) Document(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	doc, err := h.uc.RenderPayslip(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Không cho cache: phiếu lương là dữ liệu cá nhân, và một bản lưu trong
	// cache của proxy là một bản sao không ai kiểm soát.
	w.Header().Set("Cache-Control", "no-store, private")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(doc.HTML))
}

type payslipUpdateRequest struct {
	Bonuses         int64  `json:"bonuses"`
	OtherDeductions int64  `json:"other_deductions"`
	Note            string `json:"note"`
}

func (h *PayrollHandler) UpdatePayslip(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req payslipUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	s, err := h.uc.UpdatePayslip(r.Context(), appmw.ActorFrom(r.Context()), id,
		domainpay.Money(req.Bonuses), domainpay.Money(req.OtherDeductions), req.Note)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toPayslipDTO(s))
}

// ----------------------------------------------------- cấu hình lương

func (h *PayrollHandler) GetStructure(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	employeeID, err := parseUUIDParam(r, "employeeID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	s, err := h.uc.GetStructure(r.Context(), appmw.ActorFrom(r.Context()), employeeID)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toStructureDTO(s))
}

func (h *PayrollHandler) StructureHistory(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	employeeID, err := parseUUIDParam(r, "employeeID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	list, err := h.uc.StructureHistory(r.Context(), appmw.ActorFrom(r.Context()), employeeID)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		out = append(out, toStructureDTO(s))
	}
	httpx.OK(w, out)
}

type componentRequest struct {
	Kind     string `json:"kind"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Amount   int64  `json:"amount"`
	Taxable  bool   `json:"taxable"`
	Prorated bool   `json:"prorated"`
}

type structureRequest struct {
	BaseSalary      int64              `json:"base_salary"`
	InsuranceSalary *int64             `json:"insurance_salary"`
	Dependents      int                `json:"dependents"`
	BankAccount     string             `json:"bank_account"`
	BankName        string             `json:"bank_name"`
	EffectiveFrom   string             `json:"effective_from"`
	Note            string             `json:"note"`
	Components      []componentRequest `json:"components"`
}

func (h *PayrollHandler) SetStructure(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	employeeID, err := parseUUIDParam(r, "employeeID")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	var req structureRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	in := ucpay.StructureInput{
		EmployeeID:  employeeID,
		BaseSalary:  domainpay.Money(req.BaseSalary),
		Dependents:  req.Dependents,
		BankAccount: req.BankAccount,
		BankName:    req.BankName,
		Note:        req.Note,
	}
	if req.InsuranceSalary != nil {
		v := domainpay.Money(*req.InsuranceSalary)
		in.InsuranceSalary = &v
	}
	if req.EffectiveFrom != "" {
		t, err := time.Parse("2006-01-02", req.EffectiveFrom)
		if err != nil {
			Error(w, apperror.Invalid("effective_from phải có dạng YYYY-MM-DD", nil), requestID)
			return
		}
		in.EffectiveFrom = t
	}
	for _, c := range req.Components {
		in.Components = append(in.Components, &domainpay.Component{
			Kind:     domainpay.ComponentKind(c.Kind),
			Code:     c.Code,
			Name:     c.Name,
			Amount:   domainpay.Money(c.Amount),
			Taxable:  c.Taxable,
			Prorated: c.Prorated,
		})
	}

	s, err := h.uc.SetStructure(r.Context(), appmw.ActorFrom(r.Context()), in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.Created(w, toStructureDTO(s))
}

// ------------------------------------------------ tham số tính lương

func (h *PayrollHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	s, err := h.uc.GetSettings(r.Context())
	if err != nil {
		Error(w, err, requestID)
		return
	}

	brackets := make([]map[string]any, 0, len(s.Brackets))
	for _, b := range s.Brackets {
		var to *int64
		if b.To != nil {
			v := int64(*b.To)
			to = &v
		}
		brackets = append(brackets, map[string]any{
			"ordinal":     b.Ordinal,
			"from_amount": int64(b.From),
			"to_amount":   to,
			"rate":        b.Rate,
		})
	}

	httpx.OK(w, map[string]any{
		"personal_deduction":         int64(s.PersonalDeduction),
		"dependent_deduction":        int64(s.DependentDeduction),
		"social_rate":                s.SocialRate,
		"health_rate":                s.HealthRate,
		"unemployment_rate":          s.UnemploymentRate,
		"employer_social_rate":       s.EmployerSocialRate,
		"employer_health_rate":       s.EmployerHealthRate,
		"employer_unemployment_rate": s.EmployerUnemploymentRate,
		"social_cap":                 int64(s.SocialCap),
		"unemployment_cap":           int64(s.UnemploymentCap),
		"standard_workdays":          s.StandardWorkdays,
		"tax_brackets":               brackets,
	})
}

type bracketRequest struct {
	FromAmount int64   `json:"from_amount"`
	ToAmount   *int64  `json:"to_amount"`
	Rate       float64 `json:"rate"`
}

type settingsRequest struct {
	PersonalDeduction  int64 `json:"personal_deduction"`
	DependentDeduction int64 `json:"dependent_deduction"`

	SocialRate       float64 `json:"social_rate"`
	HealthRate       float64 `json:"health_rate"`
	UnemploymentRate float64 `json:"unemployment_rate"`

	EmployerSocialRate       float64 `json:"employer_social_rate"`
	EmployerHealthRate       float64 `json:"employer_health_rate"`
	EmployerUnemploymentRate float64 `json:"employer_unemployment_rate"`

	SocialCap       int64 `json:"social_cap"`
	UnemploymentCap int64 `json:"unemployment_cap"`

	StandardWorkdays float64 `json:"standard_workdays"`

	TaxBrackets []bracketRequest `json:"tax_brackets"`
}

func (h *PayrollHandler) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	var req settingsRequest
	if err := decodeJSON(r, &req); err != nil {
		Error(w, err, requestID)
		return
	}

	in := ucpay.SettingsInput{
		PersonalDeduction:        domainpay.Money(req.PersonalDeduction),
		DependentDeduction:       domainpay.Money(req.DependentDeduction),
		SocialRate:               req.SocialRate,
		HealthRate:               req.HealthRate,
		UnemploymentRate:         req.UnemploymentRate,
		EmployerSocialRate:       req.EmployerSocialRate,
		EmployerHealthRate:       req.EmployerHealthRate,
		EmployerUnemploymentRate: req.EmployerUnemploymentRate,
		SocialCap:                domainpay.Money(req.SocialCap),
		UnemploymentCap:          domainpay.Money(req.UnemploymentCap),
		StandardWorkdays:         req.StandardWorkdays,
	}
	for i, b := range req.TaxBrackets {
		br := domainpay.TaxBracket{
			Ordinal: i + 1,
			From:    domainpay.Money(b.FromAmount),
			Rate:    b.Rate,
		}
		if b.ToAmount != nil {
			v := domainpay.Money(*b.ToAmount)
			br.To = &v
		}
		in.Brackets = append(in.Brackets, br)
	}

	s, err := h.uc.UpdateSettings(r.Context(), appmw.ActorFrom(r.Context()), in)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	_ = s
	h.GetSettings(w, r)
}

// ------------------------------------------------------------ báo cáo

func toCostDTO(c *domainpay.CostRow) map[string]any {
	return map[string]any{
		"key":                c.Key,
		"label":              c.Label,
		"employee_count":     c.EmployeeCount,
		"total_gross":        int64(c.TotalGross),
		"total_net":          int64(c.TotalNet),
		"total_tax":          int64(c.TotalTax),
		"insurance_employee": int64(c.InsuranceEmployee),
		"insurance_employer": int64(c.InsuranceEmployer),
		"total_cost":         int64(c.TotalCost()),
	}
}

func (h *PayrollHandler) CostByDepartment(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "id")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	list, err := h.uc.CostByDepartment(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, c := range list {
		out = append(out, toCostDTO(c))
	}
	httpx.OK(w, out)
}

func (h *PayrollHandler) CostByMonth(w http.ResponseWriter, r *http.Request) {
	r = withAudit(r)
	requestID := chimw.GetReqID(r.Context())

	year := atoiDefault(r.URL.Query().Get("year"), time.Now().Year())

	list, err := h.uc.CostByMonth(r.Context(), appmw.ActorFrom(r.Context()), year)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, c := range list {
		out = append(out, toCostDTO(c))
	}
	httpx.OK(w, out)
}

func (h *PayrollHandler) ListAudit(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())
	q := r.URL.Query()

	var resourceID *uuid.UUID
	if v := q.Get("resource_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			Error(w, apperror.Invalid("resource_id không hợp lệ", nil), requestID)
			return
		}
		resourceID = &id
	}

	list, err := h.uc.ListAudit(r.Context(), q.Get("resource"), resourceID)
	if err != nil {
		Error(w, err, requestID)
		return
	}

	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, map[string]any{
			"id":         e.ID.String(),
			"actor_name": e.ActorName,
			"action":     e.Action,
			"resource":   e.Resource,
			"detail":     e.Detail,
			"ip":         e.IP,
			"created_at": e.CreatedAt,
		})
	}
	httpx.OK(w, out)
}
