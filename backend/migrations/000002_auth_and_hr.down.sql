DROP TRIGGER IF EXISTS trg_users_updated       ON users;
DROP TRIGGER IF EXISTS trg_employees_updated   ON employees;
DROP TRIGGER IF EXISTS trg_positions_updated   ON positions;
DROP TRIGGER IF EXISTS trg_departments_updated ON departments;
DROP TRIGGER IF EXISTS trg_companies_updated   ON companies;
DROP FUNCTION IF EXISTS set_updated_at();

DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TYPE  IF EXISTS data_scope;

DROP TABLE IF EXISTS users;

-- Phải gỡ khoá ngoại trước, vì departments và employees tham chiếu vòng nhau.
ALTER TABLE IF EXISTS departments DROP CONSTRAINT IF EXISTS fk_departments_manager;

DROP TABLE IF EXISTS employees;
DROP TYPE  IF EXISTS employee_status;
DROP TYPE  IF EXISTS work_mode;
DROP FUNCTION IF EXISTS f_unaccent(text);

DROP TABLE IF EXISTS positions;
DROP TABLE IF EXISTS departments;
DROP TABLE IF EXISTS companies;
