package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	domainauth "github.com/PhamVanPhuc2k2/manage/internal/domain/auth"
	domainchat "github.com/PhamVanPhuc2k2/manage/internal/domain/chat"
	"github.com/PhamVanPhuc2k2/manage/pkg/apperror"
	"github.com/PhamVanPhuc2k2/manage/pkg/logger"
)

// previewLength là độ dài đoạn trích trong thông báo tin nhắn mới.
const previewLength = 120

// SendInput là dữ liệu gửi một tin nhắn.
type SendInput struct {
	ConversationID uuid.UUID
	Content        string
	Kind           domainchat.MessageKind
	ReplyToID      *uuid.UUID

	// ClientMessageID do client sinh, dùng để chống trùng khi gửi lại.
	ClientMessageID string

	Attachments []AttachmentInput
}

// AttachmentInput mô tả một tệp đã tải lên R2, chờ gắn vào tin nhắn.
type AttachmentInput struct {
	StorageKey  string
	FileName    string
	ContentType string
	SizeBytes   int64
	Width       *int
	Height      *int
}

// Send ghi tin nhắn rồi phát cho mọi thành viên.
//
// Thứ tự GHI TRƯỚC, PHÁT SAU là bắt buộc: phát trước rồi ghi hỏng sẽ để lại
// một tin nhắn hiện trên màn hình mọi người mà tải lại là mất — lỗi này người
// dùng không bao giờ tin tưởng lại được.
func (u *Usecase) Send(
	ctx context.Context,
	actor *domainauth.Actor,
	in SendInput,
) (*MessageView, error) {
	if _, err := u.requireMember(ctx, in.ConversationID, actor.EmployeeID); err != nil {
		return nil, err
	}

	kind := in.Kind
	if kind == "" {
		kind = domainchat.MessageText
	}
	// Tin hệ thống chỉ do server sinh. Cho client gửi loại này là để nó giả
	// được những dòng "X đã rời nhóm" mà không ai phân biệt nổi.
	if kind == domainchat.MessageSystem || !kind.Valid() {
		return nil, apperror.Invalid("loại tin nhắn không hợp lệ", nil)
	}

	content := strings.TrimSpace(in.Content)
	if content == "" && len(in.Attachments) == 0 {
		return nil, apperror.Invalid("tin nhắn không được để trống", nil)
	}
	if len([]rune(content)) > domainchat.MaxMessageLength {
		return nil, apperror.Invalid(
			fmt.Sprintf("tin nhắn tối đa %d ký tự", domainchat.MaxMessageLength), nil)
	}
	if len(in.Attachments) > maxAttachmentsPerMessage {
		return nil, apperror.Invalid(
			fmt.Sprintf("mỗi tin nhắn tối đa %d tệp", maxAttachmentsPerMessage), nil)
	}

	// Chống trùng TRƯỚC khi ghi: client mất mạng giữa chừng không biết tin đã
	// tới hay chưa nên gửi lại, và trả về đúng tin cũ là cách duy nhất để
	// người nhận không thấy tin đôi.
	if in.ClientMessageID != "" {
		old, err := u.msgs.ByClientID(ctx, in.ConversationID, actor.EmployeeID, in.ClientMessageID)
		if err != nil {
			return nil, err
		}
		if old != nil {
			return u.hydrate(ctx, old)
		}
	}

	if in.ReplyToID != nil {
		reply, err := u.msgs.ByID(ctx, *in.ReplyToID)
		if err != nil || reply.ConversationID != in.ConversationID {
			return nil, apperror.Invalid("tin nhắn được trả lời không thuộc hội thoại này", nil)
		}
	}

	m := &domainchat.Message{
		ConversationID:  in.ConversationID,
		SenderID:        &actor.EmployeeID,
		Kind:            kind,
		Content:         content,
		ReplyToID:       in.ReplyToID,
		ClientMessageID: in.ClientMessageID,
	}
	if err := u.msgs.Create(ctx, m); err != nil {
		return nil, err
	}

	if err := u.saveAttachments(ctx, m, in.Attachments); err != nil {
		return nil, err
	}

	// Đọc lại tin vừa ghi để lấy các trường đến từ JOIN: tên người gửi và
	// trích dẫn tin được trả lời.
	//
	// Một truy vấn thêm cho mỗi tin, nhưng bản ghi vừa INSERT không có chúng,
	// và bản tin phát đi cho mọi thành viên cũng dùng chính dữ liệu này —
	// thiếu thì khung chat của người nhận hiện một trích dẫn rỗng.
	full, err := u.msgs.ByID(ctx, m.ID)
	if err != nil {
		return nil, err
	}

	view, err := u.hydrate(ctx, full)
	if err != nil {
		return nil, err
	}

	u.fanout(ctx, in.ConversationID, actor.EmployeeID, view)
	return view, nil
}

func (u *Usecase) saveAttachments(
	ctx context.Context,
	m *domainchat.Message,
	items []AttachmentInput,
) error {
	if len(items) == 0 {
		return nil
	}

	atts := make([]*domainchat.Attachment, 0, len(items))
	for _, it := range items {
		if it.StorageKey == "" || it.FileName == "" {
			return apperror.Invalid("tệp đính kèm thiếu thông tin", nil)
		}
		if it.SizeBytes <= 0 || it.SizeBytes > maxAttachmentSize {
			return apperror.Invalid("kích thước tệp không hợp lệ", nil)
		}
		atts = append(atts, &domainchat.Attachment{
			MessageID:   m.ID,
			StorageKey:  it.StorageKey,
			FileName:    it.FileName,
			ContentType: it.ContentType,
			SizeBytes:   it.SizeBytes,
			Width:       it.Width,
			Height:      it.Height,
		})
	}
	return u.msgs.AddAttachments(ctx, atts)
}

// fanout phát tin nhắn cho thành viên và tạo thông báo cho người offline.
//
// Người ĐANG online không nhận thông báo: tin nhắn đã hiện trên màn hình họ,
// và một thông báo kèm theo chỉ là tiếng chuông thừa cho thứ họ vừa đọc.
func (u *Usecase) fanout(
	ctx context.Context,
	conversationID, senderID uuid.UUID,
	m *MessageView,
) {
	log := logger.FromContext(ctx)

	members, err := u.convs.MemberIDs(ctx, conversationID)
	if err != nil {
		log.Warn().Err(err).Msg("không lấy được danh sách người nhận tin nhắn")
		return
	}

	if u.pusher != nil {
		u.pusher.PushChat(ctx, members, domainchat.EventMessageNew, m)
	}

	if u.notifier == nil {
		return
	}

	// Người tắt thông báo hội thoại này cũng bị loại ở đây. Lọc sau khi đã
	// đẩy realtime: tắt thông báo nghĩa là không muốn bị đánh động, không
	// phải là không muốn thấy tin nhắn.
	muted, err := u.mutedMembers(ctx, conversationID)
	if err != nil {
		log.Warn().Err(err).Msg("không đọc được cấu hình tắt hội thoại")
	}

	status := u.statusOf(ctx, members)

	offline := make([]uuid.UUID, 0, len(members))
	for _, id := range members {
		if id == senderID || muted[id] {
			continue
		}
		if s := status[id]; s == "" || s == "offline" {
			offline = append(offline, id)
		}
	}
	if len(offline) == 0 {
		return
	}

	c, err := u.convs.ByID(ctx, conversationID, senderID)
	if err != nil {
		log.Warn().Err(err).Msg("không đọc được hội thoại để tạo thông báo")
		return
	}

	name := ""
	if c.Kind != domainchat.KindDirect {
		name = c.Name
	}

	link := "/chat/" + conversationID.String()
	if err := u.notifier.NotifyNewMessage(ctx, offline, senderID, m.SenderName,
		name, preview(m), link); err != nil {
		log.Warn().Err(err).Msg("không tạo được thông báo tin nhắn mới")
	}
}

// preview dựng đoạn trích hiện trong thông báo.
func preview(m *MessageView) string {
	switch {
	case m.Kind == domainchat.MessageImage:
		return "[Hình ảnh]"
	case m.Kind == domainchat.MessageFile:
		return "[Tệp đính kèm]"
	}

	r := []rune(m.Content)
	if len(r) <= previewLength {
		return m.Content
	}
	return string(r[:previewLength]) + "…"
}

func (u *Usecase) mutedMembers(
	ctx context.Context,
	conversationID uuid.UUID,
) (map[uuid.UUID]bool, error) {
	members, err := u.convs.Members(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]bool, len(members))
	for _, m := range members {
		if m.IsMuted {
			out[m.EmployeeID] = true
		}
	}
	return out, nil
}

// History đọc lịch sử một hội thoại, cuộn ngược bằng cursor.
func (u *Usecase) History(
	ctx context.Context,
	actor *domainauth.Actor,
	conversationID uuid.UUID,
	before *time.Time,
	search string,
	limit int,
) ([]*MessageView, error) {
	if _, err := u.requireMember(ctx, conversationID, actor.EmployeeID); err != nil {
		return nil, err
	}

	items, err := u.msgs.List(ctx, domainchat.MessageFilter{
		ConversationID: conversationID,
		Before:         before,
		Search:         search,
		Limit:          limit,
	})
	if err != nil {
		return nil, err
	}

	hydrated, err := u.hydrateMany(ctx, items)
	if err != nil {
		return nil, err
	}
	return toMessageViews(hydrated), nil
}

// hydrateMany nạp tệp đính kèm và ký URL cho cả trang trong MỘT lượt.
func (u *Usecase) hydrateMany(
	ctx context.Context,
	items []*domainchat.Message,
) ([]*domainchat.Message, error) {
	if len(items) == 0 {
		return items, nil
	}

	ids := make([]uuid.UUID, 0, len(items))
	for _, m := range items {
		ids = append(ids, m.ID)
	}

	byMsg, err := u.msgs.AttachmentsOf(ctx, ids)
	if err != nil {
		return nil, err
	}

	for _, m := range items {
		// Tin đã thu hồi thì bỏ luôn cả nội dung lẫn tệp: giữ lại tệp của một
		// tin đã thu hồi là thu hồi trên danh nghĩa.
		if m.Deleted() {
			m.Content = ""
			continue
		}
		m.Attachments = u.signAll(ctx, byMsg[m.ID])
	}
	return items, nil
}

func (u *Usecase) hydrate(
	ctx context.Context,
	m *domainchat.Message,
) (*MessageView, error) {
	out, err := u.hydrateMany(ctx, []*domainchat.Message{m})
	if err != nil {
		return nil, err
	}
	return toMessageView(out[0]), nil
}

// signAll ký URL tải cho từng tệp. Lỗi ký một tệp không làm hỏng cả tin nhắn.
func (u *Usecase) signAll(
	ctx context.Context,
	items []*domainchat.Attachment,
) []*domainchat.Attachment {
	if u.storage == nil {
		return items
	}
	log := logger.FromContext(ctx)

	for _, a := range items {
		url, err := u.storage.PresignGet(ctx, a.StorageKey, viewURLTTL)
		if err != nil {
			log.Warn().Err(err).Str("key", a.StorageKey).Msg("không ký được URL tệp đính kèm")
			continue
		}
		a.URL = url
	}
	return items
}

// Edit sửa nội dung tin nhắn. Chỉ người gửi sửa được.
func (u *Usecase) Edit(
	ctx context.Context,
	actor *domainauth.Actor,
	messageID uuid.UUID,
	content string,
) (*MessageView, error) {
	m, err := u.ownMessage(ctx, actor, messageID)
	if err != nil {
		return nil, err
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil, apperror.Invalid("nội dung không được để trống", nil)
	}
	if len([]rune(content)) > domainchat.MaxMessageLength {
		return nil, apperror.Invalid(
			fmt.Sprintf("tin nhắn tối đa %d ký tự", domainchat.MaxMessageLength), nil)
	}

	if err := u.msgs.Edit(ctx, messageID, content); err != nil {
		return nil, notFoundOr(err)
	}

	updated, err := u.msgs.ByID(ctx, messageID)
	if err != nil {
		return nil, err
	}
	view, err := u.hydrate(ctx, updated)
	if err != nil {
		return nil, err
	}

	u.broadcast(ctx, m.ConversationID, domainchat.EventMessageEdited, view)
	return view, nil
}

// Delete thu hồi tin nhắn.
//
// Ngoài người gửi, quản trị nhóm cũng thu hồi được: nội dung không phù hợp
// trong một nhóm phải có người gỡ được, và chờ chính người gửi tự gỡ thì
// không phải lúc nào cũng xảy ra.
func (u *Usecase) Delete(
	ctx context.Context,
	actor *domainauth.Actor,
	messageID uuid.UUID,
) error {
	m, err := u.msgs.ByID(ctx, messageID)
	if err != nil {
		return apperror.NotFound("tin nhắn")
	}

	member, err := u.requireMember(ctx, m.ConversationID, actor.EmployeeID)
	if err != nil {
		return err
	}

	own := m.SenderID != nil && *m.SenderID == actor.EmployeeID
	if !own && !member.IsAdmin {
		return apperror.Forbidden("chỉ người gửi hoặc quản trị nhóm mới thu hồi được tin nhắn")
	}

	if err := u.msgs.SoftDelete(ctx, messageID); err != nil {
		return notFoundOr(err)
	}

	u.broadcast(ctx, m.ConversationID, domainchat.EventMessageDeleted, DeletedPayload{
		ID:             messageID,
		ConversationID: m.ConversationID,
	})
	return nil
}

func (u *Usecase) ownMessage(
	ctx context.Context,
	actor *domainauth.Actor,
	messageID uuid.UUID,
) (*domainchat.Message, error) {
	m, err := u.msgs.ByID(ctx, messageID)
	if err != nil {
		return nil, apperror.NotFound("tin nhắn")
	}
	if _, err := u.requireMember(ctx, m.ConversationID, actor.EmployeeID); err != nil {
		return nil, err
	}
	if m.SenderID == nil || *m.SenderID != actor.EmployeeID {
		return nil, apperror.Forbidden("chỉ người gửi mới sửa được tin nhắn này")
	}
	if m.Deleted() {
		return nil, apperror.Invalid("tin nhắn đã thu hồi", nil)
	}
	return m, nil
}

// MarkRead dời mốc đã đọc và báo cho những người còn lại.
//
// Báo cho cả CHÍNH người đọc: họ có thể đang mở nhiều tab, và tab kia cần biết
// để hạ số chưa đọc xuống mà không phải hỏi lại server.
func (u *Usecase) MarkRead(
	ctx context.Context,
	actor *domainauth.Actor,
	conversationID, messageID uuid.UUID,
) (int, error) {
	if _, err := u.requireMember(ctx, conversationID, actor.EmployeeID); err != nil {
		return 0, err
	}

	if err := u.convs.MarkRead(ctx, conversationID, actor.EmployeeID, messageID); err != nil {
		return 0, err
	}

	u.broadcast(ctx, conversationID, domainchat.EventRead, domainchat.ReadPayload{
		ConversationID: conversationID,
		EmployeeID:     actor.EmployeeID,
		MessageID:      messageID,
	})

	return u.convs.TotalUnread(ctx, actor.EmployeeID)
}

// Typing phát chỉ báo "đang nhập". KHÔNG lưu database.
//
// Nó hết giá trị sau vài giây, và ghi mỗi lần gõ phím sẽ tạo ra lượng ghi lớn
// hơn cả tin nhắn thật. Client tự tiết chế tần suất gửi.
func (u *Usecase) Typing(
	ctx context.Context,
	actor *domainauth.Actor,
	conversationID uuid.UUID,
) error {
	if _, err := u.requireMember(ctx, conversationID, actor.EmployeeID); err != nil {
		return err
	}

	names, _ := u.employee.NamesOf(ctx, []uuid.UUID{actor.EmployeeID})

	members, err := u.convs.MemberIDs(ctx, conversationID)
	if err != nil {
		return err
	}

	// Không gửi lại cho chính người đang gõ: họ biết mình đang gõ.
	others := make([]uuid.UUID, 0, len(members))
	for _, id := range members {
		if id != actor.EmployeeID {
			others = append(others, id)
		}
	}
	if len(others) == 0 || u.pusher == nil {
		return nil
	}

	u.pusher.PushChat(ctx, others, domainchat.EventTyping, domainchat.TypingPayload{
		ConversationID: conversationID,
		EmployeeID:     actor.EmployeeID,
		EmployeeName:   names[actor.EmployeeID],
	})
	return nil
}

// TotalUnread là tổng tin chưa đọc, cho huy hiệu trên biểu tượng chat.
func (u *Usecase) TotalUnread(ctx context.Context, actor *domainauth.Actor) (int, error) {
	return u.convs.TotalUnread(ctx, actor.EmployeeID)
}

// broadcast gửi một sự kiện tới mọi thành viên hội thoại.
func (u *Usecase) broadcast(
	ctx context.Context,
	conversationID uuid.UUID,
	eventType string,
	payload any,
) {
	if u.pusher == nil {
		return
	}
	members, err := u.convs.MemberIDs(ctx, conversationID)
	if err != nil {
		log := logger.FromContext(ctx)
		log.Warn().Err(err).Msg("không lấy được người nhận sự kiện chat")
		return
	}
	u.pusher.PushChat(ctx, members, eventType, payload)
}

// announce ghi một tin nhắn HỆ THỐNG vào hội thoại.
//
// Lỗi ở đây chỉ ghi log chứ không trả lên trên: thao tác chính (tạo nhóm, thêm
// người) đã thành công rồi, và làm hỏng nó vì một dòng thông báo phụ là đánh
// đổi sai.
func (u *Usecase) announce(
	ctx context.Context,
	conversationID, actorID uuid.UUID,
	content string,
) {
	m := &domainchat.Message{
		ConversationID: conversationID,
		Kind:           domainchat.MessageSystem,
		Content:        content,
	}
	if err := u.msgs.Create(ctx, m); err != nil {
		log := logger.FromContext(ctx)
		log.Warn().Err(err).Msg("không ghi được tin nhắn hệ thống")
		return
	}
	_ = actorID
	u.broadcast(ctx, conversationID, domainchat.EventMessageNew, toMessageView(m))
}

// PresignUpload cấp URL để client tải tệp thẳng lên R2.
//
// Tệp KHÔNG đi qua api: đẩy vài chục megabyte qua tiến trình Go chỉ để chuyển
// tiếp sang R2 là lãng phí băng thông và bộ nhớ của chính máy chủ đang phục vụ
// mọi request khác.
func (u *Usecase) PresignUpload(
	ctx context.Context,
	actor *domainauth.Actor,
	conversationID uuid.UUID,
	fileName, contentType string,
	size int64,
) (key, url string, err error) {
	if _, err := u.requireMember(ctx, conversationID, actor.EmployeeID); err != nil {
		return "", "", err
	}
	if u.storage == nil {
		return "", "", apperror.Internal(errors.New("chưa cấu hình lưu trữ tệp"))
	}
	if fileName == "" {
		return "", "", apperror.Invalid("thiếu tên tệp", nil)
	}
	if size <= 0 || size > maxAttachmentSize {
		return "", "", apperror.Invalid(
			fmt.Sprintf("kích thước tệp tối đa %d MB", maxAttachmentSize>>20), nil)
	}

	// Khoá gồm id hội thoại và một uuid mới: hai người gửi cùng tên tệp trong
	// cùng một hội thoại không được ghi đè lên nhau.
	key = fmt.Sprintf("chat/%s/%s/%s", conversationID, uuid.New(), sanitizeName(fileName))

	url, err = u.storage.PresignPut(ctx, key, contentType, uploadURLTTL)
	if err != nil {
		return "", "", apperror.Internal(err)
	}
	return key, url, nil
}

// sanitizeName bỏ ký tự có thể phá cấu trúc khoá object.
//
// Bỏ cả dấu gạch chéo lẫn đoạn "..": gạch chéo là thứ thật sự nguy hiểm (nó
// tạo ra tiền tố thư mục ngoài ý muốn trên R2), còn ".." thì với khoá object
// chỉ là ký tự thường — nhưng khoá này còn đi qua CDN và tên tệp lúc tải về,
// nơi không phải công cụ nào cũng coi nó là vô nghĩa. Bỏ đi thì hết phải suy
// đoán xem chỗ nào chuẩn hoá đường dẫn, chỗ nào không.
//
// Cắt độ dài theo BYTE là có ý: giới hạn của khoá object tính bằng byte, và
// đây là tên tệp dùng làm hậu tố chứ không phải văn bản hiển thị. Đổi lại,
// tên tiếng Việt dài có thể bị cắt giữa một ký tự — chấp nhận được, vì tên
// thật của tệp vẫn lưu nguyên trong cột file_name.
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	name = strings.ReplaceAll(name, "..", "_")
	name = strings.TrimSpace(name)
	if name == "" {
		return "file"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}
