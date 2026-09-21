-- Gỡ quyền Phase 2. role_permissions tự dọn theo nhờ ON DELETE CASCADE.
DELETE FROM permissions WHERE resource IN ('project', 'task');
