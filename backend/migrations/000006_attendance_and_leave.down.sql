DROP TRIGGER IF EXISTS trg_leave_balances_updated      ON leave_balances;
DROP TRIGGER IF EXISTS trg_leave_requests_updated      ON leave_requests;
DROP TRIGGER IF EXISTS trg_adjustments_updated         ON attendance_adjustments;
DROP TRIGGER IF EXISTS trg_attendance_days_updated     ON attendance_days;
DROP TRIGGER IF EXISTS trg_attendance_sessions_updated ON attendance_sessions;
DROP TRIGGER IF EXISTS trg_work_schedules_updated      ON work_schedules;

DROP TABLE IF EXISTS leave_balances;
DROP TABLE IF EXISTS leave_requests;
DROP TABLE IF EXISTS attendance_adjustments;
DROP TABLE IF EXISTS attendance_days;
DROP TABLE IF EXISTS attendance_sessions;
DROP TABLE IF EXISTS holidays;
DROP TABLE IF EXISTS work_schedules;

DROP TYPE IF EXISTS leave_day_part;
DROP TYPE IF EXISTS leave_type;
DROP TYPE IF EXISTS approval_status;
DROP TYPE IF EXISTS attendance_day_status;
DROP TYPE IF EXISTS attendance_source;
