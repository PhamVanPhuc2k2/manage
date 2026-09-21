-- Quyền của Phase 2.
--
-- Lưu ý về mô hình quyền ở module dự án: quyền hệ thống dưới đây chỉ quyết
-- định "được phép làm loại việc này hay không". Việc "được đụng vào ĐÚNG dự
-- án nào" do bảng project_members quyết định và kiểm tra ở tầng usecase —
-- giống như phạm vi phòng ban của module nhân sự.

INSERT INTO permissions (code, resource, action, description) VALUES
  ('project:read',   'project', 'read',   'Xem dự án'),
  ('project:create', 'project', 'create', 'Tạo dự án mới'),
  ('project:update', 'project', 'update', 'Sửa dự án, quản lý thành viên'),
  ('project:delete', 'project', 'delete', 'Xoá dự án'),
  ('task:read',      'task',    'read',   'Xem công việc'),
  ('task:create',    'task',    'create', 'Tạo công việc'),
  ('task:update',    'task',    'update', 'Sửa công việc, đổi trạng thái'),
  ('task:delete',    'task',    'delete', 'Xoá công việc')
ON CONFLICT (code) DO NOTHING;

-- admin: tất cả quyền.
--
-- Chạy lại CROSS JOIN cho admin là CẦN THIẾT, không thừa: migration 000003
-- chỉ gán những quyền tồn tại vào lúc đó. Tám quyền vừa thêm ở trên sẽ
-- không đến tay admin nếu không có câu này.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'admin'
ON CONFLICT DO NOTHING;

-- director: xem tất cả.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'director' AND p.action = 'read' AND p.resource IN ('project', 'task')
ON CONFLICT DO NOTHING;

-- manager: quản lý dự án và công việc trong phạm vi của mình.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'manager' AND p.code IN
      ('project:read', 'project:create', 'project:update',
       'task:read', 'task:create', 'task:update', 'task:delete')
ON CONFLICT DO NOTHING;

-- employee: làm việc được giao.
--
-- Có task:create vì người làm phải tự tách được việc của mình thành task
-- con. Không có task:delete — xoá việc người khác giao là quyết định của
-- quản lý, không phải của người thực hiện.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'employee' AND p.code IN
      ('project:read', 'task:read', 'task:create', 'task:update')
ON CONFLICT DO NOTHING;

-- hr: KHÔNG gán quyền dự án.
--
-- Nhân sự quản lý hồ sơ con người, không quản lý tiến độ công việc. Gán
-- thừa quyền ở đây là mở cho cả phòng nhân sự đọc toàn bộ dự án công ty.
