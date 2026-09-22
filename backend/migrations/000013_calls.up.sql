-- Phase 7: gọi video và gọi thoại trong hội thoại chat.
--
-- Hai bảng: một cuộc gọi, và những ai đã tham gia nó.
--
-- Vì sao lưu lại thay vì chỉ giữ trong bộ nhớ: cuộc gọi là hoạt động công
-- việc. Cần biết ai họp với ai bao lâu, và cần con số tỉ lệ phải relay qua
-- TURN để ước lượng chi phí băng thông. Cả hai thứ đó không tự có nếu trạng
-- thái chỉ sống trong RAM của một tiến trình.

-- =========================================================================
-- CUỘC GỌI
-- =========================================================================

CREATE TYPE call_kind AS ENUM ('audio', 'video');

-- Trạng thái theo vòng đời, không phải theo kết quả.
--
--   ringing   — đã mời, chưa ai bắt máy
--   active    — có ít nhất hai người trong phòng
--   ended     — kết thúc bình thường
--   missed    — hết 45 giây không ai bắt máy
--   rejected  — người nhận bấm từ chối
--   cancelled — người gọi cúp trước khi ai kịp bắt máy
--   failed    — không thiết lập được kết nối media
--
-- Tách 'missed', 'rejected' và 'cancelled' thay vì gộp vào 'ended': ba
-- tình huống này nói ba chuyện hoàn toàn khác nhau với người dùng, và giao
-- diện hiển thị khác nhau ("gọi nhỡ" đỏ, "đã từ chối" xám).
CREATE TYPE call_status AS ENUM (
    'ringing', 'active', 'ended', 'missed', 'rejected', 'cancelled', 'failed'
);

CREATE TABLE calls (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,

    -- Người bấm nút gọi. SET NULL khi họ nghỉ việc — lịch sử cuộc gọi của
    -- những người còn lại vẫn phải đọc được.
    initiator_id UUID REFERENCES employees(id) ON DELETE SET NULL,

    kind   call_kind   NOT NULL,
    status call_status NOT NULL DEFAULT 'ringing',

    -- Tên phòng trên SFU. Đặt theo call_id chứ không theo conversation_id:
    -- một hội thoại có nhiều cuộc gọi theo thời gian, và dùng chung tên
    -- phòng sẽ khiến người vào muộn rơi vào phòng của cuộc gọi trước.
    room_name VARCHAR(128) NOT NULL UNIQUE,

    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at   TIMESTAMPTZ,

    -- Lý do kết thúc, dạng chuỗi tự do để không phải migration mỗi lần thêm
    -- một nguyên nhân mới. Giá trị hay gặp: 'hangup', 'timeout', 'busy',
    -- 'network', 'no_answer'.
    end_reason VARCHAR(64),

    -- Tỉ lệ người phải đi qua TURN, tính lúc cuộc gọi kết thúc.
    --
    -- Đây là chỉ số quyết định chi phí băng thông: mỗi kết nối relay đi qua
    -- máy chủ hai lần. Không đo thì hoá đơn băng thông là một bất ngờ.
    relay_ratio NUMERIC(4, 3),

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Kết thúc phải sau lúc bắt đầu. Ràng buộc ở database vì thời lượng
    -- cuộc gọi đi vào báo cáo, và một giá trị âm sẽ lặng lẽ làm hỏng trung
    -- bình cộng.
    CONSTRAINT chk_call_time CHECK (ended_at IS NULL OR ended_at >= started_at)
);

-- Mỗi hội thoại chỉ được có MỘT cuộc gọi đang diễn ra.
--
-- Không có ràng buộc này thì hai người cùng bấm gọi trong một giây sẽ tạo
-- ra hai phòng, mỗi người vào một phòng, và cả hai ngồi nhìn màn hình trống
-- mà không hiểu vì sao. Chỉ mục một phần là cách duy nhất diễn đạt được
-- "duy nhất trong số những dòng đang hoạt động".
CREATE UNIQUE INDEX uq_calls_active_per_conversation
    ON calls (conversation_id)
    WHERE status IN ('ringing', 'active');

CREATE INDEX idx_calls_conversation ON calls (conversation_id, started_at DESC);
CREATE INDEX idx_calls_initiator    ON calls (initiator_id, started_at DESC);

-- Truy vấn "cuộc gọi đang chạy" chạy ở mọi lời mời, nên có chỉ mục riêng.
CREATE INDEX idx_calls_live ON calls (status) WHERE status IN ('ringing', 'active');

-- =========================================================================
-- NGƯỜI THAM GIA
-- =========================================================================

CREATE TABLE call_participants (
    id      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    call_id UUID NOT NULL REFERENCES calls(id) ON DELETE CASCADE,

    -- RESTRICT chứ không SET NULL: bản ghi tham gia mà không biết ai tham
    -- gia thì vô nghĩa. Nhân viên nghỉ việc chỉ bị xoá mềm nên ràng buộc
    -- này không chặn nghiệp vụ nhân sự.
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE RESTRICT,

    joined_at TIMESTAMPTZ,
    left_at   TIMESTAMPTZ,

    -- Ba cờ dưới đây ghi lại việc người đó ĐÃ TỪNG bật, không phải trạng
    -- thái hiện tại. Trạng thái hiện tại sống trong SFU và đổi liên tục;
    -- cái đáng lưu là "người này có chia sẻ màn hình trong cuộc họp không",
    -- phục vụ audit.
    had_audio  BOOLEAN NOT NULL DEFAULT FALSE,
    had_video  BOOLEAN NOT NULL DEFAULT FALSE,
    had_screen BOOLEAN NOT NULL DEFAULT FALSE,

    -- Kết nối của người này có phải đi qua TURN không. NULL khi chưa xác
    -- định được (rời phòng trước lúc ICE xong).
    used_relay BOOLEAN,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_participant_time
        CHECK (left_at IS NULL OR joined_at IS NULL OR left_at >= joined_at)
);

-- Một người chỉ có một dòng trong mỗi cuộc gọi, kể cả khi họ rớt mạng rồi
-- vào lại. Vào lại là cùng một lần tham gia dưới góc nhìn nghiệp vụ.
CREATE UNIQUE INDEX uq_call_participant
    ON call_participants (call_id, employee_id);

CREATE INDEX idx_call_participants_employee
    ON call_participants (employee_id, created_at DESC);

-- =========================================================================
-- QUYỀN
-- =========================================================================

-- Quyền gọi tách khỏi quyền chat.
--
-- Một cuộc gọi tốn băng thông máy chủ và làm phiền người khác ngay lập tức,
-- còn một tin nhắn thì không. Công ty có thể muốn cho mọi người nhắn tin
-- nhưng chỉ vài nhóm được gọi video.
INSERT INTO permissions (code, resource, action, description) VALUES
    ('call:start', 'call', 'start',
     'Gọi thoại hoặc gọi video trong hội thoại mình là thành viên'),
    ('call:read',  'call', 'read',
     'Xem lịch sử cuộc gọi và thời lượng trong hội thoại của mình')
ON CONFLICT (code) DO NOTHING;

-- admin: toàn quyền, theo đúng mẫu của các migration RBAC trước.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'admin'
ON CONFLICT DO NOTHING;

-- Ai đã chat được thì gọi được: gọi nhau là công cụ làm việc hằng ngày,
-- không phải đặc quyền. Giữ hai quyền RIÊNG với chat để về sau siết lại
-- được mà không phải migration thêm lần nữa — một cuộc gọi tốn băng thông máy
-- chủ và làm phiền người khác ngay lập tức, còn một tin nhắn thì không.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('director', 'hr', 'manager', 'employee')
  AND p.resource = 'call'
ON CONFLICT DO NOTHING;
