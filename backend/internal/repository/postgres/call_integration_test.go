//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	domaincall "github.com/PhamVanPhuc2k2/manage/internal/domain/call"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/postgres"
)

// Integration test cho kho cuộc gọi.
//
// Gói này kiểm đúng những thứ bản giả lập ở tầng usecase đang MÔ PHỎNG:
// chỉ mục một phần "mỗi hội thoại một cuộc gọi đang chạy", phép cập nhật
// trạng thái có điều kiện, và cách đếm người trong phòng. Nếu bản giả lập
// nói một đằng và SQL làm một nẻo thì bộ kiểm thử usecase đang bảo vệ một
// hành vi không tồn tại.

// callFixture dựng công ty, hai nhân viên và một hội thoại.
type callFixture struct {
	companyID      uuid.UUID
	conversationID uuid.UUID
	alice          uuid.UUID
	bob            uuid.UUID
	carol          uuid.UUID
}

func seedCallFixture(t *testing.T) callFixture {
	t.Helper()
	ctx := context.Background()

	companyID := newCompany(t)
	empRepo := NewEmployeeRepository(&postgres.DB{Pool: testPool})

	mk := func(code, email string) uuid.UUID {
		e := &domainhr.Employee{
			CompanyID:    companyID,
			EmployeeCode: code,
			FullName:     "Nguyễn " + code,
			Email:        email,
			WorkMode:     domainhr.WorkModeOnsite,
			Status:       domainhr.StatusOfficial,
			JoinedAt:     time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		if err := empRepo.Create(ctx, e); err != nil {
			t.Fatalf("tạo nhân viên %s: %v", code, err)
		}
		return e.ID
	}

	f := callFixture{
		companyID: companyID,
		alice:     mk("A001", "a@abc.vn"),
		bob:       mk("B001", "b@abc.vn"),
		carol:     mk("C001", "c@abc.vn"),
	}

	// Hội thoại nhóm, chèn thẳng: module chat không phải thứ đang kiểm ở đây.
	f.conversationID = uuid.New()
	if _, err := testPool.Exec(ctx,
		`INSERT INTO conversations (id, company_id, kind, name)
		 VALUES ($1, $2, 'group', 'Nhóm kiểm thử')`,
		f.conversationID, companyID); err != nil {
		t.Fatalf("tạo hội thoại: %v", err)
	}
	return f
}

func newCall(t *testing.T, repo *CallRepository, f callFixture,
	kind domaincall.Kind) *domaincall.Call {
	t.Helper()

	c := &domaincall.Call{
		ID:             uuid.New(),
		ConversationID: f.conversationID,
		InitiatorID:    &f.alice,
		Kind:           kind,
		Status:         domaincall.StatusRinging,
		StartedAt:      time.Now(),
	}
	c.RoomName = domaincall.RoomNameFor(c.ID)

	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatalf("tạo cuộc gọi: %v", err)
	}
	return c
}

// =========================================================================
// CHỈ MỤC MỘT PHẦN: MỘT CUỘC GỌI MỖI HỘI THOẠI
// =========================================================================

// TestOnlyOneLiveCallPerConversation là phép thử quan trọng nhất của gói.
//
// Không có ràng buộc này thì hai người cùng bấm gọi trong một giây sẽ tạo
// hai phòng, mỗi người vào một phòng, và cả hai ngồi nhìn màn hình trống.
// Chỉ mục một phần là cách duy nhất diễn đạt "duy nhất trong số những dòng
// đang hoạt động", và nó chỉ tồn tại trong SQL — không unit test nào thấy.
func TestOnlyOneLiveCallPerConversation(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	newCall(t, repo, f, domaincall.KindVideo)

	second := &domaincall.Call{
		ID:             uuid.New(),
		ConversationID: f.conversationID,
		InitiatorID:    &f.bob,
		Kind:           domaincall.KindVideo,
		Status:         domaincall.StatusRinging,
		StartedAt:      time.Now(),
	}
	second.RoomName = domaincall.RoomNameFor(second.ID)

	err := repo.Create(ctx, second)
	if err == nil {
		t.Fatal("database cho phép hai cuộc gọi cùng chạy trong một hội thoại")
	}
	if !errors.Is(err, domaincall.ErrNotFound) {
		t.Errorf("lỗi = %v, muốn ErrNotFound (đã dịch từ lỗi trùng khoá)", err)
	}
}

// TestNewCallAllowedAfterPreviousEnds: ràng buộc chỉ áp cho cuộc gọi đang
// chạy. Chặn cả cuộc đã kết thúc thì một hội thoại chỉ gọi được đúng một
// lần trong đời.
func TestNewCallAllowedAfterPreviousEnds(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	first := newCall(t, repo, f, domaincall.KindVideo)
	if err := repo.UpdateStatus(ctx, first.ID,
		domaincall.StatusRinging, domaincall.StatusEnded,
		domaincall.ReasonHangup); err != nil {
		t.Fatalf("kết thúc cuộc gọi đầu: %v", err)
	}

	second := &domaincall.Call{
		ID:             uuid.New(),
		ConversationID: f.conversationID,
		InitiatorID:    &f.bob,
		Kind:           domaincall.KindAudio,
		Status:         domaincall.StatusRinging,
		StartedAt:      time.Now(),
	}
	second.RoomName = domaincall.RoomNameFor(second.ID)

	if err := repo.Create(ctx, second); err != nil {
		t.Errorf("gọi lại sau khi cuộc trước kết thúc phải được: %v", err)
	}
}

// =========================================================================
// CẬP NHẬT TRẠNG THÁI CÓ ĐIỀU KIỆN
// =========================================================================

// TestUpdateStatusIsConditional khoá lại cơ chế chống ghi đè.
//
// Hai người cùng bấm cúp máy thì chỉ một lời gọi được thắng. Không có mệnh
// đề `AND status = $2` thì lời gọi thứ hai ghi đè lý do kết thúc của lời
// gọi thứ nhất, và lịch sử cuộc gọi nói sai mà không có lỗi nào.
func TestUpdateStatusIsConditional(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)

	if err := repo.UpdateStatus(ctx, c.ID,
		domaincall.StatusRinging, domaincall.StatusCancelled,
		domaincall.ReasonHangup); err != nil {
		t.Fatalf("lần một phải thành công: %v", err)
	}

	// Lời gọi thứ hai vẫn tưởng cuộc gọi đang ringing.
	err := repo.UpdateStatus(ctx, c.ID,
		domaincall.StatusRinging, domaincall.StatusMissed,
		domaincall.ReasonTimeout)
	if !errors.Is(err, domaincall.ErrNotFound) {
		t.Errorf("lỗi = %v, muốn ErrNotFound", err)
	}

	got, err := repo.GetByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("đọc lại: %v", err)
	}
	if got.Status != domaincall.StatusCancelled {
		t.Errorf("trạng thái = %q, lời gọi thua đã ghi đè", got.Status)
	}
	if got.EndReason != domaincall.ReasonHangup {
		t.Errorf("lý do = %q, muốn %q", got.EndReason, domaincall.ReasonHangup)
	}
}

// TestEndedAtSetOnceOnly: ghi đè ended_at sẽ kéo dài thời lượng cuộc gọi
// mỗi lần có ai gọi lại hàm này, và con số đó đi vào báo cáo.
func TestEndedAtSetOnceOnly(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)

	if err := repo.UpdateStatus(ctx, c.ID,
		domaincall.StatusRinging, domaincall.StatusActive, ""); err != nil {
		t.Fatalf("chuyển sang active: %v", err)
	}
	if err := repo.UpdateStatus(ctx, c.ID,
		domaincall.StatusActive, domaincall.StatusEnded,
		domaincall.ReasonHangup); err != nil {
		t.Fatalf("kết thúc: %v", err)
	}

	first, err := repo.GetByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("đọc lần một: %v", err)
	}
	if first.EndedAt == nil {
		t.Fatal("chưa đặt ended_at")
	}

	// Ghi lại cùng trạng thái: hàm chạy nhưng không được đẩy ended_at lên.
	if err := repo.UpdateStatus(ctx, c.ID,
		domaincall.StatusEnded, domaincall.StatusEnded, "khac"); err != nil {
		t.Fatalf("ghi lại: %v", err)
	}

	second, err := repo.GetByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("đọc lần hai: %v", err)
	}
	if !second.EndedAt.Equal(*first.EndedAt) {
		t.Errorf("ended_at đổi từ %v thành %v", first.EndedAt, second.EndedAt)
	}
}

// TestActiveClearsEndedAt: mở lại một cuộc gọi phải xoá mốc kết thúc, nếu
// không thì Duration() trả số âm.
func TestActiveClearsEndedAt(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	if err := repo.UpdateStatus(ctx, c.ID,
		domaincall.StatusRinging, domaincall.StatusActive, ""); err != nil {
		t.Fatalf("chuyển active: %v", err)
	}

	got, err := repo.GetByID(ctx, c.ID)
	if err != nil {
		t.Fatalf("đọc lại: %v", err)
	}
	if got.EndedAt != nil {
		t.Errorf("ended_at = %v, muốn nil khi cuộc gọi đang chạy", got.EndedAt)
	}
}

// =========================================================================
// TÌM CUỘC GỌI ĐANG CHẠY
// =========================================================================

// TestLiveForEmployeeIgnoresPeopleStillRinging.
//
// Người đang nghe chuông vẫn RẢNH: họ chưa nói chuyện với ai. Coi họ là bận
// thì lời mời thứ hai bị từ chối oan, và với người dùng thì đó là "gọi mãi
// không được".
func TestLiveForEmployeeIgnoresPeopleStillRinging(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	partRepo := NewCallParticipantRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	if err := partRepo.Invite(ctx, c.ID,
		[]uuid.UUID{f.alice, f.bob, f.carol}); err != nil {
		t.Fatalf("mời: %v", err)
	}
	// Chỉ alice vào phòng; bob và carol còn đang đổ chuông.
	if err := partRepo.Join(ctx, c.ID, f.alice, time.Now()); err != nil {
		t.Fatalf("alice vào: %v", err)
	}

	if _, err := repo.LiveForEmployee(ctx, f.alice); err != nil {
		t.Errorf("alice đang trong phòng, phải tìm thấy: %v", err)
	}

	_, err := repo.LiveForEmployee(ctx, f.bob)
	if !errors.Is(err, domaincall.ErrNotFound) {
		t.Errorf("bob đang nghe chuông nên vẫn rảnh, nhận: %v", err)
	}
}

// TestLiveForEmployeeIgnoresPeopleWhoLeft.
func TestLiveForEmployeeIgnoresPeopleWhoLeft(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	partRepo := NewCallParticipantRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	now := time.Now()
	if err := partRepo.Join(ctx, c.ID, f.bob, now); err != nil {
		t.Fatalf("bob vào: %v", err)
	}
	if err := partRepo.Leave(ctx, c.ID, f.bob, now.Add(time.Minute)); err != nil {
		t.Fatalf("bob rời: %v", err)
	}

	_, err := repo.LiveForEmployee(ctx, f.bob)
	if !errors.Is(err, domaincall.ErrNotFound) {
		t.Errorf("người đã rời vẫn bị coi là bận: %v", err)
	}
}

// =========================================================================
// NGƯỜI THAM GIA
// =========================================================================

// TestRejoinKeepsOriginalJoinTime.
//
// Rớt mạng rồi vào lại phải giữ mốc vào đầu tiên. Tính lại từ lần vào sau
// sẽ làm báo cáo nói rằng người đó chỉ họp có hai phút.
func TestRejoinKeepsOriginalJoinTime(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	partRepo := NewCallParticipantRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	first := time.Now().Truncate(time.Second)

	if err := partRepo.Join(ctx, c.ID, f.bob, first); err != nil {
		t.Fatalf("vào lần một: %v", err)
	}
	if err := partRepo.Leave(ctx, c.ID, f.bob, first.Add(time.Minute)); err != nil {
		t.Fatalf("rời: %v", err)
	}
	if err := partRepo.Join(ctx, c.ID, f.bob, first.Add(2*time.Minute)); err != nil {
		t.Fatalf("vào lại: %v", err)
	}

	rows, err := partRepo.ListForCall(ctx, c.ID)
	if err != nil {
		t.Fatalf("liệt kê: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("số dòng = %d, muốn 1 — vào lại không được tạo dòng mới", len(rows))
	}
	if rows[0].JoinedAt == nil || !rows[0].JoinedAt.Equal(first) {
		t.Errorf("mốc vào = %v, muốn giữ %v", rows[0].JoinedAt, first)
	}
	if rows[0].LeftAt != nil {
		t.Errorf("mốc rời = %v, muốn nil sau khi vào lại", rows[0].LeftAt)
	}
}

// TestLeaveDoesNotPushBackTimestamp: gọi Leave hai lần không được đẩy lùi
// mốc rời, nếu không thời lượng tham gia dài ra mỗi lần dọn dẹp chạy.
func TestLeaveDoesNotPushBackTimestamp(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	partRepo := NewCallParticipantRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	base := time.Now().Truncate(time.Second)

	if err := partRepo.Join(ctx, c.ID, f.bob, base); err != nil {
		t.Fatalf("vào: %v", err)
	}
	if err := partRepo.Leave(ctx, c.ID, f.bob, base.Add(time.Minute)); err != nil {
		t.Fatalf("rời lần một: %v", err)
	}
	if err := partRepo.Leave(ctx, c.ID, f.bob, base.Add(time.Hour)); err != nil {
		t.Fatalf("rời lần hai: %v", err)
	}

	rows, _ := partRepo.ListForCall(ctx, c.ID)
	if rows[0].LeftAt == nil || !rows[0].LeftAt.Equal(base.Add(time.Minute)) {
		t.Errorf("mốc rời = %v, muốn giữ lần đầu %v",
			rows[0].LeftAt, base.Add(time.Minute))
	}
}

// TestSetTracksOnlyTurnsOn: đây là dấu vết audit, không phải trạng thái
// hiện tại. Tắt camera giữa chừng không được xoá dấu vết đã từng bật.
func TestSetTracksOnlyTurnsOn(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	partRepo := NewCallParticipantRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	if err := partRepo.Join(ctx, c.ID, f.bob, time.Now()); err != nil {
		t.Fatalf("vào: %v", err)
	}

	if err := partRepo.SetTracks(ctx, c.ID, f.bob, true, true, true); err != nil {
		t.Fatalf("bật: %v", err)
	}
	// Tắt hết — cờ vẫn phải giữ nguyên.
	if err := partRepo.SetTracks(ctx, c.ID, f.bob, false, false, false); err != nil {
		t.Fatalf("tắt: %v", err)
	}

	rows, _ := partRepo.ListForCall(ctx, c.ID)
	p := rows[0]
	if !p.HadAudio || !p.HadVideo || !p.HadScreen {
		t.Errorf("cờ = %v/%v/%v, cả ba phải giữ true",
			p.HadAudio, p.HadVideo, p.HadScreen)
	}
}

// TestRoomStateCountsBothGroups khoá lại hai con số mà luật "cuộc gọi còn
// sống không" dựa vào.
func TestRoomStateCountsBothGroups(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	partRepo := NewCallParticipantRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	now := time.Now()

	if err := partRepo.Invite(ctx, c.ID,
		[]uuid.UUID{f.alice, f.bob, f.carol}); err != nil {
		t.Fatalf("mời: %v", err)
	}
	if err := partRepo.Join(ctx, c.ID, f.alice, now); err != nil {
		t.Fatalf("alice vào: %v", err)
	}
	if err := partRepo.Join(ctx, c.ID, f.bob, now); err != nil {
		t.Fatalf("bob vào: %v", err)
	}
	if err := partRepo.Leave(ctx, c.ID, f.bob, now.Add(time.Minute)); err != nil {
		t.Fatalf("bob rời: %v", err)
	}

	inRoom, pending, err := partRepo.RoomState(ctx, c.ID)
	if err != nil {
		t.Fatalf("RoomState: %v", err)
	}
	if inRoom != 1 {
		t.Errorf("trong phòng = %d, muốn 1 (alice)", inRoom)
	}
	if pending != 1 {
		t.Errorf("đang đổ chuông = %d, muốn 1 (carol)", pending)
	}
}

// TestInviteIsIdempotent: mời lại không tạo dòng trùng.
func TestInviteIsIdempotent(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	partRepo := NewCallParticipantRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	ids := []uuid.UUID{f.alice, f.bob}

	for i := 0; i < 3; i++ {
		if err := partRepo.Invite(ctx, c.ID, ids); err != nil {
			t.Fatalf("mời lần %d: %v", i+1, err)
		}
	}

	rows, _ := partRepo.ListForCall(ctx, c.ID)
	if len(rows) != 2 {
		t.Errorf("số dòng = %d, muốn 2", len(rows))
	}
}

// =========================================================================
// DỌN CUỘC GỌI QUÁ HẠN
// =========================================================================

func TestExpireRingingReturnsWhatItChanged(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)

	// Đẩy lùi started_at để cuộc gọi trở thành quá hạn.
	if _, err := testPool.Exec(ctx,
		`UPDATE calls SET started_at = NOW() - INTERVAL '5 minutes' WHERE id = $1`,
		c.ID); err != nil {
		t.Fatalf("đẩy lùi thời gian: %v", err)
	}

	got, err := repo.ExpireRinging(ctx, time.Now().Add(-domaincall.RingTimeout))
	if err != nil {
		t.Fatalf("ExpireRinging: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("số cuộc gọi hết hạn = %d, muốn 1", len(got))
	}
	if got[0].Status != domaincall.StatusMissed {
		t.Errorf("trạng thái = %q, muốn %q", got[0].Status, domaincall.StatusMissed)
	}
	if got[0].RoomName == "" {
		t.Error("thiếu room_name — không đóng được phòng trên SFU")
	}
}

// TestExpireRingingIsIdempotent: chạy lại không trả về cùng cuộc gọi lần
// nữa. Không có tính chất này thì hai worker chạy song song sẽ gửi hai lần
// thông báo gọi nhỡ cho cùng một cuộc.
func TestExpireRingingIsIdempotent(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	if _, err := testPool.Exec(ctx,
		`UPDATE calls SET started_at = NOW() - INTERVAL '5 minutes' WHERE id = $1`,
		c.ID); err != nil {
		t.Fatalf("đẩy lùi thời gian: %v", err)
	}

	cutoff := time.Now().Add(-domaincall.RingTimeout)
	if _, err := repo.ExpireRinging(ctx, cutoff); err != nil {
		t.Fatalf("lần một: %v", err)
	}

	got, err := repo.ExpireRinging(ctx, cutoff)
	if err != nil {
		t.Fatalf("lần hai: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("lần hai trả về %d cuộc gọi, muốn 0", len(got))
	}
}

// TestExpireRingingLeavesActiveAlone.
func TestExpireRingingLeavesActiveAlone(t *testing.T) {
	db := testDB(t)
	repo := NewCallRepository(db)
	f := seedCallFixture(t)
	ctx := context.Background()

	c := newCall(t, repo, f, domaincall.KindVideo)
	if err := repo.UpdateStatus(ctx, c.ID,
		domaincall.StatusRinging, domaincall.StatusActive, ""); err != nil {
		t.Fatalf("chuyển active: %v", err)
	}
	if _, err := testPool.Exec(ctx,
		`UPDATE calls SET started_at = NOW() - INTERVAL '2 hours' WHERE id = $1`,
		c.ID); err != nil {
		t.Fatalf("đẩy lùi thời gian: %v", err)
	}

	got, err := repo.ExpireRinging(ctx, time.Now().Add(-domaincall.RingTimeout))
	if err != nil {
		t.Fatalf("ExpireRinging: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("đã dọn %d cuộc gọi đang diễn ra — một cuộc họp dài hai tiếng "+
			"không phải cuộc gọi nhỡ", len(got))
	}
}
