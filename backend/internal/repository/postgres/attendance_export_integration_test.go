//go:build integration

package postgres

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domainatt "github.com/PhamVanPhuc2k2/manage/internal/domain/attendance"
)

// Kiểm phần bản giả lập không kiểm được: tệp lưu BYTEA đọc ra đúng từng
// byte, danh sách KHÔNG kéo theo nội dung tệp, và việc dọn tệp cũ chỉ xoá
// tệp chứ giữ bản ghi.
func TestAttendanceExportFileLifecycle(t *testing.T) {
	db := testDB(t)
	repo := NewAttendanceExportRepository(db)
	ctx := context.Background()

	companyID := newCompany(t)
	who := newEmployee(t, NewEmployeeRepository(db), companyID, "KT01", "kt@abc.vn")

	e := &domainatt.Export{
		Year: 2026, Month: 8, Status: domainatt.ExportQueued, RequestedBy: &who.ID,
		Actor: domainatt.ExportActor{UserID: uuid.New(), EmployeeID: who.ID, Scope: "all"},
	}
	if err := repo.Create(ctx, e); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, _, err := repo.GetFile(ctx, e.ID); !errors.Is(err, domainatt.ErrNotFound) {
		t.Fatalf("chưa xong thì chưa có tệp, được %v", err)
	}

	data := []byte("PK\x03\x04 nội dung xlsx giả \x00\xff")
	if err := repo.Complete(ctx, e.ID, "cham-cong-2026-08.xlsx", data, 42); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	got, err := repo.GetByID(ctx, e.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Status != domainatt.ExportDone || !got.HasFile || got.EmployeeCount != 42 ||
		got.FinishedAt == nil || got.RequesterName == "" || got.Actor.EmployeeID != who.ID {
		t.Fatalf("đọc lại sai: %+v", got)
	}
	name, back, err := repo.GetFile(ctx, e.ID)
	if err != nil || name != "cham-cong-2026-08.xlsx" || !bytes.Equal(back, data) {
		t.Fatalf("tệp đọc ra không khớp từng byte: %q %v", name, err)
	}

	list, err := repo.List(ctx, &who.ID, 10)
	if err != nil || len(list) != 1 || !list[0].HasFile {
		t.Fatalf("List: %v %+v", err, list)
	}

	// Chưa quá hạn: không dọn.
	if n, _ := repo.PurgeFiles(ctx, time.Now().Add(-time.Hour)); n != 0 {
		t.Fatalf("dọn nhầm tệp còn hạn: %d", n)
	}
	if n, _ := repo.PurgeFiles(ctx, time.Now().Add(time.Hour)); n != 1 {
		t.Fatalf("phải dọn đúng 1 tệp, được %d", n)
	}
	got, _ = repo.GetByID(ctx, e.ID)
	if got.HasFile || got.Status != domainatt.ExportDone {
		t.Fatalf("dọn xong: mất tệp nhưng giữ bản ghi, được %+v", got)
	}
}
