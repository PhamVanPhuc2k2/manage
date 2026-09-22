DROP TABLE IF EXISTS call_participants;
DROP TABLE IF EXISTS calls;

DROP TYPE IF EXISTS call_status;
DROP TYPE IF EXISTS call_kind;

-- Gỡ quyền. role_permissions tự đi theo nhờ ON DELETE CASCADE trên khoá ngoại.
DELETE FROM permissions WHERE resource = 'call';
