//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/google/uuid"

	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
)

// Integration test cho lượt nhập nhân viên.
//
// Kiểm những thứ bản giả lập không kiểm được: nối kết quả vào mảng JSONB
// theo từng dòng, và hai cột đếm thành công/lỗi tính bằng SQL.

func TestEmployeeImportAppendAndCount(t *testing.T) {
	db := testDB(t)
	repo := NewEmployeeImportRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	creator := newEmployee(t, NewEmployeeRepository(db), companyID, "HR01", "hr@abc.vn")

	imp := &domainhr.EmployeeImport{
		CompanyID: companyID,
		FileName:  "danh-sách.xlsx",
		Status:    domainhr.ImportQueued,
		Rows: []domainhr.ImportRow{
			{Line: 2, Values: map[string]string{"ma_nhan_vien": "NV1", "ho_ten": "Nguyễn Văn A"}},
			{Line: 3, Values: map[string]string{"ma_nhan_vien": "NV2"}},
			{Line: 4, Values: map[string]string{"ma_nhan_vien": "NV3"}},
		},
		CreatedBy: &creator.ID,
		Actor:     domainhr.ImportActor{UserID: uuid.New(), EmployeeID: creator.ID, Scope: "all"},
	}
	if err := repo.Create(ctx, imp); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.MarkProcessing(ctx, imp.ID); err != nil {
		t.Fatalf("MarkProcessing: %v", err)
	}
	id := uuid.New()
	for _, r := range []domainhr.ImportRowResult{
		{Line: 2, EmployeeCode: "NV1", EmployeeID: &id, AccountCreated: true},
		{Line: 3, EmployeeCode: "NV2", Error: "Mã nhân viên đã tồn tại"},
		// Có cảnh báo nhưng KHÔNG có lỗi: vẫn tính là thành công.
		{Line: 4, EmployeeCode: "NV3", Warning: "chưa tạo được tài khoản"},
	} {
		if err := repo.AppendResult(ctx, imp.ID, r); err != nil {
			t.Fatalf("AppendResult: %v", err)
		}
	}

	got, err := repo.GetByID(ctx, imp.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != domainhr.ImportProcessing || got.StartedAt == nil {
		t.Errorf("phải ở processing và có started_at: %s %v", got.Status, got.StartedAt)
	}
	if got.TotalRows != 3 || len(got.Rows) != 3 || got.Rows[0].Values["ho_ten"] != "Nguyễn Văn A" {
		t.Errorf("các dòng đọc lại sai: %+v", got.Rows)
	}
	if len(got.Results) != 3 || got.Results[1].Error == "" || got.Results[0].EmployeeID == nil {
		t.Fatalf("kết quả phải giữ đúng thứ tự nối: %+v", got.Results)
	}
	if got.Succeeded != 2 || got.Failed != 1 {
		t.Errorf("muốn 2 thành công 1 lỗi, được %d/%d", got.Succeeded, got.Failed)
	}
	if got.CreatorName == "" || got.Actor.EmployeeID != creator.ID {
		t.Errorf("thiếu người tạo hoặc actor: %q %+v", got.CreatorName, got.Actor)
	}

	if err := repo.Finish(ctx, imp.ID, domainhr.ImportDone, ""); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	mine, err := repo.List(ctx, companyID, &creator.ID, 10)
	if err != nil || len(mine) != 1 || mine[0].Status != domainhr.ImportDone || mine[0].FinishedAt == nil {
		t.Fatalf("List theo người tạo: %v %+v", err, mine)
	}
	if mine[0].Succeeded != 2 {
		t.Errorf("List cũng phải đếm được: %d", mine[0].Succeeded)
	}
	other := uuid.New()
	if none, _ := repo.List(ctx, companyID, &other, 10); len(none) != 0 {
		t.Error("lọc theo người khác phải rỗng")
	}
}
