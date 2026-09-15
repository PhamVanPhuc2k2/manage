-- Nạp danh mục quyền và vai trò hệ thống.
--
-- KHÔNG seed tài khoản admin ở đây — mật khẩu sẽ nằm trong git vĩnh viễn.
-- Dùng lệnh `cmd/seed` để tạo tài khoản đầu tiên.

INSERT INTO permissions (code, resource, action, description) VALUES
  ('employee:read',      'employee',   'read',    'Xem thông tin nhân viên'),
  ('employee:create',    'employee',   'create',  'Thêm nhân viên mới'),
  ('employee:update',    'employee',   'update',  'Sửa thông tin nhân viên'),
  ('employee:delete',    'employee',   'delete',  'Vô hiệu hoá nhân viên'),
  ('department:read',    'department', 'read',    'Xem phòng ban'),
  ('department:create',  'department', 'create',  'Tạo phòng ban'),
  ('department:update',  'department', 'update',  'Sửa phòng ban'),
  ('department:delete',  'department', 'delete',  'Xoá phòng ban'),
  ('position:read',      'position',   'read',    'Xem chức vụ'),
  ('position:manage',    'position',   'manage',  'Quản lý chức vụ'),
  ('role:read',          'role',       'read',    'Xem vai trò'),
  ('role:assign',        'role',       'assign',  'Gán vai trò cho người dùng')
ON CONFLICT (code) DO NOTHING;

INSERT INTO roles (code, name, scope, is_system, description) VALUES
  ('admin',    'Quản trị hệ thống', 'all',        TRUE, 'Toàn quyền, gồm cả quản trị kỹ thuật'),
  ('director', 'Giám đốc',          'all',        TRUE, 'Xem toàn bộ dữ liệu công ty'),
  ('hr',       'Nhân sự',           'all',        TRUE, 'Quản lý hồ sơ nhân viên và phòng ban'),
  ('manager',  'Trưởng phòng',      'department', TRUE, 'Quản lý nhân viên trong phòng mình'),
  ('employee', 'Nhân viên',         'self',       TRUE, 'Chỉ xem thông tin của chính mình')
ON CONFLICT (code) DO NOTHING;

-- admin: tất cả quyền
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'admin'
ON CONFLICT DO NOTHING;

-- hr: toàn quyền nhân sự, không đụng vai trò
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'hr' AND p.resource IN ('employee', 'department', 'position')
ON CONFLICT DO NOTHING;

-- director: xem tất cả
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'director' AND p.action = 'read'
ON CONFLICT DO NOTHING;

-- manager: xem, và sửa nhân viên trong phạm vi phòng mình
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'manager' AND p.code IN
      ('employee:read', 'employee:update', 'department:read', 'position:read')
ON CONFLICT DO NOTHING;

-- employee: chỉ xem
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'employee' AND p.code IN
      ('employee:read', 'department:read', 'position:read')
ON CONFLICT DO NOTHING;
