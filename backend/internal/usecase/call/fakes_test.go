package call

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
)

// Bản giả lập cho module gọi.
//
// Chúng giữ trạng thái thật trong bộ nhớ và mô phỏng cả những ràng buộc mà
// database áp: đặc biệt là "mỗi hội thoại chỉ một cuộc gọi đang chạy" và
// phép cập nhật trạng thái có điều kiện. Bỏ qua hai thứ đó thì bộ kiểm thử
// sẽ đạt với một cài đặt mà database từ chối.

func statusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	code, _ := apperror.HTTPStatus(err)
	return code
}

// =========================================================================
// KHO CUỘC GỌI
// =========================================================================

type fakeCalls struct {
	byID map[uuid.UUID]*domaincall.Call

	created []*domaincall.Call
	// transitions ghi mọi lần đổi trạng thái, dạng "from->to:reason".
	transitions []string

	liveForErr error
}

func newFakeCalls() *fakeCalls {
	return &fakeCalls{byID: map[uuid.UUID]*domaincall.Call{}}
}

func (f *fakeCalls) add(c *domaincall.Call) *domaincall.Call {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.RoomName == "" {
		c.RoomName = domaincall.RoomNameFor(c.ID)
	}
	f.byID[c.ID] = c
	return c
}

func (f *fakeCalls) Create(_ context.Context, c *domaincall.Call) error {
	// Mô phỏng chỉ mục một phần uq_calls_active_per_conversation.
	//
	// Không mô phỏng thì phép thử "không mở được hai cuộc gọi song song" sẽ
	// đạt ở đây và hỏng trên database thật — đúng kiểu sai lệch mà bản giả
	// lập sinh ra khi nó dễ tính hơn thứ nó thay thế.
	for _, other := range f.byID {
		if other.ConversationID == c.ConversationID && other.Status.Live() {
			return errors.New("uq_calls_active_per_conversation")
		}
	}
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	f.created = append(f.created, c)
	f.byID[c.ID] = c
	return nil
}

func (f *fakeCalls) GetByID(
	_ context.Context, id uuid.UUID,
) (*domaincall.Call, error) {
	if c := f.byID[id]; c != nil {
		copied := *c
		return &copied, nil
	}
	return nil, domaincall.ErrNotFound
}

func (f *fakeCalls) LiveInConversation(
	_ context.Context, conversationID uuid.UUID,
) (*domaincall.Call, error) {
	for _, c := range f.byID {
		if c.ConversationID == conversationID && c.Status.Live() {
			copied := *c
			return &copied, nil
		}
	}
	return nil, domaincall.ErrNotFound
}

// liveMembers là danh sách người đang trong cuộc gọi, do phép thử dựng sẵn.
var liveMembers = map[uuid.UUID]uuid.UUID{}

func (f *fakeCalls) LiveForEmployee(
	_ context.Context, employeeID uuid.UUID,
) (*domaincall.Call, error) {
	if f.liveForErr != nil {
		return nil, f.liveForErr
	}
	if callID, ok := liveMembers[employeeID]; ok {
		if c := f.byID[callID]; c != nil && c.Status.Live() {
			copied := *c
			return &copied, nil
		}
	}
	return nil, domaincall.ErrNotFound
}

// UpdateStatus chỉ đổi khi trạng thái hiện tại ĐÚNG như mong đợi.
//
// Mô phỏng phép cập nhật có điều kiện của repository thật. Nhờ vậy phép thử
// "hai người cùng cúp máy" mới có nghĩa: lời gọi thua phải nhận ErrNotFound
// chứ không âm thầm ghi đè lý do kết thúc của lời gọi thắng.
func (f *fakeCalls) UpdateStatus(
	_ context.Context, id uuid.UUID, from, to domaincall.Status, reason string,
) error {
	c := f.byID[id]
	if c == nil || c.Status != from {
		return domaincall.ErrNotFound
	}
	f.transitions = append(f.transitions,
		string(from)+"->"+string(to)+":"+reason)
	c.Status = to
	c.EndReason = reason
	if to.Final() {
		now := time.Now()
		c.EndedAt = &now
	}
	return nil
}

func (f *fakeCalls) SetRelayRatio(
	_ context.Context, id uuid.UUID, ratio float64,
) error {
	if c := f.byID[id]; c != nil {
		c.RelayRatio = &ratio
	}
	return nil
}

func (f *fakeCalls) ListForConversation(
	_ context.Context, conversationID uuid.UUID, limit int,
) ([]*domaincall.Call, error) {
	var out []*domaincall.Call
	for _, c := range f.byID {
		if c.ConversationID == conversationID {
			copied := *c
			out = append(out, &copied)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeCalls) ExpireRinging(
	_ context.Context, olderThan time.Time,
) ([]*domaincall.Call, error) {
	var out []*domaincall.Call
	for _, c := range f.byID {
		if c.Status == domaincall.StatusRinging && c.StartedAt.Before(olderThan) {
			c.Status = domaincall.StatusMissed
			c.EndReason = domaincall.ReasonTimeout
			copied := *c
			out = append(out, &copied)
		}
	}
	return out, nil
}

// =========================================================================
// NGƯỜI THAM GIA
// =========================================================================

type participantKey struct {
	callID     uuid.UUID
	employeeID uuid.UUID
}

type fakeParticipants struct {
	rows map[participantKey]*domaincall.Participant
}

func newFakeParticipants() *fakeParticipants {
	return &fakeParticipants{rows: map[participantKey]*domaincall.Participant{}}
}

func (f *fakeParticipants) Invite(
	_ context.Context, callID uuid.UUID, employeeIDs []uuid.UUID,
) error {
	for _, id := range employeeIDs {
		k := participantKey{callID, id}
		if f.rows[k] == nil {
			f.rows[k] = &domaincall.Participant{
				ID: uuid.New(), CallID: callID, EmployeeID: id,
			}
		}
	}
	return nil
}

func (f *fakeParticipants) Join(
	_ context.Context, callID, employeeID uuid.UUID, at time.Time,
) error {
	k := participantKey{callID, employeeID}
	p := f.rows[k]
	if p == nil {
		p = &domaincall.Participant{
			ID: uuid.New(), CallID: callID, EmployeeID: employeeID,
		}
		f.rows[k] = p
	}
	p.JoinedAt = &at
	// Vào lại sau khi rớt mạng chỉ xoá left_at, không tạo dòng mới.
	p.LeftAt = nil
	return nil
}

func (f *fakeParticipants) Leave(
	_ context.Context, callID, employeeID uuid.UUID, at time.Time,
) error {
	if p := f.rows[participantKey{callID, employeeID}]; p != nil {
		p.LeftAt = &at
	}
	return nil
}

func (f *fakeParticipants) SetTracks(
	_ context.Context, callID, employeeID uuid.UUID, audio, video, screen bool,
) error {
	if p := f.rows[participantKey{callID, employeeID}]; p != nil {
		// Chỉ BẬT, không bao giờ tắt — đây là dấu vết audit.
		p.HadAudio = p.HadAudio || audio
		p.HadVideo = p.HadVideo || video
		p.HadScreen = p.HadScreen || screen
	}
	return nil
}

func (f *fakeParticipants) SetRelay(
	_ context.Context, callID, employeeID uuid.UUID, relay bool,
) error {
	if p := f.rows[participantKey{callID, employeeID}]; p != nil {
		p.UsedRelay = &relay
	}
	return nil
}

func (f *fakeParticipants) ListForCall(
	_ context.Context, callID uuid.UUID,
) ([]*domaincall.Participant, error) {
	var out []*domaincall.Participant
	for k, p := range f.rows {
		if k.callID == callID {
			copied := *p
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (f *fakeParticipants) RoomState(
	_ context.Context, callID uuid.UUID,
) (inRoom, pending int, err error) {
	for k, p := range f.rows {
		if k.callID != callID {
			continue
		}
		switch {
		case p.InRoom():
			inRoom++
		case p.JoinedAt == nil && p.LeftAt == nil:
			// Đã được mời, chưa bắt máy, cũng chưa từ chối.
			pending++
		}
	}
	return inRoom, pending, nil
}

// =========================================================================
// CỔNG SANG MODULE KHÁC
// =========================================================================

type fakeConversations struct {
	members map[uuid.UUID][]uuid.UUID
	err     error
}

func (f *fakeConversations) IsMember(
	_ context.Context, conversationID, employeeID uuid.UUID,
) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	for _, id := range f.members[conversationID] {
		if id == employeeID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeConversations) MemberIDs(
	_ context.Context, conversationID uuid.UUID,
) ([]uuid.UUID, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.members[conversationID], nil
}

type fakeEmployees struct{ names map[uuid.UUID]string }

func (f *fakeEmployees) NamesOf(
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

type fakeMessenger struct {
	posted []string
	err    error
}

func (f *fakeMessenger) PostSystem(
	_ context.Context, _ uuid.UUID, content string,
) error {
	if f.err != nil {
		return f.err
	}
	f.posted = append(f.posted, content)
	return nil
}

// =========================================================================
// HẠ TẦNG MEDIA
// =========================================================================

type fakeMedia struct {
	tokens []string
	closed []string

	// media là thứ SFU "thấy" trong phòng, do từng phép thử đặt vào.
	media []domaincall.ParticipantMedia
	// askedBeforeClose ghi lại thứ tự: hỏi luồng media PHẢI trước khi
	// đóng phòng, vì sau khi đóng thì không còn ai để hỏi.
	askedBeforeClose bool

	tokenErr error
	closeErr error
}

func (f *fakeMedia) RoomMedia(
	_ context.Context, _ string,
) ([]domaincall.ParticipantMedia, error) {
	f.askedBeforeClose = len(f.closed) == 0
	return f.media, nil
}

func (f *fakeMedia) IssueToken(
	_ context.Context, roomName, identity, _ string, _ bool, _ time.Duration,
) (string, error) {
	if f.tokenErr != nil {
		return "", f.tokenErr
	}
	t := "token:" + roomName + ":" + identity
	f.tokens = append(f.tokens, t)
	return t, nil
}

func (f *fakeMedia) ICEServers(
	_ context.Context, _ string, _ time.Duration,
) ([]domaincall.ICEServer, error) {
	return []domaincall.ICEServer{
		{URLs: []string{"stun:stun.example:3478"}},
		{URLs: []string{"turn:turn.example:3478"}, Username: "u", Credential: "c"},
	}, nil
}

func (f *fakeMedia) CloseRoom(_ context.Context, roomName string) error {
	if f.closeErr != nil {
		return f.closeErr
	}
	f.closed = append(f.closed, roomName)
	return nil
}

// =========================================================================
// SIGNALING
// =========================================================================

type sentSignal struct {
	to      []uuid.UUID
	event   string
	payload any
}

type fakeSignaler struct{ sent []sentSignal }

func (f *fakeSignaler) PushCall(
	_ context.Context, recipients []uuid.UUID, eventType string, payload any,
) {
	f.sent = append(f.sent, sentSignal{to: recipients, event: eventType, payload: payload})
}

// eventsTo trả về các sự kiện đã gửi tới một người.
func (f *fakeSignaler) eventsTo(id uuid.UUID) []string {
	var out []string
	for _, s := range f.sent {
		for _, r := range s.to {
			if r == id {
				out = append(out, s.event)
				break
			}
		}
	}
	return out
}

// countEvent đếm số lần một sự kiện được gửi đi.
func (f *fakeSignaler) countEvent(event string) int {
	n := 0
	for _, s := range f.sent {
		if s.event == event {
			n++
		}
	}
	return n
}

// =========================================================================
// HARNESS
// =========================================================================

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

type harness struct {
	uc *Usecase

	calls         *fakeCalls
	participants  *fakeParticipants
	conversations *fakeConversations
	employees     *fakeEmployees
	messenger     *fakeMessenger
	media         *fakeMedia
	signal        *fakeSignaler

	now time.Time
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	// Dọn bản đồ dùng chung giữa các phép thử. Không dọn thì phép thử này
	// thấy người bận của phép thử trước, và lỗi chỉ xuất hiện khi chạy cả
	// gói chứ không khi chạy riêng — loại lỗi khó lần nhất.
	liveMembers = map[uuid.UUID]uuid.UUID{}

	now := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)

	h := &harness{
		calls:         newFakeCalls(),
		participants:  newFakeParticipants(),
		conversations: &fakeConversations{members: map[uuid.UUID][]uuid.UUID{}},
		employees:     &fakeEmployees{names: map[uuid.UUID]string{}},
		messenger:     &fakeMessenger{},
		media:         &fakeMedia{},
		signal:        &fakeSignaler{},
		now:           now,
	}

	h.uc = NewUsecase(
		h.calls, h.participants, h.conversations,
		h.employees, h.messenger, h.media, h.signal,
	)
	h.uc.SetClock(fixedClock{now: now})
	return h
}

// conversation dựng một hội thoại với danh sách thành viên.
func (h *harness) conversation(members ...uuid.UUID) uuid.UUID {
	id := uuid.New()
	h.conversations.members[id] = members
	for i, m := range members {
		h.employees.names[m] = "Thành viên " + string(rune('A'+i))
	}
	return id
}

func actorOf(employeeID uuid.UUID) *domainauth.Actor {
	return &domainauth.Actor{
		UserID:     uuid.New(),
		EmployeeID: employeeID,
		Scope:      domainauth.ScopeAll,
		Permissions: map[string]struct{}{
			"call:start": {}, "call:read": {},
		},
	}
}

// Ràng buộc kiểu: bản giả lập phải khớp cổng ở tầng domain.
var (
	_ domaincall.Repository            = (*fakeCalls)(nil)
	_ domaincall.ParticipantRepository = (*fakeParticipants)(nil)
	_ domaincall.ConversationLookup    = (*fakeConversations)(nil)
	_ domaincall.EmployeeLookup        = (*fakeEmployees)(nil)
	_ domaincall.SystemMessenger       = (*fakeMessenger)(nil)
	_ domaincall.MediaServer           = (*fakeMedia)(nil)
	_ domaincall.Signaler              = (*fakeSignaler)(nil)
)
