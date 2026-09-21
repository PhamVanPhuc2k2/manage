-- Quyền của Phase 5.
--
-- Thông báo KHÔNG có quyền riêng: ai đăng nhập được thì đọc thông báo của
-- chính mình, và chỉ của chính mình — tầng usecase khoá cứng theo actor,
-- không có đường nào xem thông báo người khác.
--
-- Chat thì có quyền, vì một số tổ chức muốn tắt hẳn chat cho một nhóm nhân
-- viên nào đó (ví dụ nhân sự thời vụ).

INSERT INTO permissions (code, resource, action, description) VALUES
  ('chat:read',   'chat', 'read',   'Xem và nhắn tin trong hội thoại của mình'),
  ('chat:create', 'chat', 'create', 'Tạo hội thoại và nhóm chat mới')
ON CONFLICT (code) DO NOTHING;

-- admin: tất cả quyền.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'admin'
ON CONFLICT DO NOTHING;

-- Mọi vai trò còn lại đều được chat: đây là công cụ làm việc hằng ngày,
-- không phải đặc quyền.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code IN ('director', 'hr', 'manager', 'employee')
  AND p.resource = 'chat'
ON CONFLICT DO NOTHING;
