package attendance

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
)

const (
	sheetSummary = "Tổng hợp"
	sheetDetail  = "Chi tiết"
)

// Ký hiệu trong trang Chi tiết. Một chữ cái cho mỗi ô để cả tháng vừa một
// màn hình; chú thích nằm ngay dưới bảng.
var dayCodes = map[domainatt.DayStatus]string{
	domainatt.DayPresent: "X",
	domainatt.DayAbsent:  "V",
	domainatt.DayLeave:   "P",
	domainatt.DayHoliday: "L",
	domainatt.DayWeekend: "",
}

// buildAttendanceWorkbook dựng tệp .xlsx gồm hai trang:
//
//   - Tổng hợp: mỗi người một dòng, đúng các con số đi vào bảng lương.
//   - Chi tiết: người × ngày, mỗi ô một ký hiệu — trang kế toán in ra để
//     đối chiếu khi có người thắc mắc "sao tôi bị trừ công ngày 12".
func buildAttendanceWorkbook(
	e *domainatt.Export,
	summaries []*domainatt.MonthSummary,
	days []*domainatt.Day,
) ([]byte, error) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := f.SetSheetName(f.GetSheetName(0), sheetSummary); err != nil {
		return nil, err
	}
	if _, err := f.NewSheet(sheetDetail); err != nil {
		return nil, err
	}

	st, err := newWorkbookStyles(f)
	if err != nil {
		return nil, err
	}

	// Phòng ban lấy từ bảng ngày: MonthSummary không mang theo tên phòng.
	deptOf := map[uuid.UUID]string{}
	for _, d := range days {
		if d.DepartmentName != "" {
			deptOf[d.EmployeeID] = d.DepartmentName
		}
	}

	// Sắp theo phòng rồi theo tên: kế toán đọc theo từng phòng.
	sorted := append([]*domainatt.MonthSummary(nil), summaries...)
	sort.SliceStable(sorted, func(i, j int) bool {
		di, dj := deptOf[sorted[i].EmployeeID], deptOf[sorted[j].EmployeeID]
		if di != dj {
			return di < dj
		}
		return sorted[i].EmployeeName < sorted[j].EmployeeName
	})

	if err := writeSummarySheet(f, st, e, sorted, deptOf); err != nil {
		return nil, err
	}
	if err := writeDetailSheet(f, st, e, sorted, deptOf, days); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type workbookStyles struct {
	title, header, hours, weekend, absent int
}

func newWorkbookStyles(f *excelize.File) (*workbookStyles, error) {
	var (
		s   workbookStyles
		err error
	)
	border := []excelize.Border{
		{Type: "bottom", Color: "A3A3A3", Style: 1},
	}
	if s.title, err = f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 13}}); err != nil {
		return nil, err
	}
	if s.header, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F5F5F5"}},
		Border:    border,
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	}); err != nil {
		return nil, err
	}
	fmtHours := "0.0"
	if s.hours, err = f.NewStyle(&excelize.Style{CustomNumFmt: &fmtHours}); err != nil {
		return nil, err
	}
	if s.weekend, err = f.NewStyle(&excelize.Style{
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"EDEDED"}},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	}); err != nil {
		return nil, err
	}
	if s.absent, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "B91C1C"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	}); err != nil {
		return nil, err
	}
	return &s, nil
}

func reportTitle(e *domainatt.Export) string {
	t := fmt.Sprintf("Báo cáo chấm công tháng %02d/%d", e.Month, e.Year)
	if e.DepartmentName != "" {
		t += " — " + e.DepartmentName
	}
	return t
}

func writeSummarySheet(
	f *excelize.File,
	st *workbookStyles,
	e *domainatt.Export,
	rows []*domainatt.MonthSummary,
	deptOf map[uuid.UUID]string,
) error {
	s := sheetSummary
	if err := f.SetCellValue(s, "A1", reportTitle(e)); err != nil {
		return err
	}
	_ = f.SetCellStyle(s, "A1", "A1", st.title)

	headers := []string{
		"Nhân viên", "Phòng ban", "Ngày công chuẩn", "Có mặt", "Vắng", "Nghỉ phép",
		"Số lần đi muộn", "Phút đi muộn", "Phút thiếu giờ", "Giờ online", "Giờ hoạt động",
	}
	if err := f.SetSheetRow(s, "A3", &headers); err != nil {
		return err
	}
	_ = f.SetCellStyle(s, "A3", cell(len(headers), 3), st.header)
	_ = f.SetRowHeight(s, 3, 30)

	for i, m := range rows {
		r := 4 + i
		vals := []any{
			m.EmployeeName, deptOf[m.EmployeeID], m.WorkdayCount, m.PresentDays, m.AbsentDays,
			m.LeaveDays, m.LateDays, m.LateMinutes, m.ShortfallMins,
			hours(m.OnlineMinutes), hours(m.ActiveMinutes),
		}
		if err := f.SetSheetRow(s, cell(1, r), &vals); err != nil {
			return err
		}
	}
	if len(rows) > 0 {
		last := 3 + len(rows)
		_ = f.SetCellStyle(s, cell(10, 4), cell(11, last), st.hours)
	} else {
		_ = f.SetCellValue(s, "A4", "Không có dữ liệu chấm công nào trong phạm vi được xem")
	}

	_ = f.SetColWidth(s, "A", "A", 28)
	_ = f.SetColWidth(s, "B", "B", 20)
	_ = f.SetColWidth(s, "C", "K", 12)
	// Cố định dòng tiêu đề và cột tên khi cuộn.
	return f.SetPanes(s, &excelize.Panes{
		Freeze: true, XSplit: 1, YSplit: 3, TopLeftCell: "B4", ActivePane: "bottomRight",
	})
}

func writeDetailSheet(
	f *excelize.File,
	st *workbookStyles,
	e *domainatt.Export,
	rows []*domainatt.MonthSummary,
	deptOf map[uuid.UUID]string,
	days []*domainatt.Day,
) error {
	s := sheetDetail
	first := time.Date(e.Year, time.Month(e.Month), 1, 0, 0, 0, 0, time.UTC)
	n := first.AddDate(0, 1, -1).Day()

	if err := f.SetCellValue(s, "A1", reportTitle(e)); err != nil {
		return err
	}
	_ = f.SetCellStyle(s, "A1", "A1", st.title)

	// Hai dòng tiêu đề: số ngày và thứ trong tuần. Không có thứ thì người
	// đọc không phân biệt được ô trống cuối tuần với ô trống vì thiếu dữ liệu.
	weekday := []string{"CN", "T2", "T3", "T4", "T5", "T6", "T7"}
	_ = f.SetCellValue(s, "A3", "Nhân viên")
	_ = f.SetCellValue(s, "B3", "Phòng ban")
	_ = f.MergeCell(s, "A3", "A4")
	_ = f.MergeCell(s, "B3", "B4")
	for d := 1; d <= n; d++ {
		date := first.AddDate(0, 0, d-1)
		_ = f.SetCellValue(s, cell(2+d, 3), d)
		_ = f.SetCellValue(s, cell(2+d, 4), weekday[date.Weekday()])
	}
	_ = f.SetCellStyle(s, "A3", cell(2+n, 4), st.header)

	type key struct {
		emp uuid.UUID
		day int
	}
	status := map[key]domainatt.DayStatus{}
	for _, d := range days {
		status[key{d.EmployeeID, d.WorkDate.Day()}] = d.Status
	}

	for i, m := range rows {
		r := 5 + i
		_ = f.SetCellValue(s, cell(1, r), m.EmployeeName)
		_ = f.SetCellValue(s, cell(2, r), deptOf[m.EmployeeID])
		for d := 1; d <= n; d++ {
			c := cell(2+d, r)
			st0, ok := status[key{m.EmployeeID, d}]
			if ok {
				_ = f.SetCellValue(s, c, dayCodes[st0])
			}
			switch {
			case st0 == domainatt.DayWeekend:
				_ = f.SetCellStyle(s, c, c, st.weekend)
			case st0 == domainatt.DayAbsent:
				_ = f.SetCellStyle(s, c, c, st.absent)
			case !ok && isWeekend(first.AddDate(0, 0, d-1)):
				_ = f.SetCellStyle(s, c, c, st.weekend)
			}
		}
	}

	legend := 6 + len(rows)
	_ = f.SetCellValue(s, cell(1, legend),
		"Ký hiệu: X có mặt · V vắng · P nghỉ phép · L nghỉ lễ · ô xám là cuối tuần · ô trống là chưa có dữ liệu")

	_ = f.SetColWidth(s, "A", "A", 28)
	_ = f.SetColWidth(s, "B", "B", 18)
	_ = f.SetColWidth(s, cellCol(3), cellCol(2+n), 4)
	return f.SetPanes(s, &excelize.Panes{
		Freeze: true, XSplit: 2, YSplit: 4, TopLeftCell: "C5", ActivePane: "bottomRight",
	})
}

func isWeekend(t time.Time) bool {
	return t.Weekday() == time.Saturday || t.Weekday() == time.Sunday
}

// hours đổi phút sang giờ, một chữ số thập phân.
func hours(minutes int) float64 {
	return math.Round(float64(minutes)/60*10) / 10
}

func cell(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

func cellCol(col int) string {
	name, _ := excelize.ColumnNumberToName(col)
	return name
}
