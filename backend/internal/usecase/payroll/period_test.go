package payroll

import (
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"context"

	"github.com/google/uuid"

	domainpay "github.com/PhamVanPhuc2k2/manage/internal/domain/payroll"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// Bộ kiểm thử vòng đời kỳ lương.
//
// Mỗi luật ở đây tồn tại để chặn một cách làm sai số liệu tiền lương mà hệ
// thống không tự phát hiện được: tính lương cho tháng chưa kết thúc, khoá
// một kỳ rỗng, hay để kỳ kẹt vĩnh viễn ở trạng thái "đang tính".

func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	code, _ := apperror.HTTPStatus(err)
	return code
}

// nowAfterAugust là một thời điểm sau khi tháng 8/2026 đã kết thúc, để kỳ
// lương tháng 8 luôn tạo được.
func nowAfterAugust() time.Time {
	return time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
}

// =========================================================================
// TẠO KỲ LƯƠNG
// =========================================================================

func TestCreatePeriodRejectsBadPeriod(t *testing.T) {
	cases := []struct {
		name  string
		year  int
		month int
	}{
		{"tháng 0", 2026, 0},
		{"tháng 13", 2026, 13},
		{"năm quá xa quá khứ", 1999, 1},
		{"năm quá xa tương lai", 2201, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(nowAfterAugust())

			_, err := h.uc.CreatePeriod(context.Background(),
				payActor(uuid.New()), tc.year, tc.month, "")

			if got := statusOf(err); got != http.StatusBadRequest {
				t.Errorf("mã lỗi = %d, muốn 400", got)
			}
		})
	}
}

// TestCreatePeriodRejectsUnfinishedMonth.
//
// Dữ liệu công của tháng đang chạy còn thay đổi mỗi ngày, nên bảng lương
// tính ra sẽ sai ngay khi vừa tính xong. Chặn ở đây thay vì để người dùng
// phát hiện sau khi đã gửi phiếu lương đi.
func TestCreatePeriodRejectsUnfinishedMonth(t *testing.T) {
	now := nowAfterAugust() // 15/09/2026
	h := newHarness(now)

	_, err := h.uc.CreatePeriod(context.Background(),
		payActor(uuid.New()), 2026, 9, "")

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.periods.created) != 0 {
		t.Error("đã tạo kỳ lương cho tháng chưa kết thúc")
	}
}

// TestCreatePeriodAcceptsFinishedMonth là phía ngược lại: luật trên không
// được chặn nhầm tháng đã qua.
func TestCreatePeriodAcceptsFinishedMonth(t *testing.T) {
	h := newHarness(nowAfterAugust())

	got, err := h.uc.CreatePeriod(context.Background(),
		payActor(uuid.New()), 2026, 8, "")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got.PeriodStart.Format("2006-01-02") != "2026-08-01" {
		t.Errorf("ngày đầu kỳ = %s, muốn 2026-08-01",
			got.PeriodStart.Format("2006-01-02"))
	}
	if got.PeriodEnd.Format("2006-01-02") != "2026-08-31" {
		t.Errorf("ngày cuối kỳ = %s, muốn 2026-08-31",
			got.PeriodEnd.Format("2006-01-02"))
	}
}

func TestCreatePeriodRejectsDuplicateMonth(t *testing.T) {
	h := newHarness(nowAfterAugust())
	h.periods.forMonth[monthKey(2026, 8)] = true

	_, err := h.uc.CreatePeriod(context.Background(),
		payActor(uuid.New()), 2026, 8, "")

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

func TestCreatePeriodGeneratesName(t *testing.T) {
	h := newHarness(nowAfterAugust())

	got, err := h.uc.CreatePeriod(context.Background(),
		payActor(uuid.New()), 2026, 8, "   ")
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.Name != "Lương tháng 08/2026" {
		t.Errorf("tên kỳ = %q, muốn %q", got.Name, "Lương tháng 08/2026")
	}
}

// =========================================================================
// ĐẨY VIỆC TÍNH LƯƠNG
// =========================================================================

// TestRequestCalculationMarksCalculatingBeforePublishing.
//
// Đổi trạng thái TRƯỚC khi đẩy việc: giao diện phải thấy ngay là đang chạy,
// và chính trạng thái này chặn người thứ hai bấm tính lại.
func TestRequestCalculationMarksCalculatingBeforePublishing(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p := h.periods.add(&domainpay.Period{
		CompanyID: h.company.id, Year: 2026, Month: 8,
		Status: domainpay.StatusDraft,
	})

	if err := h.uc.RequestCalculation(
		context.Background(), payActor(uuid.New()), p.ID,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if got := h.periods.byID[p.ID].Status; got != domainpay.StatusCalculating {
		t.Errorf("trạng thái = %q, muốn %q", got, domainpay.StatusCalculating)
	}
	if len(h.jobs.published) != 1 {
		t.Errorf("số việc đã đẩy = %d, muốn 1", len(h.jobs.published))
	}
}

// TestRequestCalculationRollsBackOnPublishFailure.
//
// Đẩy việc hỏng mà không trả trạng thái về nháp thì kỳ lương kẹt ở "đang
// tính" vĩnh viễn — không có ai chạy tiếp để đổi nó, và giao diện không cho
// bấm lại vì tưởng đang chạy. Phải sửa tay trong database mới thoát ra được.
func TestRequestCalculationRollsBackOnPublishFailure(t *testing.T) {
	h := newHarness(nowAfterAugust())
	h.jobs.err = errors.New("RabbitMQ sập")

	p := h.periods.add(&domainpay.Period{
		CompanyID: h.company.id, Year: 2026, Month: 8,
		Status: domainpay.StatusDraft,
	})

	err := h.uc.RequestCalculation(
		context.Background(), payActor(uuid.New()), p.ID)
	if err == nil {
		t.Fatal("phải trả lỗi khi không đẩy được việc")
	}

	if got := h.periods.byID[p.ID].Status; got != domainpay.StatusDraft {
		t.Errorf("trạng thái = %q, muốn quay về %q", got, domainpay.StatusDraft)
	}
}

func TestRequestCalculationRejectsLockedPeriod(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p := h.periods.add(&domainpay.Period{
		CompanyID: h.company.id, Year: 2026, Month: 8,
		Status: domainpay.StatusLocked,
	})

	err := h.uc.RequestCalculation(
		context.Background(), payActor(uuid.New()), p.ID)

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.jobs.published) != 0 {
		t.Error("đã đẩy việc tính lại cho kỳ đã khoá")
	}
}

func TestRequestCalculationOnMissingPeriodReturns404(t *testing.T) {
	h := newHarness(nowAfterAugust())

	err := h.uc.RequestCalculation(
		context.Background(), payActor(uuid.New()), uuid.New())

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// MÁY TÍNH LƯƠNG
// =========================================================================

// periodForCalc dựng một kỳ lương kèm hai nhân viên có cấu hình lương.
func periodForCalc(h *harness) (*domainpay.Period, uuid.UUID, uuid.UUID) {
	a, b := uuid.New(), uuid.New()
	h.employees.activeIDs = []uuid.UUID{a, b}

	for _, id := range []uuid.UUID{a, b} {
		h.structures.effective[id] = &domainpay.Structure{
			ID: uuid.New(), EmployeeID: id, BaseSalary: money(20_000_000),
		}
		h.attendance.workdays[id] = domainpay.Workdays{Present: 22}
	}

	p := h.periods.add(&domainpay.Period{
		CompanyID:   h.company.id,
		Year:        2026,
		Month:       8,
		Status:      domainpay.StatusDraft,
		PeriodStart: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
	})
	return p, a, b
}

func TestCalculateProducesOnePayslipPerEmployee(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p, _, _ := periodForCalc(h)

	res, err := h.uc.Calculate(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if res.Processed != 2 || res.Skipped != 0 {
		t.Errorf("đã tính %d, bỏ qua %d; muốn 2 và 0", res.Processed, res.Skipped)
	}
	if len(h.payslips.replaced) != 1 || len(h.payslips.replaced[0]) != 2 {
		t.Errorf("số phiếu đã ghi = %v, muốn một lượt hai phiếu", h.payslips.replaced)
	}
	if !slices.Contains(h.periods.totalsFor, p.ID) {
		t.Error("chưa cập nhật các số tổng của kỳ")
	}
}

// TestCalculateSkipsEmployeesWithoutStructure.
//
// Chưa cấu hình lương thì BỎ QUA, không tính bừa bằng lương 0. Một phiếu
// lương 0 đồng trông như đã tính xong và rất dễ lọt qua khâu rà soát; một
// người vắng mặt khỏi bảng lương thì kế toán nhìn ra ngay.
func TestCalculateSkipsEmployeesWithoutStructure(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p, a, _ := periodForCalc(h)
	delete(h.structures.effective, a)

	res, err := h.uc.Calculate(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if res.Processed != 1 || res.Skipped != 1 {
		t.Errorf("đã tính %d, bỏ qua %d; muốn 1 và 1", res.Processed, res.Skipped)
	}
	if len(h.payslips.replaced[0]) != 1 {
		t.Errorf("số phiếu = %d, muốn 1 — không được tạo phiếu 0 đồng",
			len(h.payslips.replaced[0]))
	}
}

// TestCalculateUsesSettingsOfThePeriodNotToday.
//
// Tham số có hiệu lực TẠI KỲ đó, không phải tham số mới nhất. Tính lại kỳ
// tháng trước phải cho ra đúng con số của tháng trước — nếu không, mỗi lần
// luật thay đổi là toàn bộ lịch sử lương thay đổi theo.
func TestCalculateUsesSettingsOfThePeriodNotToday(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p, _, _ := periodForCalc(h)

	if _, err := h.uc.Calculate(context.Background(), p.ID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !h.settings.lastAt.Equal(p.PeriodEnd) {
		t.Errorf("tham số lấy tại thời điểm %v, muốn cuối kỳ %v",
			h.settings.lastAt, p.PeriodEnd)
	}
}

func TestCalculateRejectsLockedPeriod(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p, _, _ := periodForCalc(h)
	h.periods.byID[p.ID].Status = domainpay.StatusLocked

	_, err := h.uc.Calculate(context.Background(), p.ID)
	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
	if len(h.payslips.replaced) != 0 {
		t.Error("đã ghi đè phiếu lương của kỳ đã khoá")
	}
}

func TestCalculateWithoutSettingsIsUnprocessable(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p, _, _ := periodForCalc(h)
	h.settings.current = nil

	_, err := h.uc.Calculate(context.Background(), p.ID)
	if got := statusOf(err); got != http.StatusUnprocessableEntity {
		t.Errorf("mã lỗi = %d, muốn 422", got)
	}
}

func TestCalculateWithoutEmployeesIsUnprocessable(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p, _, _ := periodForCalc(h)
	h.employees.activeIDs = nil

	_, err := h.uc.Calculate(context.Background(), p.ID)
	if got := statusOf(err); got != http.StatusUnprocessableEntity {
		t.Errorf("mã lỗi = %d, muốn 422", got)
	}
}

// TestCalculateReturnsToDraft: chạy xong phải quay về nháp để kế toán rà
// soát và sửa, không tự nhảy sang đã khoá.
func TestCalculateReturnsToDraft(t *testing.T) {
	now := nowAfterAugust()
	h := newHarness(now)
	p, _, _ := periodForCalc(h)
	h.periods.byID[p.ID].Status = domainpay.StatusCalculating

	if _, err := h.uc.Calculate(context.Background(), p.ID); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	got := h.periods.byID[p.ID]
	if got.Status != domainpay.StatusDraft {
		t.Errorf("trạng thái = %q, muốn %q", got.Status, domainpay.StatusDraft)
	}
	if got.CalculatedAt == nil || !got.CalculatedAt.Equal(now) {
		t.Errorf("mốc đã tính = %v, muốn %v", got.CalculatedAt, now)
	}
}

// TestCalculateIsRepeatable: chạy lại phải cho ra đúng một bộ kết quả, không
// cộng dồn với lần trước.
func TestCalculateIsRepeatable(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p, _, _ := periodForCalc(h)

	first, err := h.uc.Calculate(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("lần một lỗi: %v", err)
	}
	second, err := h.uc.Calculate(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("lần hai lỗi: %v", err)
	}

	if first.TotalNet != second.TotalNet || first.Processed != second.Processed {
		t.Errorf("hai lần chạy khác nhau: %+v và %+v", first, second)
	}
	if len(h.payslips.replaced[1]) != 2 {
		t.Errorf("lần hai ghi %d phiếu, muốn 2", len(h.payslips.replaced[1]))
	}
}

// =========================================================================
// CHUYỂN TRẠNG THÁI
// =========================================================================

func TestChangeStatusFollowsLifecycle(t *testing.T) {
	cases := []struct {
		name string
		from domainpay.Status
		to   domainpay.Status
		want int
	}{
		{"nháp → đã khoá", domainpay.StatusDraft, domainpay.StatusLocked, http.StatusOK},
		{"đã khoá → đã trả", domainpay.StatusLocked, domainpay.StatusPaid, http.StatusOK},
		{"đã khoá → nháp", domainpay.StatusLocked, domainpay.StatusDraft, http.StatusOK},
		{"nháp → đã trả", domainpay.StatusDraft, domainpay.StatusPaid, http.StatusBadRequest},
		{"đã trả → nháp", domainpay.StatusPaid, domainpay.StatusDraft, http.StatusBadRequest},
		{"đã trả → đã khoá", domainpay.StatusPaid, domainpay.StatusLocked, http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(nowAfterAugust())
			p := h.periods.add(&domainpay.Period{
				CompanyID: h.company.id, Year: 2026, Month: 8,
				Status: tc.from, EmployeeCount: 5,
			})

			_, err := h.uc.ChangeStatus(
				context.Background(), payActor(uuid.New()), p.ID, tc.to)

			if got := statusOf(err); got != tc.want {
				t.Errorf("mã lỗi = %d, muốn %d (%v)", got, tc.want, err)
			}
		})
	}
}

func TestChangeStatusRejectsUnknownStatus(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p := h.periods.add(&domainpay.Period{
		CompanyID: h.company.id, Status: domainpay.StatusDraft, EmployeeCount: 5,
	})

	_, err := h.uc.ChangeStatus(
		context.Background(), payActor(uuid.New()), p.ID, "đã-xong-rồi")

	if got := statusOf(err); got != http.StatusBadRequest {
		t.Errorf("mã lỗi = %d, muốn 400", got)
	}
}

// TestCannotLockEmptyPeriod: khoá một kỳ rỗng nghĩa là chốt xong một tháng
// không trả lương cho ai, và sau khi khoá thì không tính được nữa.
func TestCannotLockEmptyPeriod(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p := h.periods.add(&domainpay.Period{
		CompanyID: h.company.id, Status: domainpay.StatusDraft, EmployeeCount: 0,
	})

	_, err := h.uc.ChangeStatus(
		context.Background(), payActor(uuid.New()), p.ID, domainpay.StatusLocked)

	if got := statusOf(err); got != http.StatusConflict {
		t.Errorf("mã lỗi = %d, muốn 409", got)
	}
}

// TestUnlockClearsLockTimestamp: mở khoá phải xoá mốc khoá, nếu không lịch
// sử nói dối — kỳ đang ở trạng thái nháp nhưng vẫn ghi "đã khoá lúc ...".
func TestUnlockClearsLockTimestamp(t *testing.T) {
	now := nowAfterAugust()
	locked := now.Add(-time.Hour)

	h := newHarness(now)
	p := h.periods.add(&domainpay.Period{
		CompanyID: h.company.id, Status: domainpay.StatusLocked,
		EmployeeCount: 5, LockedAt: &locked,
	})

	if _, err := h.uc.ChangeStatus(
		context.Background(), payActor(uuid.New()), p.ID, domainpay.StatusDraft,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if h.periods.byID[p.ID].LockedAt != nil {
		t.Error("mốc khoá chưa được xoá khi mở khoá")
	}
}

func TestLockAndPayRecordTimestamps(t *testing.T) {
	now := nowAfterAugust()

	t.Run("khoá", func(t *testing.T) {
		h := newHarness(now)
		p := h.periods.add(&domainpay.Period{
			CompanyID: h.company.id, Status: domainpay.StatusDraft, EmployeeCount: 5,
		})

		if _, err := h.uc.ChangeStatus(context.Background(),
			payActor(uuid.New()), p.ID, domainpay.StatusLocked,
		); err != nil {
			t.Fatalf("lỗi không mong đợi: %v", err)
		}
		if got := h.periods.byID[p.ID].LockedAt; got == nil || !got.Equal(now) {
			t.Errorf("mốc khoá = %v, muốn %v", got, now)
		}
	})

	t.Run("đã trả", func(t *testing.T) {
		h := newHarness(now)
		p := h.periods.add(&domainpay.Period{
			CompanyID: h.company.id, Status: domainpay.StatusLocked, EmployeeCount: 5,
		})

		if _, err := h.uc.ChangeStatus(context.Background(),
			payActor(uuid.New()), p.ID, domainpay.StatusPaid,
		); err != nil {
			t.Fatalf("lỗi không mong đợi: %v", err)
		}
		if got := h.periods.byID[p.ID].PaidAt; got == nil || !got.Equal(now) {
			t.Errorf("mốc đã trả = %v, muốn %v", got, now)
		}
	})
}

// TestChangeStatusIsAudited: chốt và xác nhận đã trả là hai thao tác có hệ
// quả tài chính. Không ghi lại ai làm thì khi đối soát sai không truy được.
func TestChangeStatusIsAudited(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p := h.periods.add(&domainpay.Period{
		CompanyID: h.company.id, Status: domainpay.StatusDraft, EmployeeCount: 5,
	})

	if _, err := h.uc.ChangeStatus(context.Background(),
		payActor(uuid.New()), p.ID, domainpay.StatusLocked,
	); err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}

	if !slices.Contains(h.audit.actions(), domainpay.AuditLockPeriod) {
		t.Errorf("nhật ký = %v, phải có %q",
			h.audit.actions(), domainpay.AuditLockPeriod)
	}
}

func TestChangeStatusOnMissingPeriodReturns404(t *testing.T) {
	h := newHarness(nowAfterAugust())

	_, err := h.uc.ChangeStatus(context.Background(),
		payActor(uuid.New()), uuid.New(), domainpay.StatusLocked)

	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// =========================================================================
// ĐỌC
// =========================================================================

func TestListAndGetPeriod(t *testing.T) {
	h := newHarness(nowAfterAugust())
	p := h.periods.add(&domainpay.Period{
		CompanyID: h.company.id, Year: 2026, Month: 8,
		Status: domainpay.StatusDraft,
	})

	list, err := h.uc.ListPeriods(context.Background())
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("số kỳ = %d, muốn 1", len(list))
	}

	got, err := h.uc.GetPeriod(context.Background(), p.ID)
	if err != nil {
		t.Fatalf("lỗi không mong đợi: %v", err)
	}
	if got.ID != p.ID {
		t.Errorf("trả về %v, muốn %v", got.ID, p.ID)
	}
}

func TestGetMissingPeriodReturns404(t *testing.T) {
	h := newHarness(nowAfterAugust())

	_, err := h.uc.GetPeriod(context.Background(), uuid.New())
	if got := statusOf(err); got != http.StatusNotFound {
		t.Errorf("mã lỗi = %d, muốn 404", got)
	}
}

// TestStatusLabelCoversEveryStatus: nhãn thiếu sẽ làm thông báo lỗi thành
// `Không chuyển được từ "" sang ""` — vô dụng với người đọc.
func TestStatusLabelCoversEveryStatus(t *testing.T) {
	all := []domainpay.Status{
		domainpay.StatusDraft, domainpay.StatusCalculating,
		domainpay.StatusLocked, domainpay.StatusPaid, domainpay.StatusCancelled,
	}

	for _, s := range all {
		if statusLabel(s) == "" {
			t.Errorf("trạng thái %q không có nhãn tiếng Việt", s)
		}
	}
}
