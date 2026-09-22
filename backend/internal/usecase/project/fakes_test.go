package project

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainproject "github.com/PhamVanPhuc2k2/manage/internal/domain/project"
)

// Bộ giả lập cho cổng của module dự án. Cùng cách viết với chấm công và chat:
// hiện thực đầy đủ interface, chỉ những method có dữ liệu gán vào mới làm gì.
//
// fakeTasks ở đây nhiều hơn một stub: nó giữ một bảng Kanban trong bộ nhớ và
// hiện thực Reorder/RenumberColumn/ListBoard đúng như PostgreSQL làm. Thuật
// toán sắp thứ tự phân số là thứ đáng kiểm thử nhất của module này, và nó chỉ
// kiểm được khi bản giả lập phản ứng thật với việc ghi.

type fakeProjects struct {
	byID  map[uuid.UUID]*domainproject.Project
	codes map[string]bool

	created  []*domainproject.Project
	updated  []*domainproject.Project
	deleted  []uuid.UUID
	nextSeq  int
	memberOf []uuid.UUID
}

func newFakeProjects() *fakeProjects {
	return &fakeProjects{
		byID:  map[uuid.UUID]*domainproject.Project{},
		codes: map[string]bool{},
	}
}

func (f *fakeProjects) put(p *domainproject.Project) { f.byID[p.ID] = p }

func (f *fakeProjects) Create(_ context.Context, p *domainproject.Project) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	p.CreatedAt = time.Now()
	f.created = append(f.created, p)
	f.byID[p.ID] = p
	return nil
}

func (f *fakeProjects) Update(_ context.Context, p *domainproject.Project) error {
	if f.byID[p.ID] == nil {
		return domainproject.ErrNotFound
	}
	f.updated = append(f.updated, p)
	f.byID[p.ID] = p
	return nil
}

func (f *fakeProjects) SoftDelete(_ context.Context, id uuid.UUID) error {
	if f.byID[id] == nil {
		return domainproject.ErrNotFound
	}
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeProjects) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainproject.Project, error) {
	if p := f.byID[id]; p != nil {
		// Trả BẢN SAO: usecase gán ViewerRole vào kết quả, và trả con trỏ gốc
		// sẽ khiến giá trị đó dính lại giữa các lần gọi trong cùng bài kiểm thử.
		copied := *p
		return &copied, nil
	}
	return nil, domainproject.ErrNotFound
}

func (f *fakeProjects) List(
	context.Context, domainproject.ProjectFilter,
) ([]*domainproject.Project, int, error) {
	return nil, 0, nil
}

func (f *fakeProjects) ExistsCode(
	_ context.Context, _ uuid.UUID, code string, _ *uuid.UUID,
) (bool, error) {
	return f.codes[code], nil
}

func (f *fakeProjects) ListIDsForMember(
	context.Context, uuid.UUID,
) ([]uuid.UUID, error) {
	return f.memberOf, nil
}

func (f *fakeProjects) NextTaskSeq(context.Context, uuid.UUID) (int, error) {
	f.nextSeq++
	return f.nextSeq, nil
}

type fakeMembers struct {
	// byProject[projectID][employeeID] = vai trò
	byProject map[uuid.UUID]map[uuid.UUID]domainproject.Role

	added   []*domainproject.Member
	removed []uuid.UUID
}

func newFakeMembers() *fakeMembers {
	return &fakeMembers{byProject: map[uuid.UUID]map[uuid.UUID]domainproject.Role{}}
}

func (f *fakeMembers) join(projectID, empID uuid.UUID, role domainproject.Role) {
	if f.byProject[projectID] == nil {
		f.byProject[projectID] = map[uuid.UUID]domainproject.Role{}
	}
	f.byProject[projectID][empID] = role
}

func (f *fakeMembers) Add(_ context.Context, m *domainproject.Member) error {
	f.added = append(f.added, m)
	f.join(m.ProjectID, m.EmployeeID, m.Role)
	return nil
}

func (f *fakeMembers) UpdateRole(
	_ context.Context, projectID, empID uuid.UUID, role domainproject.Role,
) error {
	if f.byProject[projectID] == nil || f.byProject[projectID][empID] == "" {
		return domainproject.ErrNotFound
	}
	f.byProject[projectID][empID] = role
	return nil
}

func (f *fakeMembers) Remove(_ context.Context, projectID, empID uuid.UUID) error {
	if f.byProject[projectID] == nil || f.byProject[projectID][empID] == "" {
		return domainproject.ErrNotFound
	}
	delete(f.byProject[projectID], empID)
	f.removed = append(f.removed, empID)
	return nil
}

func (f *fakeMembers) List(
	_ context.Context, projectID uuid.UUID,
) ([]*domainproject.Member, error) {
	out := make([]*domainproject.Member, 0, len(f.byProject[projectID]))
	for id, role := range f.byProject[projectID] {
		out = append(out, &domainproject.Member{
			ProjectID: projectID, EmployeeID: id, Role: role,
		})
	}
	return out, nil
}

func (f *fakeMembers) Get(
	_ context.Context, projectID, empID uuid.UUID,
) (*domainproject.Member, error) {
	if f.byProject[projectID] == nil {
		return nil, domainproject.ErrNotFound
	}
	role := f.byProject[projectID][empID]
	if role == "" {
		return nil, domainproject.ErrNotFound
	}
	return &domainproject.Member{ProjectID: projectID, EmployeeID: empID, Role: role}, nil
}

func (f *fakeMembers) CountByRole(
	_ context.Context, projectID uuid.UUID, role domainproject.Role,
) (int, error) {
	n := 0
	for _, r := range f.byProject[projectID] {
		if r == role {
			n++
		}
	}
	return n, nil
}

// fakeTasks giữ một bảng Kanban trong bộ nhớ.
type fakeTasks struct {
	byID map[uuid.UUID]*domainproject.Task

	created    []*domainproject.Task
	updated    []*domainproject.Task
	deleted    []uuid.UUID
	renumbered int
	reorders   []reorderCall
}

type reorderCall struct {
	taskID    uuid.UUID
	status    domainproject.TaskStatus
	sortOrder float64
}

func newFakeTasks() *fakeTasks {
	return &fakeTasks{byID: map[uuid.UUID]*domainproject.Task{}}
}

// add đưa một task vào bảng, để bài kiểm thử dựng sẵn bối cảnh.
func (f *fakeTasks) add(t *domainproject.Task) *domainproject.Task {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	f.byID[t.ID] = t
	return t
}

func (f *fakeTasks) Create(_ context.Context, t *domainproject.Task) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	t.CreatedAt = time.Now()
	f.created = append(f.created, t)
	f.byID[t.ID] = t
	return nil
}

func (f *fakeTasks) Update(_ context.Context, t *domainproject.Task) error {
	if f.byID[t.ID] == nil {
		return domainproject.ErrNotFound
	}
	f.updated = append(f.updated, t)
	f.byID[t.ID] = t
	return nil
}

func (f *fakeTasks) SoftDelete(_ context.Context, id uuid.UUID) error {
	if f.byID[id] == nil {
		return domainproject.ErrNotFound
	}
	f.deleted = append(f.deleted, id)
	delete(f.byID, id)
	return nil
}

func (f *fakeTasks) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainproject.Task, error) {
	if t := f.byID[id]; t != nil {
		copied := *t
		return &copied, nil
	}
	return nil, domainproject.ErrNotFound
}

func (f *fakeTasks) List(
	context.Context, domainproject.TaskFilter,
) ([]*domainproject.Task, int, error) {
	return nil, 0, nil
}

// ListBoard trả về task gốc của một dự án, ĐÃ SẮP theo cột rồi sort_order.
//
// Thứ tự này là hợp đồng mà firstSortOrder và nextSortOrder dựa vào: chúng
// duyệt tuần tự và lấy phần tử khớp đầu tiên. Bản giả lập không sắp thì thuật
// toán sẽ trả kết quả ngẫu nhiên và bài kiểm thử đạt hay hỏng tuỳ lần chạy.
func (f *fakeTasks) ListBoard(
	_ context.Context, projectID uuid.UUID,
) ([]*domainproject.Task, error) {
	var out []*domainproject.Task
	for _, t := range f.byID {
		if t.ProjectID == projectID && t.ParentTaskID == nil {
			copied := *t
			out = append(out, &copied)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status < out[j].Status
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out, nil
}

func (f *fakeTasks) ListSubtasks(
	_ context.Context, parentID uuid.UUID,
) ([]*domainproject.Task, error) {
	var out []*domainproject.Task
	for _, t := range f.byID {
		if t.ParentTaskID != nil && *t.ParentTaskID == parentID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeTasks) CountSubtasks(_ context.Context, parentID uuid.UUID) (int, error) {
	list, _ := f.ListSubtasks(context.Background(), parentID)
	return len(list), nil
}

func (f *fakeTasks) SortOrderOf(_ context.Context, taskID uuid.UUID) (float64, error) {
	if t := f.byID[taskID]; t != nil {
		return t.SortOrder, nil
	}
	return 0, domainproject.ErrNotFound
}

func (f *fakeTasks) Reorder(
	_ context.Context, taskID uuid.UUID,
	status domainproject.TaskStatus, sortOrder float64,
) error {
	t := f.byID[taskID]
	if t == nil {
		return domainproject.ErrNotFound
	}
	t.Status = status
	t.SortOrder = sortOrder
	f.reorders = append(f.reorders, reorderCall{taskID, status, sortOrder})
	return nil
}

// RenumberColumn đánh số lại một cột theo bước đều 1000, giống hàm SQL thật.
func (f *fakeTasks) RenumberColumn(
	_ context.Context, projectID uuid.UUID, status domainproject.TaskStatus,
) error {
	f.renumbered++

	var col []*domainproject.Task
	for _, t := range f.byID {
		if t.ProjectID == projectID && t.Status == status && t.ParentTaskID == nil {
			col = append(col, t)
		}
	}
	sort.Slice(col, func(i, j int) bool { return col[i].SortOrder < col[j].SortOrder })

	for i, t := range col {
		t.SortOrder = float64(i+1) * sortOrderStep
	}
	return nil
}

func (f *fakeTasks) MaxSortOrder(
	_ context.Context, projectID uuid.UUID, status domainproject.TaskStatus,
) (float64, error) {
	max := 0.0
	for _, t := range f.byID {
		if t.ProjectID == projectID && t.Status == status && t.SortOrder > max {
			max = t.SortOrder
		}
	}
	return max, nil
}

type fakeComments struct {
	byID    map[uuid.UUID]*domainproject.Comment
	created []*domainproject.Comment
}

func newFakeComments() *fakeComments {
	return &fakeComments{byID: map[uuid.UUID]*domainproject.Comment{}}
}

func (f *fakeComments) Create(_ context.Context, c *domainproject.Comment) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	c.CreatedAt = time.Now()
	f.created = append(f.created, c)
	f.byID[c.ID] = c
	return nil
}

func (f *fakeComments) Update(_ context.Context, c *domainproject.Comment) error {
	if f.byID[c.ID] == nil {
		return domainproject.ErrNotFound
	}
	f.byID[c.ID] = c
	return nil
}

func (f *fakeComments) SoftDelete(_ context.Context, id uuid.UUID) error {
	if f.byID[id] == nil {
		return domainproject.ErrNotFound
	}
	delete(f.byID, id)
	return nil
}

func (f *fakeComments) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainproject.Comment, error) {
	if c := f.byID[id]; c != nil {
		return c, nil
	}
	return nil, domainproject.ErrNotFound
}

func (f *fakeComments) ListByTask(
	context.Context, uuid.UUID,
) ([]*domainproject.Comment, error) {
	return nil, nil
}

func (f *fakeComments) CountByTask(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}

type fakeAttachments struct{}

func (fakeAttachments) Create(context.Context, *domainproject.Attachment) error { return nil }
func (fakeAttachments) Delete(context.Context, uuid.UUID) error                 { return nil }

func (fakeAttachments) GetByID(
	context.Context, uuid.UUID,
) (*domainproject.Attachment, error) {
	return nil, domainproject.ErrNotFound
}

func (fakeAttachments) ListByTask(
	context.Context, uuid.UUID,
) ([]*domainproject.Attachment, error) {
	return nil, nil
}

type fakeActivities struct{ logged []*domainproject.Activity }

func (f *fakeActivities) Log(_ context.Context, a *domainproject.Activity) error {
	f.logged = append(f.logged, a)
	return nil
}

func (f *fakeActivities) ListByTask(
	context.Context, uuid.UUID, int,
) ([]*domainproject.Activity, error) {
	return nil, nil
}

type fakeTimelogs struct{}

func (fakeTimelogs) Create(context.Context, *domainproject.Timelog) error { return nil }
func (fakeTimelogs) Delete(context.Context, uuid.UUID) error              { return nil }

func (fakeTimelogs) GetByID(
	context.Context, uuid.UUID,
) (*domainproject.Timelog, error) {
	return nil, domainproject.ErrNotFound
}

func (fakeTimelogs) ListByTask(
	context.Context, uuid.UUID,
) ([]*domainproject.Timelog, error) {
	return nil, nil
}

func (fakeTimelogs) SumMinutesByTask(context.Context, uuid.UUID) (int, error) {
	return 0, nil
}

type fakeReports struct{}

func (fakeReports) ProjectProgress(
	context.Context, uuid.UUID,
) (*domainproject.ProjectProgress, error) {
	return &domainproject.ProjectProgress{}, nil
}

func (fakeReports) Workload(
	context.Context, []uuid.UUID, bool,
) ([]*domainproject.Workload, error) {
	return nil, nil
}

type fakeProjectEmployees struct {
	names   map[uuid.UUID]string
	missing map[uuid.UUID]bool
	depts   map[uuid.UUID]uuid.UUID
}

func (f *fakeProjectEmployees) Exists(_ context.Context, id uuid.UUID) (bool, error) {
	return !f.missing[id], nil
}

func (f *fakeProjectEmployees) NamesOf(
	_ context.Context, ids []uuid.UUID,
) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	for _, id := range ids {
		if n, ok := f.names[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

func (f *fakeProjectEmployees) DepartmentOf(
	_ context.Context, id uuid.UUID,
) (*uuid.UUID, error) {
	if d, ok := f.depts[id]; ok {
		return &d, nil
	}
	return nil, nil
}

type fakeEvents struct{ published []domainproject.Event }

func (f *fakeEvents) PublishEvent(_ context.Context, e domainproject.Event) error {
	f.published = append(f.published, e)
	return nil
}

type fakeProjectCompany struct{ id uuid.UUID }

func (f *fakeProjectCompany) CurrentCompanyID(context.Context) (uuid.UUID, error) {
	return f.id, nil
}

// projectHarness gom usecase và các bản giả lập.
type projectHarness struct {
	uc *Usecase

	projects   *fakeProjects
	members    *fakeMembers
	tasks      *fakeTasks
	comments   *fakeComments
	activities *fakeActivities
	employees  *fakeProjectEmployees
	events     *fakeEvents
	company    *fakeProjectCompany
}

func newProjectHarness() *projectHarness {
	h := &projectHarness{
		projects:   newFakeProjects(),
		members:    newFakeMembers(),
		tasks:      newFakeTasks(),
		comments:   newFakeComments(),
		activities: &fakeActivities{},
		employees: &fakeProjectEmployees{
			names:   map[uuid.UUID]string{},
			missing: map[uuid.UUID]bool{},
			depts:   map[uuid.UUID]uuid.UUID{},
		},
		events:  &fakeEvents{},
		company: &fakeProjectCompany{id: uuid.New()},
	}

	h.uc = NewUsecase(
		h.projects, h.members, h.tasks, h.comments,
		fakeAttachments{}, h.activities, fakeTimelogs{}, fakeReports{},
		h.employees, h.company, nil, h.events,
	)
	return h
}

// seedProject dựng một dự án đang hoạt động với một chủ dự án.
func (h *projectHarness) seedProject(owner uuid.UUID) *domainproject.Project {
	p := &domainproject.Project{
		ID:        uuid.New(),
		CompanyID: h.company.id,
		Code:      "TEST",
		Name:      "Dự án kiểm thử",
		OwnerID:   owner,
		Status:    domainproject.StatusActive,
	}
	h.projects.put(p)
	h.members.join(p.ID, owner, domainproject.RoleOwner)
	return p
}

// actorIn dựng actor có đủ quyền thô, phạm vi "self".
//
// Phạm vi "self" là có ý: nó buộc mọi phép kiểm tra truy cập phải đi qua bảng
// project_members thay vì được phạm vi toàn công ty cho qua. Đó chính là
// đường mà người dùng thường gặp nhất.
func actorIn(employeeID uuid.UUID) *domainauth.Actor {
	return &domainauth.Actor{
		EmployeeID: employeeID,
		Scope:      domainauth.ScopeSelf,
		Permissions: map[string]struct{}{
			domainauth.PermProjectRead:   {},
			domainauth.PermProjectCreate: {},
			domainauth.PermProjectUpdate: {},
			domainauth.PermProjectDelete: {},
			domainauth.PermTaskRead:      {},
			domainauth.PermTaskCreate:    {},
			domainauth.PermTaskUpdate:    {},
			domainauth.PermTaskDelete:    {},
		},
	}
}

// actorCompanyWide dựng actor có phạm vi toàn công ty.
func actorCompanyWide(employeeID uuid.UUID) *domainauth.Actor {
	a := actorIn(employeeID)
	a.Scope = domainauth.ScopeAll
	return a
}
