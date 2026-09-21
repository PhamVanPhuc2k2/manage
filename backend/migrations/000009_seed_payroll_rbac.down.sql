-- Gỡ quyền Phase 4. role_permissions tự dọn theo nhờ ON DELETE CASCADE.
DELETE FROM permissions WHERE resource IN ('payroll', 'salary', 'audit');
