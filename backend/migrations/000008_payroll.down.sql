DROP TRIGGER IF EXISTS trg_payslips_updated          ON payslips;
DROP TRIGGER IF EXISTS trg_payroll_periods_updated   ON payroll_periods;
DROP TRIGGER IF EXISTS trg_salary_structures_updated ON salary_structures;
DROP TRIGGER IF EXISTS trg_payroll_settings_updated  ON payroll_settings;

DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS payslip_items;
DROP TABLE IF EXISTS payslips;
DROP TABLE IF EXISTS payroll_periods;
DROP TABLE IF EXISTS salary_components;
DROP TABLE IF EXISTS salary_structures;
DROP TABLE IF EXISTS tax_brackets;
DROP TABLE IF EXISTS payroll_settings;

DROP TYPE IF EXISTS payroll_status;
DROP TYPE IF EXISTS salary_component_kind;
