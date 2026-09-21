-- =========================================================================
-- PHASE 5 — THÔNG BÁO & CHAT
-- =========================================================================

-- =========================================================================
-- THÔNG BÁO
-- =========================================================================
CREATE TYPE notification_type AS ENUM (
    'task_assigned',
    'task_status_changed',
    'task_mentioned',
    'task_due_soon',
    'leave_requested',
    'leave_decided',
    'adjustment_requested',
    'adjustment_decided',
    'payslip_ready',
    'new_message',
    'system'
);

CREATE TABLE notifications (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    -- Người NHẬN. Khoá theo nhân viên vì mọi module nghiệp vụ đều làm việc
    -- với nhân viên, không phải với tài khoản đăng nhập.
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,

    type  notification_type NOT NULL,
    title VARCHAR(255) NOT NULL,
    body  TEXT,

    -- Đường dẫn để bấm vào thông báo là tới thẳng chỗ cần xem.
    -- Lưu đường dẫn tương đối, không lưu URL đầy đủ: đổi tên miền không
    -- được làm hỏng toàn bộ thông báo cũ.
    link  VARCHAR(500),

    -- Ai gây ra thông báo này. NULL khi do hệ thống sinh.
    actor_id UUID REFERENCES employees(id) ON DELETE SET NULL,

    -- Đối tượng liên quan, để gộp và để dọn khi đối tượng bị xoá.
    resource    VARCHAR(50),
    resource_id UUID,

    read_at   TIMESTAMPTZ,
    -- Đã gửi email nhắc chưa. Không có cờ này thì job nhắc chạy mỗi 5 phút
    -- sẽ gửi lại cùng một thông báo mãi mãi.
    emailed_at TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Chỉ mục phục vụ truy vấn hay gặp nhất: "thông báo của tôi, mới nhất trước".
CREATE INDEX idx_notifications_employee
  ON notifications (employee_id, created_at DESC);

-- Chỉ mục RIÊNG cho việc đếm số chưa đọc.
--
-- Chuông thông báo hỏi con số này ở mọi trang, và chỉ mục một phần (chỉ
-- chứa dòng chưa đọc) nhỏ hơn nhiều lần chỉ mục đầy đủ — thông báo đã đọc
-- chiếm phần lớn bảng sau vài tháng.
CREATE INDEX idx_notifications_unread
  ON notifications (employee_id) WHERE read_at IS NULL;

-- Chỉ mục cho job gửi email nhắc.
CREATE INDEX idx_notifications_pending_email
  ON notifications (created_at) WHERE read_at IS NULL AND emailed_at IS NULL;

-- =========================================================================
-- CẤU HÌNH NHẬN THÔNG BÁO
--
-- Chỉ lưu những loại người dùng đã TẮT. Mặc định là bật tất cả, nên bảng
-- này rỗng với phần lớn người dùng — rẻ hơn nhiều so với việc tạo sẵn một
-- dòng cho mỗi người nhân mỗi loại.
-- =========================================================================
CREATE TABLE notification_mutes (
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    type        notification_type NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (employee_id, type)
);

-- =========================================================================
-- HỘI THOẠI
-- =========================================================================
CREATE TYPE conversation_kind AS ENUM ('direct', 'group', 'department', 'project');

CREATE TABLE conversations (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,

    kind  conversation_kind NOT NULL,
    -- NULL với hội thoại 1-1: tên hiển thị là tên người đối diện, và người
    -- đối diện khác nhau tuỳ ai đang xem.
    name  VARCHAR(255),

    -- Nguồn gốc với nhóm tự động theo phòng ban hoặc dự án.
    department_id UUID REFERENCES departments(id) ON DELETE SET NULL,
    project_id    UUID REFERENCES projects(id)    ON DELETE SET NULL,

    -- direct_key là khoá duy nhất của một cặp hội thoại 1-1.
    --
    -- Ghép hai id đã SẮP XẾP thành một chuỗi, để (A,B) và (B,A) cho cùng
    -- một khoá. Không có nó thì hai người cùng bấm "nhắn tin" một lúc sẽ
    -- tạo ra hai hội thoại song song, và tin nhắn của họ đi vào hai chỗ
    -- khác nhau mà không ai hiểu vì sao.
    direct_key VARCHAR(80),

    created_by UUID REFERENCES employees(id) ON DELETE SET NULL,
    -- Cập nhật mỗi khi có tin nhắn mới, để sắp danh sách hội thoại theo
    -- hoạt động gần nhất mà không phải JOIN sang bảng messages.
    last_message_at TIMESTAMPTZ,

    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_conversations_direct
        CHECK (kind <> 'direct' OR direct_key IS NOT NULL),
    CONSTRAINT chk_conversations_group_name
        CHECK (kind = 'direct' OR name IS NOT NULL)
);

CREATE UNIQUE INDEX idx_conversations_direct
  ON conversations (direct_key) WHERE direct_key IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_conversations_recent
  ON conversations (last_message_at DESC NULLS LAST) WHERE deleted_at IS NULL;
-- Một phòng ban / dự án chỉ có MỘT nhóm tự động.
CREATE UNIQUE INDEX idx_conversations_department
  ON conversations (department_id) WHERE department_id IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX idx_conversations_project
  ON conversations (project_id) WHERE project_id IS NOT NULL AND deleted_at IS NULL;

CREATE TABLE conversation_members (
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    employee_id     UUID NOT NULL REFERENCES employees(id)     ON DELETE CASCADE,

    is_admin BOOLEAN NOT NULL DEFAULT FALSE,

    -- Tin nhắn cuối người này đã đọc. Đếm chưa đọc = số tin sau mốc này.
    --
    -- Lưu MỘT mốc thay vì một dòng cho mỗi tin đã đọc: chat sinh ra rất
    -- nhiều tin, và bảng "ai đã đọc tin nào" sẽ lớn gấp bội bảng tin nhắn.
    last_read_message_id UUID,
    last_read_at         TIMESTAMPTZ,

    is_pinned BOOLEAN NOT NULL DEFAULT FALSE,
    is_muted  BOOLEAN NOT NULL DEFAULT FALSE,

    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at   TIMESTAMPTZ,

    PRIMARY KEY (conversation_id, employee_id)
);

CREATE INDEX idx_conversation_members_employee
  ON conversation_members (employee_id) WHERE left_at IS NULL;

-- =========================================================================
-- TIN NHẮN
-- =========================================================================
CREATE TYPE message_kind AS ENUM ('text', 'file', 'image', 'system');

CREATE TABLE messages (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_id       UUID REFERENCES employees(id) ON DELETE SET NULL,

    kind    message_kind NOT NULL DEFAULT 'text',
    content TEXT NOT NULL,

    -- Trả lời một tin khác. SET NULL khi tin gốc bị xoá cứng — mất ngữ cảnh
    -- vẫn hơn mất cả câu trả lời.
    reply_to_id UUID REFERENCES messages(id) ON DELETE SET NULL,

    -- client_message_id chống trùng khi client gửi lại.
    --
    -- Mất mạng giữa chừng, client không biết tin đã tới hay chưa nên gửi
    -- lại. Không có khoá này thì người nhận thấy tin đôi, và đó là lỗi
    -- người dùng nhớ rất lâu.
    client_message_id VARCHAR(64),

    -- Vector tìm kiếm toàn văn, cập nhật bằng trigger.
    --
    -- Cột sinh sẵn (GENERATED) không dùng được vì f_unaccent là hàm
    -- STABLE chứ không IMMUTABLE. Trigger là cách còn lại, và nó cũng cho
    -- phép đổi cách lập chỉ mục sau này mà không phải viết lại cả bảng.
    search_vector tsvector,

    edited_at  TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Chỉ mục chính: đọc lịch sử một hội thoại, mới nhất trước, phân trang
-- bằng cursor theo (created_at, id).
CREATE INDEX idx_messages_conversation
  ON messages (conversation_id, created_at DESC, id DESC);

CREATE UNIQUE INDEX idx_messages_client_id
  ON messages (conversation_id, sender_id, client_message_id)
  WHERE client_message_id IS NOT NULL;

CREATE INDEX idx_messages_search ON messages USING GIN (search_vector);

-- Cập nhật vector tìm kiếm. Bỏ dấu để gõ không dấu vẫn tìm ra — thói quen
-- gõ nhanh rất phổ biến khi tìm trong chat.
CREATE OR REPLACE FUNCTION messages_update_search()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.search_vector := to_tsvector('simple', f_unaccent(COALESCE(NEW.content, '')));
    RETURN NEW;
END $$;

CREATE TRIGGER trg_messages_search
  BEFORE INSERT OR UPDATE OF content ON messages
  FOR EACH ROW EXECUTE FUNCTION messages_update_search();

-- Nâng last_message_at của hội thoại mỗi khi có tin mới.
--
-- Làm bằng trigger chứ không để Go tự cập nhật: mọi đường ghi tin nhắn đều
-- phải nâng mốc này, và trigger không bao giờ quên.
CREATE OR REPLACE FUNCTION messages_touch_conversation()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    UPDATE conversations SET last_message_at = NEW.created_at
    WHERE id = NEW.conversation_id;
    RETURN NEW;
END $$;

CREATE TRIGGER trg_messages_touch_conversation
  AFTER INSERT ON messages
  FOR EACH ROW EXECUTE FUNCTION messages_touch_conversation();

-- =========================================================================
-- TỆP ĐÍNH KÈM TRONG TIN NHẮN
-- =========================================================================
CREATE TABLE message_attachments (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,

    -- Khoá object trên Cloudflare R2, KHÔNG phải URL. Presigned URL có hạn,
    -- lưu lại thì ít lâu sau là hỏng.
    storage_key  TEXT NOT NULL,
    file_name    VARCHAR(255) NOT NULL,
    content_type VARCHAR(127) NOT NULL,
    size_bytes   BIGINT NOT NULL,

    -- Kích thước ảnh, để khung chat chừa sẵn chỗ và không bị giật khi ảnh
    -- tải xong. NULL với tệp không phải ảnh.
    width  INT,
    height INT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_message_attachments_size CHECK (size_bytes > 0)
);

CREATE INDEX idx_message_attachments_message ON message_attachments (message_id);

-- =========================================================================
-- TRIGGER CẬP NHẬT updated_at
-- =========================================================================
CREATE TRIGGER trg_conversations_updated BEFORE UPDATE ON conversations
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
