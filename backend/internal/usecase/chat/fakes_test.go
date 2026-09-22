package chat

import (
	"context"
	"time"

	"github.com/google/uuid"

	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
)

// Bộ giả lập cho cổng của module chat. Cùng cách viết với module chấm công:
// hiện thực đầy đủ interface, nhưng chỉ những method có hàm gán vào mới làm
// gì — nhờ vậy mỗi bài kiểm thử chỉ khai báo đúng thứ nó quan tâm.

type fakeConvs struct {
	// members quyết định ai là thành viên của hội thoại nào. Đây là cửa kiểm
	// soát truy cập của toàn module, nên hầu hết bài kiểm thử đều chạm tới nó.
	members map[uuid.UUID]map[uuid.UUID]*domainchat.Member
	convs   map[uuid.UUID]*domainchat.Conversation

	created  []*domainchat.Conversation
	updated  []*domainchat.Conversation
	added    []uuid.UUID
	removed  []uuid.UUID
	syncedTo map[uuid.UUID][]uuid.UUID

	bySource   func(field string, id uuid.UUID) *domainchat.Conversation
	byDirect   func(key string) *domainchat.Conversation
	markedRead []uuid.UUID
	unread     int
}

func newFakeConvs() *fakeConvs {
	return &fakeConvs{
		members:  map[uuid.UUID]map[uuid.UUID]*domainchat.Member{},
		convs:    map[uuid.UUID]*domainchat.Conversation{},
		syncedTo: map[uuid.UUID][]uuid.UUID{},
	}
}

// join ghi nhận một người là thành viên, để bài kiểm thử dựng sẵn bối cảnh.
func (f *fakeConvs) join(convID, empID uuid.UUID, admin bool) {
	if f.members[convID] == nil {
		f.members[convID] = map[uuid.UUID]*domainchat.Member{}
	}
	f.members[convID][empID] = &domainchat.Member{
		ConversationID: convID,
		EmployeeID:     empID,
		IsAdmin:        admin,
		EmployeeName:   "Thành viên",
	}
}

func (f *fakeConvs) put(c *domainchat.Conversation) { f.convs[c.ID] = c }

func (f *fakeConvs) Create(_ context.Context, c *domainchat.Conversation) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	c.CreatedAt = time.Now()
	f.created = append(f.created, c)
	f.convs[c.ID] = c
	return nil
}

func (f *fakeConvs) Update(_ context.Context, c *domainchat.Conversation) error {
	f.updated = append(f.updated, c)
	if existing := f.convs[c.ID]; existing != nil {
		existing.Name = c.Name
	}
	return nil
}

func (f *fakeConvs) SoftDelete(context.Context, uuid.UUID) error { return nil }

func (f *fakeConvs) ByID(
	_ context.Context,
	id, viewerID uuid.UUID,
) (*domainchat.Conversation, error) {
	c := f.convs[id]
	if c == nil {
		return nil, domainchat.ErrNotFound
	}
	// Sao chép rồi gắn góc nhìn người xem, đúng như repository thật làm.
	view := *c
	if m := f.members[id][viewerID]; m != nil {
		view.IsAdmin = m.IsAdmin
		view.IsPinned = m.IsPinned
		view.IsMuted = m.IsMuted
	}
	view.DisplayName = c.Name
	return &view, nil
}

func (f *fakeConvs) ByDirectKey(
	_ context.Context, _ uuid.UUID, key string,
) (*domainchat.Conversation, error) {
	if f.byDirect == nil {
		return nil, domainchat.ErrNotFound
	}
	if c := f.byDirect(key); c != nil {
		return c, nil
	}
	return nil, domainchat.ErrNotFound
}

func (f *fakeConvs) BySource(
	_ context.Context, field string, id uuid.UUID,
) (*domainchat.Conversation, error) {
	if f.bySource == nil {
		return nil, domainchat.ErrNotFound
	}
	if c := f.bySource(field, id); c != nil {
		return c, nil
	}
	return nil, domainchat.ErrNotFound
}

func (f *fakeConvs) ListFor(
	context.Context, uuid.UUID, string,
) ([]*domainchat.Conversation, error) {
	return nil, nil
}

func (f *fakeConvs) Members(
	_ context.Context, convID uuid.UUID,
) ([]*domainchat.Member, error) {
	out := make([]*domainchat.Member, 0, len(f.members[convID]))
	for _, m := range f.members[convID] {
		out = append(out, m)
	}
	return out, nil
}

func (f *fakeConvs) MemberIDs(
	_ context.Context, convID uuid.UUID,
) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(f.members[convID]))
	for id := range f.members[convID] {
		out = append(out, id)
	}
	return out, nil
}

func (f *fakeConvs) Member(
	_ context.Context, convID, empID uuid.UUID,
) (*domainchat.Member, error) {
	if m := f.members[convID][empID]; m != nil {
		return m, nil
	}
	return nil, domainchat.ErrNotMember
}

func (f *fakeConvs) AddMembers(
	_ context.Context, convID uuid.UUID, ids []uuid.UUID, admin bool,
) error {
	f.added = append(f.added, ids...)
	for _, id := range ids {
		f.join(convID, id, admin)
	}
	return nil
}

func (f *fakeConvs) RemoveMember(_ context.Context, convID, empID uuid.UUID) error {
	if f.members[convID][empID] == nil {
		return domainchat.ErrNotMember
	}
	delete(f.members[convID], empID)
	f.removed = append(f.removed, empID)
	return nil
}

func (f *fakeConvs) SetAdmin(_ context.Context, convID, empID uuid.UUID, admin bool) error {
	m := f.members[convID][empID]
	if m == nil {
		return domainchat.ErrNotMember
	}
	m.IsAdmin = admin
	return nil
}

func (f *fakeConvs) SyncMembers(
	_ context.Context, convID uuid.UUID, want []uuid.UUID,
) (int, int, error) {
	f.syncedTo[convID] = want
	return len(want), 0, nil
}

func (f *fakeConvs) SetPinned(_ context.Context, convID, empID uuid.UUID, v bool) error {
	m := f.members[convID][empID]
	if m == nil {
		return domainchat.ErrNotMember
	}
	m.IsPinned = v
	return nil
}

func (f *fakeConvs) SetMuted(_ context.Context, convID, empID uuid.UUID, v bool) error {
	m := f.members[convID][empID]
	if m == nil {
		return domainchat.ErrNotMember
	}
	m.IsMuted = v
	return nil
}

func (f *fakeConvs) MarkRead(_ context.Context, _, _, messageID uuid.UUID) error {
	f.markedRead = append(f.markedRead, messageID)
	return nil
}

func (f *fakeConvs) TotalUnread(context.Context, uuid.UUID) (int, error) {
	return f.unread, nil
}

type fakeMsgs struct {
	created []*domainchat.Message
	byID    map[uuid.UUID]*domainchat.Message

	// byClient mô phỏng bản ghi đã có cùng client_message_id.
	byClient map[string]*domainchat.Message

	edited  map[uuid.UUID]string
	deleted []uuid.UUID
	atts    []*domainchat.Attachment
	list    []*domainchat.Message
}

func newFakeMsgs() *fakeMsgs {
	return &fakeMsgs{
		byID:     map[uuid.UUID]*domainchat.Message{},
		byClient: map[string]*domainchat.Message{},
		edited:   map[uuid.UUID]string{},
	}
}

func (f *fakeMsgs) Create(_ context.Context, m *domainchat.Message) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.CreatedAt = time.Now()
	f.created = append(f.created, m)
	f.byID[m.ID] = m
	if m.ClientMessageID != "" {
		f.byClient[m.ClientMessageID] = m
	}
	return nil
}

func (f *fakeMsgs) ByClientID(
	_ context.Context, _, _ uuid.UUID, clientID string,
) (*domainchat.Message, error) {
	if clientID == "" {
		return nil, nil
	}
	return f.byClient[clientID], nil
}

func (f *fakeMsgs) ByID(_ context.Context, id uuid.UUID) (*domainchat.Message, error) {
	if m := f.byID[id]; m != nil {
		return m, nil
	}
	return nil, domainchat.ErrNotFound
}

func (f *fakeMsgs) List(
	context.Context, domainchat.MessageFilter,
) ([]*domainchat.Message, error) {
	return f.list, nil
}

func (f *fakeMsgs) Edit(_ context.Context, id uuid.UUID, content string) error {
	if f.byID[id] == nil {
		return domainchat.ErrNotFound
	}
	f.edited[id] = content
	f.byID[id].Content = content
	return nil
}

func (f *fakeMsgs) SoftDelete(_ context.Context, id uuid.UUID) error {
	m := f.byID[id]
	if m == nil {
		return domainchat.ErrNotFound
	}
	now := time.Now()
	m.DeletedAt = &now
	m.Content = ""
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeMsgs) AddAttachments(_ context.Context, items []*domainchat.Attachment) error {
	f.atts = append(f.atts, items...)
	return nil
}

func (f *fakeMsgs) AttachmentsOf(
	context.Context, []uuid.UUID,
) (map[uuid.UUID][]*domainchat.Attachment, error) {
	return map[uuid.UUID][]*domainchat.Attachment{}, nil
}

type fakeLookup struct {
	names       map[uuid.UUID]string
	sameCompany bool
	byDept      map[uuid.UUID][]uuid.UUID
	byProject   map[uuid.UUID][]uuid.UUID
}

func (f *fakeLookup) NamesOf(
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

func (f *fakeLookup) SameCompany(
	context.Context, uuid.UUID, []uuid.UUID,
) (bool, error) {
	return f.sameCompany, nil
}

func (f *fakeLookup) ByDepartment(
	_ context.Context, id uuid.UUID,
) ([]uuid.UUID, error) {
	return f.byDept[id], nil
}

func (f *fakeLookup) ByProject(_ context.Context, id uuid.UUID) ([]uuid.UUID, error) {
	return f.byProject[id], nil
}

type fakeSources struct{ list []domainchat.GroupSource }

func (f *fakeSources) Sources(
	context.Context, uuid.UUID,
) ([]domainchat.GroupSource, error) {
	return f.list, nil
}

type fakeChatPresence struct{ status map[uuid.UUID]string }

func (f *fakeChatPresence) StatusOf(
	context.Context, []uuid.UUID,
) (map[uuid.UUID]string, error) {
	return f.status, nil
}

type fakeChatPusher struct {
	events   []string
	payloads []any
	to       [][]uuid.UUID
}

func (f *fakeChatPusher) PushChat(
	_ context.Context, recipients []uuid.UUID, eventType string, payload any,
) {
	f.events = append(f.events, eventType)
	f.payloads = append(f.payloads, payload)
	f.to = append(f.to, recipients)
}

type fakeNotifier struct {
	calls      int
	recipients []uuid.UUID
	preview    string
}

func (f *fakeNotifier) NotifyNewMessage(
	_ context.Context, recipients []uuid.UUID, _ uuid.UUID,
	_, _, preview, _ string,
) error {
	f.calls++
	f.recipients = recipients
	f.preview = preview
	return nil
}

type fakeStorage struct{ keys []string }

func (f *fakeStorage) PresignPut(
	_ context.Context, key, _ string, _ time.Duration,
) (string, error) {
	f.keys = append(f.keys, key)
	return "https://r2.test/" + key, nil
}

func (f *fakeStorage) PresignGet(
	_ context.Context, key string, _ time.Duration,
) (string, error) {
	return "https://r2.test/get/" + key, nil
}

type fakeChatCompany struct{ id uuid.UUID }

func (f *fakeChatCompany) CurrentCompanyID(context.Context) (uuid.UUID, error) {
	return f.id, nil
}

// chatHarness gom usecase và các bản giả lập.
type chatHarness struct {
	uc *Usecase

	convs    *fakeConvs
	msgs     *fakeMsgs
	lookup   *fakeLookup
	sources  *fakeSources
	presence *fakeChatPresence
	pusher   *fakeChatPusher
	notifier *fakeNotifier
	storage  *fakeStorage
	company  *fakeChatCompany
}

func newChatHarness() *chatHarness {
	h := &chatHarness{
		convs:    newFakeConvs(),
		msgs:     newFakeMsgs(),
		lookup:   &fakeLookup{names: map[uuid.UUID]string{}, sameCompany: true},
		sources:  &fakeSources{},
		presence: &fakeChatPresence{status: map[uuid.UUID]string{}},
		pusher:   &fakeChatPusher{},
		notifier: &fakeNotifier{},
		storage:  &fakeStorage{},
		company:  &fakeChatCompany{id: uuid.New()},
	}

	h.uc = NewUsecase(
		h.convs, h.msgs, h.lookup, h.sources,
		h.presence, h.pusher, h.notifier, h.storage, h.company,
	)
	return h
}
