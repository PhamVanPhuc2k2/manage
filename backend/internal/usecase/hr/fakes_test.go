package hr

import (
	"context"
	"strings"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
)

// Bản giả lập cho các repository còn lại của module nhân sự.
//
// Chúng giữ dữ liệu thật trong bộ nhớ và tự suy ra kết quả từ dữ liệu đó,
// thay vì trả về giá trị dựng sẵn. Một bản giả lập trả dữ liệu cứng sẽ khiến
// phép thử đạt kể cả khi usecase gọi sai thứ tự hoặc quên gọi — đúng những
// lỗi ta muốn bắt.

// =========================================================================
// NHÂN VIÊN
// =========================================================================

type fakeEmployees struct {
	byID   map[uuid.UUID]*domainhr.Employee
	codes  map[string]bool
	emails map[string]bool

	// lastFilter giữ lại bộ lọc của lần List gần nhất. Phạm vi dữ liệu được
	// áp bằng cách ĐẶT TRƯỜNG trong filter, nên cách duy nhất để kiểm chứng
	// nó là nhìn vào filter mà usecase gửi xuống.
	lastFilter domainhr.EmployeeFilter

	deleted []uuid.UUID
	updated []*domainhr.Employee
	avatars map[uuid.UUID]string
}

func newFakeEmployees() *fakeEmployees {
	return &fakeEmployees{
		byID:    map[uuid.UUID]*domainhr.Employee{},
		codes:   map[string]bool{},
		emails:  map[string]bool{},
		avatars: map[uuid.UUID]string{},
	}
}

func (f *fakeEmployees) add(e *domainhr.Employee) *domainhr.Employee {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.byID[e.ID] = e
	return e
}

func (f *fakeEmployees) Create(_ context.Context, e *domainhr.Employee) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	f.byID[e.ID] = e
	return nil
}

func (f *fakeEmployees) Update(_ context.Context, e *domainhr.Employee) error {
	if f.byID[e.ID] == nil {
		return domainhr.ErrNotFound
	}
	f.updated = append(f.updated, e)
	f.byID[e.ID] = e
	return nil
}

func (f *fakeEmployees) SoftDelete(_ context.Context, id uuid.UUID) error {
	if f.byID[id] == nil {
		return domainhr.ErrNotFound
	}
	f.deleted = append(f.deleted, id)
	delete(f.byID, id)
	return nil
}

func (f *fakeEmployees) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainhr.Employee, error) {
	if e := f.byID[id]; e != nil {
		copied := *e
		return &copied, nil
	}
	return nil, domainhr.ErrNotFound
}

func (f *fakeEmployees) List(
	_ context.Context, filter domainhr.EmployeeFilter,
) ([]*domainhr.Employee, int, error) {
	f.lastFilter = filter

	out := make([]*domainhr.Employee, 0, len(f.byID))
	for _, e := range f.byID {
		copied := *e
		out = append(out, &copied)
	}
	return out, len(out), nil
}

func (f *fakeEmployees) ListAncestorIDs(
	_ context.Context, id uuid.UUID,
) ([]uuid.UUID, error) {
	var out []uuid.UUID
	seen := map[uuid.UUID]bool{}

	cur := f.byID[id]
	// Chặn trên 1000 bước vì lý do giống bên phòng ban: chuỗi cấp trên đã có
	// vòng lặp sẽ làm chính bài kiểm thử treo thay vì báo hỏng.
	for i := 0; cur != nil && cur.ManagerID != nil && i < 1000; i++ {
		m := *cur.ManagerID
		if seen[m] {
			break
		}
		seen[m] = true
		out = append(out, m)
		cur = f.byID[m]
	}
	return out, nil
}

func (f *fakeEmployees) ExistsCode(
	_ context.Context, _ uuid.UUID, code string, _ *uuid.UUID,
) (bool, error) {
	return f.codes[strings.TrimSpace(code)], nil
}

func (f *fakeEmployees) ExistsEmail(
	_ context.Context, email string, _ *uuid.UUID,
) (bool, error) {
	return f.emails[strings.TrimSpace(email)], nil
}

func (f *fakeEmployees) UpdateAvatarKey(
	_ context.Context, id uuid.UUID, key string,
) error {
	f.avatars[id] = key
	return nil
}

// =========================================================================
// CHỨC VỤ
// =========================================================================

type fakePositions struct {
	byID   map[uuid.UUID]*domainhr.Position
	codes  map[string]bool
	counts map[uuid.UUID]int

	deleted []uuid.UUID
}

func newFakePositions() *fakePositions {
	return &fakePositions{
		byID:   map[uuid.UUID]*domainhr.Position{},
		codes:  map[string]bool{},
		counts: map[uuid.UUID]int{},
	}
}

func (f *fakePositions) add(p *domainhr.Position) *domainhr.Position {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.byID[p.ID] = p
	return p
}

func (f *fakePositions) Create(_ context.Context, p *domainhr.Position) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.byID[p.ID] = p
	return nil
}

func (f *fakePositions) Update(_ context.Context, p *domainhr.Position) error {
	if f.byID[p.ID] == nil {
		return domainhr.ErrNotFound
	}
	f.byID[p.ID] = p
	return nil
}

func (f *fakePositions) SoftDelete(_ context.Context, id uuid.UUID) error {
	if f.byID[id] == nil {
		return domainhr.ErrNotFound
	}
	f.deleted = append(f.deleted, id)
	delete(f.byID, id)
	return nil
}

func (f *fakePositions) GetByID(
	_ context.Context, id uuid.UUID,
) (*domainhr.Position, error) {
	if p := f.byID[id]; p != nil {
		copied := *p
		return &copied, nil
	}
	return nil, domainhr.ErrNotFound
}

func (f *fakePositions) List(
	context.Context, uuid.UUID,
) ([]*domainhr.Position, error) {
	out := make([]*domainhr.Position, 0, len(f.byID))
	for _, p := range f.byID {
		copied := *p
		out = append(out, &copied)
	}
	return out, nil
}

func (f *fakePositions) CountEmployees(
	_ context.Context, id uuid.UUID,
) (int, error) {
	return f.counts[id], nil
}

func (f *fakePositions) ExistsCode(
	_ context.Context, _ uuid.UUID, code string, _ *uuid.UUID,
) (bool, error) {
	return f.codes[strings.TrimSpace(code)], nil
}

// =========================================================================
// TÀI KHOẢN
// =========================================================================

type fakeUsers struct {
	byID         map[uuid.UUID]*domainhr.User
	byEmployeeID map[uuid.UUID]*domainhr.User
	roleCodes    map[uuid.UUID][]string

	assigned []string // "userID:roleID"
	removed  []string
	actives  map[uuid.UUID]bool
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{
		byID:         map[uuid.UUID]*domainhr.User{},
		byEmployeeID: map[uuid.UUID]*domainhr.User{},
		roleCodes:    map[uuid.UUID][]string{},
		actives:      map[uuid.UUID]bool{},
	}
}

func (f *fakeUsers) add(u *domainhr.User, codes ...string) *domainhr.User {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	f.byID[u.ID] = u
	f.byEmployeeID[u.EmployeeID] = u
	f.roleCodes[u.ID] = codes
	return u
}

func (f *fakeUsers) Create(_ context.Context, u *domainhr.User) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	f.byID[u.ID] = u
	f.byEmployeeID[u.EmployeeID] = u
	return nil
}

func (f *fakeUsers) FindByEmail(
	_ context.Context, email string,
) (*domainhr.User, error) {
	for _, u := range f.byID {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, domainhr.ErrNotFound
}

func (f *fakeUsers) FindByID(
	_ context.Context, id uuid.UUID,
) (*domainhr.User, error) {
	if u := f.byID[id]; u != nil {
		return u, nil
	}
	return nil, domainhr.ErrNotFound
}

func (f *fakeUsers) FindByEmployeeID(
	_ context.Context, employeeID uuid.UUID,
) (*domainhr.User, error) {
	if u := f.byEmployeeID[employeeID]; u != nil {
		return u, nil
	}
	return nil, domainhr.ErrNotFound
}

func (f *fakeUsers) UpdatePasswordHash(context.Context, uuid.UUID, string) error {
	return nil
}
func (f *fakeUsers) SetMustChangePassword(context.Context, uuid.UUID, bool) error {
	return nil
}
func (f *fakeUsers) UpdateLastLogin(context.Context, uuid.UUID) error         { return nil }
func (f *fakeUsers) IncrementFailedAttempts(context.Context, uuid.UUID) error { return nil }
func (f *fakeUsers) ResetFailedAttempts(context.Context, uuid.UUID) error     { return nil }

func (f *fakeUsers) SetActive(_ context.Context, id uuid.UUID, active bool) error {
	f.actives[id] = active
	return nil
}

func (f *fakeUsers) ListRoleCodes(
	_ context.Context, userID uuid.UUID,
) ([]string, error) {
	return f.roleCodes[userID], nil
}

func (f *fakeUsers) AssignRole(
	_ context.Context, userID, roleID, _ uuid.UUID,
) error {
	f.assigned = append(f.assigned, userID.String()+":"+roleID.String())
	return nil
}

func (f *fakeUsers) RemoveRole(_ context.Context, userID, roleID uuid.UUID) error {
	f.removed = append(f.removed, userID.String()+":"+roleID.String())
	return nil
}

// =========================================================================
// VAI TRÒ
// =========================================================================

type fakeRoles struct{ byCode map[string]*domainhr.Role }

func newFakeRoles(codes ...string) *fakeRoles {
	f := &fakeRoles{byCode: map[string]*domainhr.Role{}}
	for _, c := range codes {
		f.byCode[c] = &domainhr.Role{ID: uuid.New(), Code: c, Name: c}
	}
	return f
}

func (f *fakeRoles) List(context.Context) ([]*domainhr.Role, error) {
	out := make([]*domainhr.Role, 0, len(f.byCode))
	for _, r := range f.byCode {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeRoles) GetByCode(
	_ context.Context, code string,
) (*domainhr.Role, error) {
	if r := f.byCode[code]; r != nil {
		return r, nil
	}
	return nil, domainhr.ErrNotFound
}

// =========================================================================
// TIỆN ÍCH DỰNG ACTOR
// =========================================================================

// actorAll là người có phạm vi toàn công ty — dùng cho các phép thử không
// quan tâm tới phân quyền, để kết quả phản ánh đúng logic nghiệp vụ.
func actorAll(roles ...string) *domainauth.Actor {
	return &domainauth.Actor{
		UserID:     uuid.New(),
		EmployeeID: uuid.New(),
		Roles:      roles,
		Scope:      domainauth.ScopeAll,
	}
}

func actorSelf(employeeID uuid.UUID) *domainauth.Actor {
	return &domainauth.Actor{
		UserID:     uuid.New(),
		EmployeeID: employeeID,
		Scope:      domainauth.ScopeSelf,
	}
}
