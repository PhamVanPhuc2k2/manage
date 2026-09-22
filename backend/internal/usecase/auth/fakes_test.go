package auth

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainhr "github.com/PhamVanPhuc2k2/manage/internal/domain/hr"
	"github.com/PhamVanPhuc2k2/manage/pkg/hash"
	"github.com/PhamVanPhuc2k2/manage/pkg/jwt"
)

// Bộ giả lập cho module xác thực.
//
// fakeRefresh ở đây hiện thực ĐẦY ĐỦ ngữ nghĩa của refresh token: dùng một
// lần, phát hiện tái sử dụng, thời gian ân hạn, và đặt chỗ id phiên thay thế.
// Đó chính là phần đáng kiểm thử nhất của cả hệ thống — cơ chế chống đánh cắp
// token chỉ đúng khi toàn bộ những thứ đó khớp nhau.

type fakeUsers struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]*domainhr.User
	roles map[uuid.UUID][]string

	passwordUpdates map[uuid.UUID]string
	mustChange      map[uuid.UUID]bool
	failedResets    int
	failedIncrement int
}

func newFakeUsers() *fakeUsers {
	return &fakeUsers{
		byID:            map[uuid.UUID]*domainhr.User{},
		roles:           map[uuid.UUID][]string{},
		passwordUpdates: map[uuid.UUID]string{},
		mustChange:      map[uuid.UUID]bool{},
	}
}

func (f *fakeUsers) put(u *domainhr.User) { f.byID[u.ID] = u }

func (f *fakeUsers) Create(context.Context, *domainhr.User) error { return nil }

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

func (f *fakeUsers) FindByID(_ context.Context, id uuid.UUID) (*domainhr.User, error) {
	if u := f.byID[id]; u != nil {
		return u, nil
	}
	return nil, domainhr.ErrNotFound
}

func (f *fakeUsers) FindByEmployeeID(
	context.Context, uuid.UUID,
) (*domainhr.User, error) {
	return nil, domainhr.ErrNotFound
}

func (f *fakeUsers) UpdatePasswordHash(
	_ context.Context, id uuid.UUID, h string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.passwordUpdates[id] = h
	if u := f.byID[id]; u != nil {
		u.PasswordHash = h
	}
	return nil
}

func (f *fakeUsers) SetMustChangePassword(
	_ context.Context, id uuid.UUID, must bool,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mustChange[id] = must
	return nil
}

func (f *fakeUsers) UpdateLastLogin(context.Context, uuid.UUID) error { return nil }

func (f *fakeUsers) IncrementFailedAttempts(context.Context, uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failedIncrement++
	return nil
}

func (f *fakeUsers) ResetFailedAttempts(context.Context, uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failedResets++
	return nil
}

func (f *fakeUsers) SetActive(context.Context, uuid.UUID, bool) error { return nil }

func (f *fakeUsers) ListRoleCodes(
	_ context.Context, userID uuid.UUID,
) ([]string, error) {
	return f.roles[userID], nil
}

func (f *fakeUsers) AssignRole(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
func (f *fakeUsers) RemoveRole(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type fakeAuthReader struct{ authz *domainauth.Authorization }

func (f *fakeAuthReader) Load(
	context.Context, uuid.UUID, uuid.UUID,
) (*domainauth.Authorization, error) {
	if f.authz != nil {
		return f.authz, nil
	}
	return &domainauth.Authorization{Scope: domainauth.ScopeSelf}, nil
}

type fakeSessions struct {
	mu   sync.Mutex
	byID map[uuid.UUID]*domainauth.Session

	deletedAll []uuid.UUID
	deleted    []uuid.UUID
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{byID: map[uuid.UUID]*domainauth.Session{}}
}

func (f *fakeSessions) Create(
	_ context.Context, s domainauth.Session, _ time.Duration,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := s
	f.byID[s.ID] = &copied
	return nil
}

func (f *fakeSessions) Get(
	_ context.Context, id uuid.UUID,
) (*domainauth.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s := f.byID[id]; s != nil {
		return s, nil
	}
	return nil, domainauth.ErrNoSession
}

func (f *fakeSessions) Touch(context.Context, uuid.UUID) error { return nil }

func (f *fakeSessions) Delete(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byID, id)
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeSessions) DeleteAllOfUser(_ context.Context, userID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, s := range f.byID {
		if s.UserID == userID {
			delete(f.byID, id)
		}
	}
	f.deletedAll = append(f.deletedAll, userID)
	return nil
}

func (f *fakeSessions) ListOfUser(
	_ context.Context, userID uuid.UUID,
) ([]domainauth.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domainauth.Session
	for _, s := range f.byID {
		if s.UserID == userID {
			out = append(out, *s)
		}
	}
	return out, nil
}

// refreshRecord là một bản ghi refresh token trong bộ nhớ.
type refreshRecord struct {
	sessionID     uuid.UUID
	userID        uuid.UUID
	consumedAt    time.Time
	nextSessionID uuid.UUID
}

// fakeRefresh hiện thực đầy đủ ngữ nghĩa dùng-một-lần của refresh token.
//
// Đây là bản giả lập quan trọng nhất trong tệp này. Nó phải phân biệt được ba
// trường hợp mà cơ chế chống đánh cắp dựa vào:
//
//	chưa tiêu            → thành công
//	tiêu lại TRONG ân hạn → ErrTokenReusedInGrace (nhiều tab, gửi lại do mạng)
//	tiêu lại NGOÀI ân hạn → ErrTokenReused        (bị đánh cắp)
//
// Gộp hai trường hợp sau lại thì hoặc là mỗi lần mở hai tab đều bị đá ra
// ngoài, hoặc là token bị đánh cắp dùng được mãi.
type fakeRefresh struct {
	mu      sync.Mutex
	records map[string]*refreshRecord
	grace   time.Duration
	now     func() time.Time
}

func newFakeRefresh() *fakeRefresh {
	return &fakeRefresh{
		records: map[string]*refreshRecord{},
		grace:   10 * time.Second,
		now:     time.Now,
	}
}

func (f *fakeRefresh) Save(
	_ context.Context, tokenHash string, sessionID, userID uuid.UUID, _ time.Duration,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[tokenHash] = &refreshRecord{sessionID: sessionID, userID: userID}
	return nil
}

func (f *fakeRefresh) Consume(
	_ context.Context, tokenHash string, reserveSessionID uuid.UUID,
) (domainauth.RefreshConsumed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	rec := f.records[tokenHash]
	if rec == nil {
		return domainauth.RefreshConsumed{}, domainauth.ErrTokenInvalid
	}

	out := domainauth.RefreshConsumed{
		SessionID:     rec.sessionID,
		UserID:        rec.userID,
		NextSessionID: rec.nextSessionID,
	}

	if rec.consumedAt.IsZero() {
		// Lần tiêu đầu tiên: đánh dấu và ĐẶT CHỖ id phiên thay thế trong cùng
		// một thao tác — đúng như lệnh Lua trong bản thật.
		rec.consumedAt = f.now()
		rec.nextSessionID = reserveSessionID
		return out, nil
	}

	if f.now().Sub(rec.consumedAt) <= f.grace {
		return out, domainauth.ErrTokenReusedInGrace
	}
	return out, domainauth.ErrTokenReused
}

type fakeThrottle struct {
	wait      time.Duration
	failures  int
	resets    int
	checkedAt []string
}

func (f *fakeThrottle) Check(
	_ context.Context, email, _ string,
) (time.Duration, error) {
	f.checkedAt = append(f.checkedAt, email)
	return f.wait, nil
}

func (f *fakeThrottle) RecordFailure(context.Context, string, string) error {
	f.failures++
	return nil
}

func (f *fakeThrottle) Reset(context.Context, string, string) error {
	f.resets++
	return nil
}

type fakeReset struct {
	saved    map[string]uuid.UUID
	consumed map[string]bool
}

func newFakeReset() *fakeReset {
	return &fakeReset{saved: map[string]uuid.UUID{}, consumed: map[string]bool{}}
}

func (f *fakeReset) Save(
	_ context.Context, tokenHash string, userID uuid.UUID, _ time.Duration,
) error {
	f.saved[tokenHash] = userID
	return nil
}

func (f *fakeReset) Consume(
	_ context.Context, tokenHash string,
) (uuid.UUID, error) {
	id, ok := f.saved[tokenHash]
	if !ok || f.consumed[tokenHash] {
		return uuid.Nil, domainauth.ErrTokenInvalid
	}
	f.consumed[tokenHash] = true
	return id, nil
}

type fakeOTP struct {
	mu         sync.Mutex
	challenges map[string]*domainauth.OTPChallenge
	codeHashes map[string]string
	attempts   map[string]int
	resends    int
}

func newFakeOTP() *fakeOTP {
	return &fakeOTP{
		challenges: map[string]*domainauth.OTPChallenge{},
		codeHashes: map[string]string{},
		attempts:   map[string]int{},
	}
}

func (f *fakeOTP) Create(
	_ context.Context, challengeHash string,
	c domainauth.OTPChallenge, codeHash string, _ time.Duration,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copied := c
	f.challenges[challengeHash] = &copied
	f.codeHashes[challengeHash] = codeHash
	return nil
}

func (f *fakeOTP) Verify(
	_ context.Context, challengeHash, codeHash string, maxAttempts int,
) (domainauth.OTPVerified, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c := f.challenges[challengeHash]
	if c == nil {
		return domainauth.OTPVerified{}, 0, domainauth.ErrOTPNotFound
	}

	if f.codeHashes[challengeHash] == codeHash {
		delete(f.challenges, challengeHash)
		return domainauth.OTPVerified{Challenge: *c}, 0, nil
	}

	f.attempts[challengeHash]++
	left := maxAttempts - f.attempts[challengeHash]
	if left <= 0 {
		// Hết lượt: XOÁ thử thách. Giữ lại thì kẻ tấn công cứ thử tiếp mãi,
		// và bộ đếm 5 lần thành vô nghĩa.
		delete(f.challenges, challengeHash)
		return domainauth.OTPVerified{}, 0, domainauth.ErrOTPTooManyAttempts
	}
	return domainauth.OTPVerified{}, left, domainauth.ErrOTPWrongCode
}

func (f *fakeOTP) Resend(
	_ context.Context, challengeHash, newCodeHash string,
	_ time.Duration, maxResends int, _ time.Time,
) (domainauth.OTPChallenge, time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	c := f.challenges[challengeHash]
	if c == nil {
		return domainauth.OTPChallenge{}, 0, domainauth.ErrOTPNotFound
	}
	if maxResends > 0 && f.resends >= maxResends {
		return domainauth.OTPChallenge{}, 0, domainauth.ErrOTPTooManyResends
	}

	// Thay mã, GIỮ bộ đếm số lần thử. Đặt lại bộ đếm ở đây sẽ khiến kẻ tấn
	// công cứ bấm gửi lại là có thêm 5 lượt đoán.
	f.codeHashes[challengeHash] = newCodeHash
	f.resends++
	return *c, 0, nil
}

func (f *fakeOTP) Delete(_ context.Context, challengeHash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.challenges, challengeHash)
	delete(f.codeHashes, challengeHash)
	return nil
}

type fakeMailer struct {
	mu         sync.Mutex
	otpCodes   []string
	resetURLs  []string
	suspicious []uuid.UUID
	err        error
}

func (f *fakeMailer) SendPasswordReset(
	_ context.Context, _, _, resetURL string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.resetURLs = append(f.resetURLs, resetURL)
	return nil
}

func (f *fakeMailer) SendSuspiciousActivity(
	context.Context, string, string, string, string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.suspicious = append(f.suspicious, uuid.Nil)
	return nil
}

func (f *fakeMailer) SendLoginOTP(
	_ context.Context, _, _, code string, _ int, _ string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.otpCodes = append(f.otpCodes, code)
	return nil
}

const authTestSecret = "khoa-bi-mat-cho-test-xac-thuc-dai-hon-32-ky-tu"

// authHarness gom usecase và các bản giả lập.
type authHarness struct {
	uc *Usecase

	users    *fakeUsers
	sessions *fakeSessions
	refresh  *fakeRefresh
	throttle *fakeThrottle
	reset    *fakeReset
	otp      *fakeOTP
	mailer   *fakeMailer
	jwt      *jwt.Manager
}

func newAuthHarness(t interface{ Fatalf(string, ...any) }, otpEnabled bool) *authHarness {
	jwtMgr, err := jwt.NewManager(authTestSecret, 15*time.Minute, "manage-test")
	if err != nil {
		t.Fatalf("dựng jwt.Manager lỗi: %v", err)
	}

	h := &authHarness{
		users:    newFakeUsers(),
		sessions: newFakeSessions(),
		refresh:  newFakeRefresh(),
		throttle: &fakeThrottle{},
		reset:    newFakeReset(),
		otp:      newFakeOTP(),
		mailer:   &fakeMailer{},
		jwt:      jwtMgr,
	}

	h.uc = NewUsecase(
		h.users, &fakeAuthReader{}, h.sessions, h.refresh,
		h.throttle, h.reset, h.otp, jwtMgr, h.mailer,
		Config{
			AccessTTL:     15 * time.Minute,
			RefreshTTL:    7 * 24 * time.Hour,
			PublicBaseURL: "https://manage.test",
			OTPEnabled:    otpEnabled,
		},
	)
	return h
}

// seedUser dựng một tài khoản đăng nhập được, mật khẩu đã băm thật.
func (h *authHarness) seedUser(
	t interface{ Fatalf(string, ...any) },
	email, password string,
) *domainhr.User {
	hashed, err := hash.Password(password)
	if err != nil {
		t.Fatalf("băm mật khẩu lỗi: %v", err)
	}

	u := &domainhr.User{
		ID:           uuid.New(),
		EmployeeID:   uuid.New(),
		Email:        email,
		PasswordHash: hashed,
		IsActive:     true,
	}
	h.users.put(u)
	return u
}
