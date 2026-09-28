package hr

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/unicode/norm"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// Tên chuẩn của các cột trong tệp nhập.
const (
	colCode       = "ma_nhan_vien"
	colName       = "ho_ten"
	colEmail      = "email"
	colPhone      = "so_dien_thoai"
	colBirth      = "ngay_sinh"
	colGender     = "gioi_tinh"
	colAddress    = "dia_chi"
	colDepartment = "phong_ban"
	colPosition   = "chuc_vu"
	colManager    = "ma_cap_tren"
	colWorkMode   = "hinh_thuc_lam_viec"
	colStatus     = "trang_thai"
	colJoined     = "ngay_vao_lam"
)

// ImportColumns là thứ tự cột của tệp mẫu. Frontend lấy đúng danh sách này
// qua API để sinh tệp mẫu, nên tệp mẫu và bộ đọc không bao giờ lệch nhau.
var ImportColumns = []string{
	colCode, colName, colEmail, colPhone, colBirth, colGender, colAddress,
	colDepartment, colPosition, colManager, colWorkMode, colStatus, colJoined,
}

// RequiredImportColumns là các cột bắt buộc phải có trong tệp.
var RequiredImportColumns = []string{colCode, colName, colEmail, colJoined}

// Tên khác mà người ta hay đặt cho cùng một cột. Khoá đã qua normalizeHeader.
var importColumnAliases = map[string]string{
	"ma_nv":        colCode,
	"ma":           colCode,
	"ho_va_ten":    colName,
	"ten":          colName,
	"sdt":          colPhone,
	"dien_thoai":   colPhone,
	"phong":        colDepartment,
	"cap_tren":     colManager,
	"ma_quan_ly":   colManager,
	"hinh_thuc":    colWorkMode,
	"ngay_vao":     colJoined,
	"ngay_bat_dau": colJoined,
}

// Cột được bỏ qua lặng lẽ. Chỉ "số thứ tự" — cột ai cũng thêm vào bảng
// tính mà không mang dữ liệu gì. Mọi cột lạ khác đều bị từ chối: bỏ qua
// lặng lẽ một cột gõ sai tên nghĩa là mất dữ liệu của cả cột mà không ai hay.
var ignoredImportColumns = map[string]bool{"stt": true, "so_thu_tu": true}

// MaxImportBytes chặn kích thước tệp. 1000 dòng CSV chỉ cỡ 200 KB; XLSX
// nén nhưng có phần định dạng — 5 MB là dư dả.
const MaxImportBytes = 5 << 20

// parseImportFile đọc tệp CSV hoặc XLSX thành các dòng có tiêu đề chuẩn.
//
// Lỗi ở đây là lỗi của CẢ TỆP (sai định dạng, thiếu cột bắt buộc, quá nhiều
// dòng) và trả về ngay lúc tải lên. Lỗi của từng dòng để worker báo sau.
func parseImportFile(fileName string, data []byte) ([]domainhr.ImportRow, error) {
	if len(data) == 0 {
		return nil, apperror.Invalid("Tệp rỗng", nil)
	}
	if len(data) > MaxImportBytes {
		return nil, apperror.Invalid("Tệp quá lớn (tối đa 5 MB)", nil)
	}

	var (
		records [][]string
		err     error
	)
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".csv":
		records, err = readCSV(data)
	case ".xlsx":
		records, err = readXLSX(data)
	default:
		return nil, apperror.Invalid("Chỉ nhận tệp .csv hoặc .xlsx", nil)
	}
	if err != nil {
		return nil, err
	}
	return recordsToRows(records)
}

func readCSV(data []byte) ([][]string, error) {
	// Excel lưu "CSV UTF-8" kèm BOM ở đầu tệp. Không bỏ đi thì tiêu đề
	// cột đầu tiên dính BOM và bị báo là cột lạ.
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	// Excel lưu "CSV" thường (không phải "CSV UTF-8") theo bảng mã của
	// Windows — tiếng Việt thành ký tự rác. Không đoán bảng mã: từ chối và
	// chỉ đúng cách lưu lại, còn hơn nhập vào cả trăm cái tên hỏng.
	if !utf8.Valid(data) {
		return nil, apperror.Invalid(
			"Tệp CSV không ở dạng UTF-8 nên tiếng Việt sẽ bị lỗi. "+
				"Trong Excel chọn Lưu thành → \"CSV UTF-8\", hoặc tải lên tệp .xlsx", nil)
	}

	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = guessCSVDelimiter(data)
	r.FieldsPerRecord = -1 // dòng thiếu cột cuối vẫn đọc được
	r.TrimLeadingSpace = true

	var out [][]string
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, apperror.Invalid(fmt.Sprintf("Không đọc được tệp CSV: %v", err), nil)
		}
		out = append(out, rec)
	}
	return out, nil
}

// guessCSVDelimiter chọn giữa dấu phẩy và chấm phẩy theo dòng tiêu đề.
//
// Excel trên máy đặt vùng Việt Nam (dấu phẩy là dấu thập phân) lưu CSV bằng
// dấu chấm phẩy. Chỉ nhận dấu phẩy thì cả dòng thành một cột duy nhất.
func guessCSVDelimiter(data []byte) rune {
	first, _, _ := bytes.Cut(data, []byte("\n"))
	if bytes.Count(first, []byte(";")) > bytes.Count(first, []byte(",")) {
		return ';'
	}
	return ','
}

func readXLSX(data []byte) ([][]string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, apperror.Invalid("Không đọc được tệp Excel", nil)
	}
	defer func() { _ = f.Close() }()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, apperror.Invalid("Tệp Excel không có trang tính nào", nil)
	}

	// RawCellValue: ô ngày trả về số sê-ri của Excel (45292) chứ không
	// phải chuỗi theo định dạng hiển thị của máy người tạo tệp — thứ mà
	// có thể là 01-02-24, 2/1/2024 hay 1/2/2024 tuỳ vùng.
	rows, err := f.GetRows(sheets[0], excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, apperror.Invalid("Không đọc được trang tính đầu tiên", nil)
	}
	return rows, nil
}

func recordsToRows(records [][]string) ([]domainhr.ImportRow, error) {
	// Tiêu đề là dòng KHÔNG rỗng đầu tiên: người ta hay chừa vài dòng
	// trống ở trên cùng bảng tính.
	headerAt := -1
	for i, rec := range records {
		if !blankRecord(rec) {
			headerAt = i
			break
		}
	}
	if headerAt < 0 {
		return nil, apperror.Invalid("Tệp không có dữ liệu", nil)
	}

	cols := make([]string, len(records[headerAt]))
	seen := map[string]bool{}
	var unknown []string
	for i, raw := range records[headerAt] {
		key := normalizeHeader(raw)
		if alias, ok := importColumnAliases[key]; ok {
			key = alias
		}
		switch {
		case key == "" || ignoredImportColumns[key]:
			continue
		case !isImportColumn(key):
			unknown = append(unknown, strings.TrimSpace(raw))
			continue
		case seen[key]:
			return nil, apperror.Invalid(
				fmt.Sprintf("Cột %q xuất hiện hai lần", strings.TrimSpace(raw)), nil)
		}
		seen[key] = true
		cols[i] = key
	}
	if len(unknown) > 0 {
		return nil, apperror.Invalid(fmt.Sprintf(
			"Không nhận ra cột: %s. Các cột hợp lệ: %s",
			strings.Join(unknown, ", "), strings.Join(ImportColumns, ", ")), nil)
	}
	var missing []string
	for _, c := range RequiredImportColumns {
		if !seen[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return nil, apperror.Invalid(
			"Thiếu cột bắt buộc: "+strings.Join(missing, ", "), nil)
	}

	var out []domainhr.ImportRow
	for i := headerAt + 1; i < len(records); i++ {
		rec := records[i]
		if blankRecord(rec) {
			continue
		}
		values := map[string]string{}
		for j, v := range rec {
			if j < len(cols) && cols[j] != "" {
				values[cols[j]] = strings.TrimSpace(v)
			}
		}
		// Line đánh số theo bảng tính (bắt đầu từ 1) để người dùng tìm
		// đúng dòng khi đọc kết quả.
		out = append(out, domainhr.ImportRow{Line: i + 1, Values: values})
		if len(out) > domainhr.MaxImportRows {
			return nil, apperror.Invalid(fmt.Sprintf(
				"Tệp có hơn %d dòng. Chia thành nhiều tệp nhỏ hơn", domainhr.MaxImportRows), nil)
		}
	}
	if len(out) == 0 {
		return nil, apperror.Invalid("Tệp chỉ có dòng tiêu đề, không có nhân viên nào", nil)
	}
	return out, nil
}

func isImportColumn(key string) bool {
	for _, c := range ImportColumns {
		if c == key {
			return true
		}
	}
	return false
}

func blankRecord(rec []string) bool {
	for _, v := range rec {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

// normalizeHeader đưa "Mã nhân viên", "MÃ NHÂN VIÊN", "ma-nhan-vien" về
// cùng một khoá "ma_nhan_vien".
func normalizeHeader(s string) string {
	s = foldVietnamese(s)
	var b strings.Builder
	underscore := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			underscore = false
		} else if b.Len() > 0 && !underscore {
			b.WriteByte('_')
			underscore = true
		}
	}
	return strings.TrimSuffix(b.String(), "_")
}

// foldVietnamese bỏ dấu và viết thường: "Phòng Kế Toán" → "phong ke toan".
func foldVietnamese(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(strings.TrimSpace(s))) {
		switch {
		case unicode.Is(unicode.Mn, r):
			// dấu thanh và dấu mũ tách ra sau NFD
		case r == 'đ':
			b.WriteRune('d')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// parseImportDate nhận ba dạng người dùng thật sự gặp:
//   - 2024-01-15 (ISO, cũng là dạng của tệp mẫu)
//   - 15/01/2024 hoặc 5/1/2024 (cách người Việt gõ tay)
//   - 45306 (số sê-ri ngày của Excel, khi ô được định dạng là ngày)
//
// KHÔNG nhận dạng tháng/ngày kiểu Mỹ: 03/04/2024 luôn là mùng 3 tháng 4.
func parseImportDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006", "2/1/2006", "02-01-2006", "2-1-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return &t, nil
		}
	}
	// Excel đếm ngày từ 30/12/1899. Giới hạn trong khoảng 1920–2100 để một
	// số lạc chỗ (mã nhân viên, số điện thoại) không bị đọc thành ngày.
	if n, err := strconv.ParseFloat(s, 64); err == nil && n >= 7306 && n <= 73051 {
		t := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(n))
		return &t, nil
	}
	return nil, fmt.Errorf("không đọc được ngày %q", s)
}

// Giá trị chấp nhận cho các cột liệt kê. Khoá đã qua foldVietnamese, nên
// "Tại văn phòng", "tai van phong" và "onsite" đều khớp.
var (
	importGenders = map[string]string{
		"nam": "nam", "male": "nam",
		"nu": "nu", "female": "nu",
		"khac": "khac", "other": "khac",
	}
	importWorkModes = map[string]domainhr.WorkMode{
		"onsite": domainhr.WorkModeOnsite, "tai van phong": domainhr.WorkModeOnsite,
		"remote": domainhr.WorkModeRemote, "tu xa": domainhr.WorkModeRemote,
		"hybrid": domainhr.WorkModeHybrid, "ket hop": domainhr.WorkModeHybrid,
	}
	importStatuses = map[string]domainhr.EmployeeStatus{
		"probation": domainhr.StatusProbation, "thu viec": domainhr.StatusProbation,
		"official": domainhr.StatusOfficial, "chinh thuc": domainhr.StatusOfficial,
	}
)

// choices liệt kê các giá trị hợp lệ cho câu báo lỗi, theo thứ tự cố định.
func choices[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
