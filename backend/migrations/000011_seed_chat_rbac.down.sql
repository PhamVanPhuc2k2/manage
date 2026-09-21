-- Gỡ quyền Phase 5. role_permissions tự dọn theo nhờ ON DELETE CASCADE.
DELETE FROM permissions WHERE resource = 'chat';
