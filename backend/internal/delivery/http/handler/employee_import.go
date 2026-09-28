package handler

import (
	"errors"
	"io"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	appmw "github.com/PhamVanPhuc2k2/manage/internal/delivery/http/middleware"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	uchr "github.com/PhamVanPhuc2k2/manage/internal/usecase/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/httpx"
)

// EmployeeImportHandler nhận tệp nhập nhân viên hàng loạt.
type EmployeeImportHandler struct {
	uc *uchr.Usecase
}

func NewEmployeeImportHandler(uc *uchr.Usecase) *EmployeeImportHandler {
	return &EmployeeImportHandler{uc: uc}
}

type employeeImportDTO struct {
	ID             string                     `json:"id"`
	FileName       string                     `json:"file_name"`
	Status         string                     `json:"status"`
	CreateAccounts bool                       `json:"create_accounts"`
	TotalRows      int                        `json:"total_rows"`
	Processed      int                        `json:"processed"`
	Succeeded      int                        `json:"succeeded"`
	Failed         int                        `json:"failed"`
	CreatorName    string                     `json:"creator_name,omitempty"`
	Error          string                     `json:"error,omitempty"`
	CreatedAt      time.Time                  `json:"created_at"`
	StartedAt      *time.Time                 `json:"started_at,omitempty"`
	FinishedAt     *time.Time                 `json:"finished_at,omitempty"`
	Results        []domainhr.ImportRowResult `json:"results,omitempty"`
}

func toEmployeeImportDTO(i *domainhr.EmployeeImport, withResults bool) employeeImportDTO {
	d := employeeImportDTO{
		ID:             i.ID.String(),
		FileName:       i.FileName,
		Status:         string(i.Status),
		CreateAccounts: i.CreateAccounts,
		TotalRows:      i.TotalRows,
		Processed:      i.Succeeded + i.Failed,
		Succeeded:      i.Succeeded,
		Failed:         i.Failed,
		CreatorName:    i.CreatorName,
		Error:          i.Error,
		CreatedAt:      i.CreatedAt,
		StartedAt:      i.StartedAt,
		FinishedAt:     i.FinishedAt,
	}
	if withResults {
		// Mảng rỗng chứ không phải null: trang kết quả đang chờ worker vẫn
		// nhận một danh sách để hiển thị.
		d.Results = i.Results
		if d.Results == nil {
			d.Results = []domainhr.ImportRowResult{}
		}
	}
	return d
}

// Create nhận tệp qua multipart/form-data: trường "file" và tuỳ chọn
// "create_accounts" = "true".
//
// Không đi đường presigned URL lên R2 như avatar: tệp chỉ vài trăm KB, được
// đọc ngay trong request rồi bỏ đi, và chức năng này phải chạy được cả khi
// chưa cấu hình R2.
func (h *EmployeeImportHandler) Create(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	// Chặn ở tầng HTTP trước khi đọc: không chặn thì một tệp 1 GB được đọc
	// hết vào RAM rồi mới bị usecase từ chối. Cộng thêm phần đầu multipart.
	r.Body = http.MaxBytesReader(w, r.Body, uchr.MaxImportBytes+64<<10)
	if err := r.ParseMultipartForm(uchr.MaxImportBytes); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			Error(w, apperror.Invalid("Tệp quá lớn (tối đa 5 MB)", nil), requestID)
			return
		}
		Error(w, apperror.Invalid("Yêu cầu phải là multipart/form-data có trường file", nil), requestID)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	file, header, err := r.FormFile("file")
	if err != nil {
		Error(w, apperror.Invalid("Thiếu tệp (trường file)", nil), requestID)
		return
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		Error(w, apperror.Invalid("Không đọc được tệp", nil), requestID)
		return
	}

	ctx := uchr.WithImportRequestID(r.Context(), requestID)
	imp, err := h.uc.StartImport(ctx, appmw.ActorFrom(ctx),
		header.Filename, data, r.FormValue("create_accounts") == "true")
	if err != nil {
		Error(w, err, requestID)
		return
	}

	// 202: tệp đã được nhận, nhân viên thì chưa tạo xong.
	httpx.JSON(w, http.StatusAccepted, httpx.Envelope{Data: toEmployeeImportDTO(imp, true)})
}

type importTemplateMeta struct {
	Columns         []string `json:"columns"`
	RequiredColumns []string `json:"required_columns"`
	MaxRows         int      `json:"max_rows"`
}

// List trả về các lượt nhập gần nhất, kèm mô tả tệp mẫu trong meta — để
// frontend sinh tệp mẫu từ đúng danh sách cột mà bộ đọc chấp nhận.
func (h *EmployeeImportHandler) List(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	items, err := h.uc.ListImports(r.Context(), appmw.ActorFrom(r.Context()))
	if err != nil {
		Error(w, err, requestID)
		return
	}
	out := make([]employeeImportDTO, 0, len(items))
	for _, i := range items {
		out = append(out, toEmployeeImportDTO(i, false))
	}
	httpx.JSON(w, http.StatusOK, httpx.Envelope{
		Data: out,
		Meta: importTemplateMeta{
			Columns:         uchr.ImportColumns,
			RequiredColumns: uchr.RequiredImportColumns,
			MaxRows:         domainhr.MaxImportRows,
		},
	})
}

func (h *EmployeeImportHandler) Get(w http.ResponseWriter, r *http.Request) {
	requestID := chimw.GetReqID(r.Context())

	id, err := parseUUIDParam(r, "importID")
	if err != nil {
		Error(w, err, requestID)
		return
	}
	imp, err := h.uc.GetImport(r.Context(), appmw.ActorFrom(r.Context()), id)
	if err != nil {
		Error(w, err, requestID)
		return
	}
	httpx.OK(w, toEmployeeImportDTO(imp, true))
}
