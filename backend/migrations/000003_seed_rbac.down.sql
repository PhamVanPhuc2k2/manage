DELETE FROM role_permissions;
DELETE FROM user_roles;
DELETE FROM roles WHERE is_system = TRUE;
DELETE FROM permissions;
