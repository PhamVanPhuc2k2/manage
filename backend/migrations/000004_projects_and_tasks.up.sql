-- =========================================================================
-- PHASE 2 — DỰ ÁN & CÔNG VIỆC
-- =========================================================================

CREATE TYPE project_status AS ENUM
    ('planning', 'active', 'on_hold', 'completed', 'cancelled');

-- Vai trò TRONG MỘT DỰ ÁN. Khác hoàn toàn với vai trò hệ thống (bảng roles):
-- một người có thể là 'owner' ở dự án này và 'viewer' ở dự án khác.
CREATE TYPE project_role AS ENUM ('owner', 'member', 'viewer');

CREATE TYPE task_status   AS ENUM ('todo', 'in_progress', 'review', 'done');
CREATE TYPE task_priority AS ENUM ('low', 'medium', 'high', 'urgent');

-- =========================================================================
-- DỰ ÁN
-- =========================================================================
CREATE TABLE projects (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    company_id   UUID NOT NULL REFERENCES companies(id) ON DELETE RESTRICT,
    code         VARCHAR(20)  NOT NULL,
    name         VARCHAR(255) NOT NULL,
    description  TEXT,
    status       project_status NOT NULL DEFAULT 'planning',

    -- Chủ dự án. NOT NULL: dự án không có chủ thì không ai chịu trách nhiệm
    -- đóng nó, và mọi kiểm tra quyền sửa sẽ phải xử lý thêm một nhánh NULL.
    owner_id     UUID NOT NULL REFERENCES employees(id) ON DELETE RESTRICT,

    -- Phòng ban chủ quản. NULL được: dự án liên phòng ban là chuyện bình thường.
    department_id UUID REFERENCES departments(id) ON DELETE SET NULL,

    start_date   DATE,
    due_date     DATE,
    completed_at TIMESTAMPTZ,

    -- Bộ đếm sinh mã task trong dự án (PRJ-1, PRJ-2...).
    --
    -- Đếm ở ĐÂY chứ không dùng MAX(seq)+1 lúc tạo task: hai người tạo task
    -- cùng lúc sẽ cùng đọc ra một giá trị MAX và sinh trùng mã. UPDATE trên
    -- dòng này khoá dòng, nên hai giao dịch buộc phải xếp hàng.
    task_seq     INT NOT NULL DEFAULT 0,

    deleted_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_projects_dates
        CHECK (start_date IS NULL OR due_date IS NULL OR start_date <= due_date)
);

CREATE UNIQUE INDEX idx_projects_code
  ON projects (company_id, lower(code)) WHERE deleted_at IS NULL;
CREATE INDEX idx_projects_owner  ON projects (owner_id);
CREATE INDEX idx_projects_status ON projects (status) WHERE deleted_at IS NULL;

-- =========================================================================
-- THÀNH VIÊN DỰ ÁN
-- =========================================================================
CREATE TABLE project_members (
    project_id  UUID NOT NULL REFERENCES projects(id)  ON DELETE CASCADE,
    employee_id UUID NOT NULL REFERENCES employees(id) ON DELETE RESTRICT,
    role        project_role NOT NULL DEFAULT 'member',
    added_by    UUID REFERENCES employees(id) ON DELETE SET NULL,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (project_id, employee_id)
);

CREATE INDEX idx_project_members_employee ON project_members (employee_id);

-- =========================================================================
-- CÔNG VIỆC
-- =========================================================================
CREATE TABLE tasks (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

    -- Số thứ tự trong dự án, dùng để hiển thị "PRJ-42".
    seq         INT NOT NULL,

    -- Task cha. ON DELETE CASCADE: xoá task cha thì task con đi theo.
    -- Giới hạn "không lồng quá 2 cấp" được chặn ở tầng usecase — database
    -- không diễn đạt được ràng buộc đó bằng CHECK.
    parent_task_id UUID REFERENCES tasks(id) ON DELETE CASCADE,

    title       VARCHAR(500) NOT NULL,
    description TEXT,
    status      task_status   NOT NULL DEFAULT 'todo',
    priority    task_priority NOT NULL DEFAULT 'medium',

    -- Người thực hiện. SET NULL khi nhân viên bị xoá: mất người làm thì
    -- task vẫn phải còn để giao lại, không được biến mất theo.
    assignee_id UUID REFERENCES employees(id) ON DELETE SET NULL,
    -- Người tạo task.
    reporter_id UUID NOT NULL REFERENCES employees(id) ON DELETE RESTRICT,

    due_date       TIMESTAMPTZ,
    estimate_hours NUMERIC(6,2),

    -- Thứ tự trong cột Kanban.
    --
    -- Dùng số thực để chèn được vào GIỮA hai task mà không phải đánh số lại
    -- cả cột: task mới nằm giữa A và B thì sort_order = (A + B) / 2.
    -- Sau rất nhiều lần chèn cùng một chỗ, khoảng cách hai số sẽ nhỏ tới mức
    -- float64 không phân biệt nổi — usecase phát hiện và đánh số lại cả cột.
    sort_order  DOUBLE PRECISION NOT NULL DEFAULT 0,

    started_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,

    deleted_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_tasks_estimate CHECK (estimate_hours IS NULL OR estimate_hours >= 0),
    -- Task không thể là con của chính nó. Vòng lặp sâu hơn do usecase chặn.
    CONSTRAINT chk_tasks_not_self_parent CHECK (parent_task_id IS NULL OR parent_task_id <> id)
);

CREATE UNIQUE INDEX idx_tasks_seq ON tasks (project_id, seq);
CREATE INDEX idx_tasks_project  ON tasks (project_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_assignee ON tasks (assignee_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_tasks_parent   ON tasks (parent_task_id) WHERE parent_task_id IS NOT NULL;
-- Chỉ mục phục vụ bảng Kanban: lấy theo cột, đã sắp sẵn.
CREATE INDEX idx_tasks_board    ON tasks (project_id, status, sort_order) WHERE deleted_at IS NULL;
-- Phục vụ báo cáo task quá hạn.
CREATE INDEX idx_tasks_due      ON tasks (due_date) WHERE deleted_at IS NULL AND status <> 'done';

-- =========================================================================
-- BÌNH LUẬN
-- =========================================================================
CREATE TABLE task_comments (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id    UUID NOT NULL REFERENCES tasks(id)     ON DELETE CASCADE,
    author_id  UUID NOT NULL REFERENCES employees(id) ON DELETE RESTRICT,
    content    TEXT NOT NULL,

    -- Danh sách người được @mention, rút ra từ nội dung lúc lưu.
    --
    -- Lưu sẵn thay vì phân tích lại mỗi lần đọc: nội dung không đổi thì kết
    -- quả cũng không đổi, và Phase 5 cần đọc nhanh danh sách này để sinh
    -- thông báo.
    mentioned_ids UUID[] NOT NULL DEFAULT '{}',

    edited_at  TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_task_comments_task ON task_comments (task_id, created_at)
  WHERE deleted_at IS NULL;

-- =========================================================================
-- TỆP ĐÍNH KÈM
-- =========================================================================
CREATE TABLE task_attachments (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id      UUID NOT NULL REFERENCES tasks(id)     ON DELETE CASCADE,
    uploaded_by  UUID NOT NULL REFERENCES employees(id) ON DELETE RESTRICT,

    -- Khoá object trên Cloudflare R2. KHÔNG lưu URL: presigned URL có hạn,
    -- lưu lại thì ít lâu sau là hỏng. URL được ký lại mỗi lần đọc.
    storage_key  TEXT NOT NULL,
    file_name    VARCHAR(255) NOT NULL,
    content_type VARCHAR(127) NOT NULL,
    size_bytes   BIGINT NOT NULL,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_task_attachments_size CHECK (size_bytes > 0)
);

CREATE INDEX idx_task_attachments_task ON task_attachments (task_id);

-- =========================================================================
-- NHẬT KÝ THAY ĐỔI
--
-- Bảng chỉ ghi thêm, không sửa không xoá. Đây là câu trả lời cho "ai đổi
-- trạng thái task này lúc nào" — câu hỏi luôn xuất hiện khi có tranh cãi.
-- =========================================================================
CREATE TABLE task_activities (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id    UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,

    -- SET NULL khi nhân viên bị xoá cứng: mất tên người làm vẫn hơn mất
    -- cả dòng lịch sử.
    actor_id   UUID REFERENCES employees(id) ON DELETE SET NULL,

    action     VARCHAR(40) NOT NULL,
    field      VARCHAR(40),
    old_value  TEXT,
    new_value  TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_task_activities_task ON task_activities (task_id, created_at DESC);

-- =========================================================================
-- GHI NHẬN THỜI GIAN
-- =========================================================================
CREATE TABLE task_timelogs (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    task_id       UUID NOT NULL REFERENCES tasks(id)     ON DELETE CASCADE,
    employee_id   UUID NOT NULL REFERENCES employees(id) ON DELETE RESTRICT,

    -- Lưu bằng PHÚT chứ không phải giờ thập phân: "1.5 giờ" nhập tay dễ
    -- thành 1.05, còn 90 phút thì không hiểu nhầm được.
    spent_minutes INT  NOT NULL,
    note          TEXT,
    -- Ngày làm việc, không phải ngày bấm nút — người ta hay ghi bù hôm sau.
    logged_on     DATE NOT NULL,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_task_timelogs_minutes CHECK (spent_minutes > 0 AND spent_minutes <= 24 * 60)
);

CREATE INDEX idx_task_timelogs_task     ON task_timelogs (task_id);
CREATE INDEX idx_task_timelogs_employee ON task_timelogs (employee_id, logged_on);

-- =========================================================================
-- TRIGGER CẬP NHẬT updated_at
-- =========================================================================
CREATE TRIGGER trg_projects_updated      BEFORE UPDATE ON projects
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_tasks_updated         BEFORE UPDATE ON tasks
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_task_comments_updated BEFORE UPDATE ON task_comments
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER trg_task_timelogs_updated BEFORE UPDATE ON task_timelogs
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
