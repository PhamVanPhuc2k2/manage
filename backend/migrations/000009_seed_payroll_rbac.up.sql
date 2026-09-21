-- Quyền của Phase 4.
--
-- Lương là dữ liệu nhạy cảm nhất trong hệ thống. Mô hình quyền ở đây chặt
-- hơn mọi module khác:
--
--   payroll:read_own  — xem phiếu lương của CHÍNH MÌNH. Ai cũng có.
--   payroll:read_all  — xem bảng lương người khác. Chỉ nhân sự và giám đốc.
--   payroll:manage    — tạo kỳ, chạy tính lương, sửa phiếu khi còn nháp.
--   payroll:approve   — khoá kỳ và đánh dấu đã trả. Tách khỏi manage để
--                       người chạy tính lương không tự chốt được kỳ của
--                       chính mình làm — nguyên tắc bốn mắt.
--   salary:read       — xem cấu hình lương (mức lương từng người).
--   salary:manage     — sửa cấu hình lương, tham số thuế và bảo hiểm.

INSERT INTO permissions (code, resource, action, description) VALUES
  ('payroll:read_own', 'payroll', 'read_own', 'Xem phiếu lương của chính mình'),
  ('payroll:read_all', 'payroll', 'read_all', 'Xem bảng lương toàn công ty'),
  ('payroll:manage',   'payroll', 'manage',   'Tạo kỳ lương và chạy tính lương'),
  ('payroll:approve',  'payroll', 'approve',  'Khoá kỳ lương và xác nhận đã trả'),
  ('salary:read',      'salary',  'read',     'Xem cấu hình lương nhân viên'),
  ('salary:manage',    'salary',  'manage',   'Sửa cấu hình lương và tham số tính lương'),
  ('audit:read',       'audit',   'read',     'Xem nhật ký truy cập dữ liệu nhạy cảm')
ON CONFLICT (code) DO NOTHING;

-- admin: tất cả quyền.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'admin'
ON CONFLICT DO NOTHING;

-- hr: vận hành toàn bộ nghiệp vụ lương, nhưng KHÔNG có payroll:approve.
--
-- Người chạy tính lương không được tự chốt kỳ mình vừa chạy. Đây là nguyên
-- tắc bốn mắt, và nó chỉ có tác dụng khi hai quyền nằm ở hai người.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'hr' AND p.code IN
      ('payroll:read_own', 'payroll:read_all', 'payroll:manage',
       'salary:read', 'salary:manage')
ON CONFLICT DO NOTHING;

-- director: xem toàn bộ và chốt kỳ lương.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'director' AND p.code IN
      ('payroll:read_own', 'payroll:read_all', 'payroll:approve',
       'salary:read', 'audit:read')
ON CONFLICT DO NOTHING;

-- manager: CHỈ xem phiếu lương của chính mình.
--
-- Cố ý không cho trưởng phòng xem lương nhân viên phòng mình. Khác với
-- chấm công, lương không phải thông tin cần để điều hành công việc hằng
-- ngày, và mở ra thì không thu lại được.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'manager' AND p.code = 'payroll:read_own'
ON CONFLICT DO NOTHING;

-- employee: chỉ phiếu lương của mình.
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id FROM roles r CROSS JOIN permissions p
WHERE r.code = 'employee' AND p.code = 'payroll:read_own'
ON CONFLICT DO NOTHING;
