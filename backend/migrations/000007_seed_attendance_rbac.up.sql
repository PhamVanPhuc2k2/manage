-- Quyền của Phase 3.
--
-- Phân biệt attendance:read (xem công của mình) với attendance:read_all
-- (xem công người khác) là điểm then chốt: dữ liệu chấm công là dữ liệu
-- nhạy cảm về cá nhân, và "ai cũng xem được của nhau" là thứ không sửa lại
-- được sau khi đã lỡ mở.

INSERT INTO permissions (code, resource, action, description) VALUES
  ('attendance:read',     'attendance', 'read',     'Xem công của chính mình'),
  ('attendance:read_all', 'attendance', 'read_all', 'Xem công của người khác trong phạm vi'),
  ('attendance:manage',   'attendance', 'manage',   'Sửa công, duyệt điều chỉnh, khoá kỳ'),
  ('leave:read',          'leave',      'read',     'Xem đơn nghỉ phép'),
  ('leave:create',        'leave',      'create',   'Tạo đơn nghỉ phép'),
  ('leave:approve',       'leave',      'approve',  'Duyệt hoặc từ chối đơn nghỉ phép'),
  ('leave:manage',        'leave',      'manage',   'Quản lý quỹ phép và ngày lễ'),
  ('schedule:manage',     'schedule',   'manage',   'Cấu hình khung giờ làm việc')
ON CONFLICT (code) DO NOTHING;

-- admin: tất cả quyền. Chạy lại CROSS JOIN là CẦN THIẾT — migration trước
-- chỉ gán những quyền tồn tại vào lúc đó.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'admin'
ON CONFLICT DO NOTHING;

-- director: xem toàn bộ.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'director'
  AND p.code IN ('attendance:read', 'attendance:read_all', 'leave:read')
ON CONFLICT DO NOTHING;

-- hr: quản lý toàn bộ công và phép. Đây chính là nghiệp vụ của họ.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'hr' AND p.resource IN ('attendance', 'leave', 'schedule')
ON CONFLICT DO NOTHING;

-- manager: xem công phòng mình và duyệt đơn của nhân viên mình.
--
-- KHÔNG có attendance:manage: sửa số liệu công là việc của nhân sự. Trưởng
-- phòng duyệt yêu cầu điều chỉnh, còn việc ghi đè con số thì không.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'manager' AND p.code IN
      ('attendance:read', 'attendance:read_all',
       'leave:read', 'leave:create', 'leave:approve')
ON CONFLICT DO NOTHING;

-- employee: xem công của mình, gửi đơn nghỉ phép.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'employee' AND p.code IN
      ('attendance:read', 'leave:read', 'leave:create')
ON CONFLICT DO NOTHING;
