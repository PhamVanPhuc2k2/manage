package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
)

func TestSyncSourcesCreatesMissingGroups(t *testing.T) {
	h := newChatHarness()

	deptID, projID := uuid.New(), uuid.New()
	h.lookup.byDept = map[uuid.UUID][]uuid.UUID{deptID: {uuid.New(), uuid.New()}}
	h.lookup.byProject = map[uuid.UUID][]uuid.UUID{projID: {uuid.New()}}

	sources := []domainchat.GroupSource{
		{Kind: domainchat.KindDepartment, ID: deptID, Name: "Kỹ thuật"},
		{Kind: domainchat.KindProject, ID: projID, Name: "Website mới"},
	}

	created, changed, err := h.uc.SyncSources(
		context.Background(), h.company.id, sources)
	if err != nil {
		t.Fatalf("SyncSources lỗi: %v", err)
	}
	if created != 2 {
		t.Errorf("tạo %d nhóm, muốn 2", created)
	}
	if changed != 2 {
		t.Errorf("đổi thành viên %d nhóm, muốn 2", changed)
	}

	// Tên nhóm phải có tiền tố theo loại nguồn, để người dùng phân biệt được
	// ngay trong danh sách mà không phải mở ra xem.
	names := map[string]bool{}
	for _, c := range h.convs.created {
		names[c.Name] = true
	}
	for _, want := range []string{"Phòng: Kỹ thuật", "Dự án: Website mới"} {
		if !names[want] {
			t.Errorf("thiếu nhóm tên %q; có %v", want, names)
		}
	}
}

// TestSyncSourcesSkipsEmptySource là phép thử chống mất dữ liệu.
//
// Một phòng ban tạm thời không còn ai gần như luôn là dữ liệu đang dở, và
// đồng bộ theo nó sẽ gỡ sạch thành viên khỏi nhóm — mà lịch sử trò chuyện thì
// không lấy lại được bằng lần chạy sau.
func TestSyncSourcesSkipsEmptySource(t *testing.T) {
	h := newChatHarness()

	deptID := uuid.New()
	existing := &domainchat.Conversation{
		ID:           uuid.New(),
		Kind:         domainchat.KindDepartment,
		Name:         "Phòng: Kỹ thuật",
		DepartmentID: &deptID,
	}
	h.convs.put(existing)
	h.convs.bySource = func(field string, id uuid.UUID) *domainchat.Conversation {
		if field == "department_id" && id == deptID {
			return existing
		}
		return nil
	}
	// Nguồn RỖNG: không có nhân viên nào.
	h.lookup.byDept = map[uuid.UUID][]uuid.UUID{deptID: {}}

	_, changed, err := h.uc.SyncSources(context.Background(), h.company.id,
		[]domainchat.GroupSource{
			{Kind: domainchat.KindDepartment, ID: deptID, Name: "Kỹ thuật"},
		})
	if err != nil {
		t.Fatalf("SyncSources lỗi: %v", err)
	}
	if changed != 0 {
		t.Errorf("đổi %d nhóm, muốn 0", changed)
	}
	if _, synced := h.convs.syncedTo[existing.ID]; synced {
		t.Error("nguồn rỗng thì KHÔNG được gọi đồng bộ thành viên")
	}
}

// TestSyncSourcesRenamesWhenSourceRenamed: nhóm tự động không cho sửa tên tay,
// nên nếu job không đổi theo thì tên nhóm đứng yên ở tên cũ mãi mãi và người
// dùng không có cách nào sửa.
func TestSyncSourcesRenamesWhenSourceRenamed(t *testing.T) {
	h := newChatHarness()

	deptID := uuid.New()
	existing := &domainchat.Conversation{
		ID:           uuid.New(),
		Kind:         domainchat.KindDepartment,
		Name:         "Phòng: Tên cũ",
		DepartmentID: &deptID,
	}
	h.convs.put(existing)
	h.convs.bySource = func(string, uuid.UUID) *domainchat.Conversation { return existing }
	h.lookup.byDept = map[uuid.UUID][]uuid.UUID{deptID: {uuid.New()}}

	if _, _, err := h.uc.SyncSources(context.Background(), h.company.id,
		[]domainchat.GroupSource{
			{Kind: domainchat.KindDepartment, ID: deptID, Name: "Tên mới"},
		}); err != nil {
		t.Fatalf("SyncSources lỗi: %v", err)
	}

	if len(h.convs.updated) != 1 {
		t.Fatalf("gọi Update %d lần, muốn 1", len(h.convs.updated))
	}
	if got := h.convs.updated[0].Name; got != "Phòng: Tên mới" {
		t.Errorf("tên mới = %q, muốn \"Phòng: Tên mới\"", got)
	}
}

// TestSyncSourcesDoesNotRenameWhenUnchanged: không có phép so sánh này thì mỗi
// lần chạy job là một lệnh UPDATE cho mọi nhóm, mỗi 15 phút, mãi mãi.
func TestSyncSourcesDoesNotRenameWhenUnchanged(t *testing.T) {
	h := newChatHarness()

	deptID := uuid.New()
	existing := &domainchat.Conversation{
		ID:           uuid.New(),
		Kind:         domainchat.KindDepartment,
		Name:         "Phòng: Kỹ thuật",
		DepartmentID: &deptID,
	}
	h.convs.put(existing)
	h.convs.bySource = func(string, uuid.UUID) *domainchat.Conversation { return existing }
	h.lookup.byDept = map[uuid.UUID][]uuid.UUID{deptID: {uuid.New()}}

	if _, _, err := h.uc.SyncSources(context.Background(), h.company.id,
		[]domainchat.GroupSource{
			{Kind: domainchat.KindDepartment, ID: deptID, Name: "Kỹ thuật"},
		}); err != nil {
		t.Fatalf("SyncSources lỗi: %v", err)
	}

	if len(h.convs.updated) != 0 {
		t.Errorf("tên không đổi mà vẫn gọi Update %d lần", len(h.convs.updated))
	}
}

// TestSyncSourcesSetsCorrectSourceColumn: gắn sai cột nguồn sẽ làm chỉ mục
// duy nhất (một phòng ban chỉ có một nhóm) mất tác dụng, và job tạo nhóm mới
// ở mỗi lần chạy.
func TestSyncSourcesSetsCorrectSourceColumn(t *testing.T) {
	h := newChatHarness()

	deptID, projID := uuid.New(), uuid.New()
	h.lookup.byDept = map[uuid.UUID][]uuid.UUID{deptID: {uuid.New()}}
	h.lookup.byProject = map[uuid.UUID][]uuid.UUID{projID: {uuid.New()}}

	if _, _, err := h.uc.SyncSources(context.Background(), h.company.id,
		[]domainchat.GroupSource{
			{Kind: domainchat.KindDepartment, ID: deptID, Name: "Kỹ thuật"},
			{Kind: domainchat.KindProject, ID: projID, Name: "Website"},
		}); err != nil {
		t.Fatalf("SyncSources lỗi: %v", err)
	}

	for _, c := range h.convs.created {
		switch c.Kind {
		case domainchat.KindDepartment:
			if c.DepartmentID == nil || *c.DepartmentID != deptID {
				t.Error("nhóm phòng ban không gắn department_id")
			}
			if c.ProjectID != nil {
				t.Error("nhóm phòng ban không được gắn project_id")
			}
		case domainchat.KindProject:
			if c.ProjectID == nil || *c.ProjectID != projID {
				t.Error("nhóm dự án không gắn project_id")
			}
			if c.DepartmentID != nil {
				t.Error("nhóm dự án không được gắn department_id")
			}
		}
	}
}

// TestSyncSourcesContinuesAfterOneFailure: một nguồn lỗi không được làm hỏng
// cả lượt đồng bộ — những phòng ban còn lại vẫn phải được xử lý.
func TestSyncSourcesContinuesAfterOneFailure(t *testing.T) {
	h := newChatHarness()

	badID, goodID := uuid.New(), uuid.New()
	// Nguồn đầu không có nhân viên (bị bỏ qua), nguồn sau có.
	h.lookup.byDept = map[uuid.UUID][]uuid.UUID{
		badID:  {},
		goodID: {uuid.New()},
	}

	created, changed, err := h.uc.SyncSources(context.Background(), h.company.id,
		[]domainchat.GroupSource{
			{Kind: domainchat.KindDepartment, ID: badID, Name: "Phòng rỗng"},
			{Kind: domainchat.KindDepartment, ID: goodID, Name: "Phòng có người"},
		})
	if err != nil {
		t.Fatalf("SyncSources lỗi: %v", err)
	}

	// Cả hai nhóm đều được TẠO (nhóm rỗng vẫn cần tồn tại để đón người sau),
	// nhưng chỉ nhóm có người mới được đồng bộ thành viên.
	if created != 2 {
		t.Errorf("tạo %d nhóm, muốn 2", created)
	}
	if changed != 1 {
		t.Errorf("đồng bộ %d nhóm, muốn 1", changed)
	}
}

func TestSyncAllWithoutSourceListerIsNoop(t *testing.T) {
	h := newChatHarness()
	h.uc.sources = nil

	created, changed, err := h.uc.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll lỗi: %v", err)
	}
	if created != 0 || changed != 0 {
		t.Error("không có SourceLister thì không làm gì")
	}
}

func TestSyncAllUsesSourceLister(t *testing.T) {
	h := newChatHarness()

	deptID := uuid.New()
	h.sources.list = []domainchat.GroupSource{
		{Kind: domainchat.KindDepartment, ID: deptID, Name: "Kỹ thuật"},
	}
	h.lookup.byDept = map[uuid.UUID][]uuid.UUID{deptID: {uuid.New()}}

	created, _, err := h.uc.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll lỗi: %v", err)
	}
	if created != 1 {
		t.Errorf("tạo %d nhóm, muốn 1", created)
	}
}
