-- Gỡ quyền Phase 3. role_permissions tự dọn theo nhờ ON DELETE CASCADE.
DELETE FROM permissions WHERE resource IN ('attendance', 'leave', 'schedule');
