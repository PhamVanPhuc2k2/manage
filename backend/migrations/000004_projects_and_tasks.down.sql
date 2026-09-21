DROP TRIGGER IF EXISTS trg_task_timelogs_updated ON task_timelogs;
DROP TRIGGER IF EXISTS trg_task_comments_updated ON task_comments;
DROP TRIGGER IF EXISTS trg_tasks_updated         ON tasks;
DROP TRIGGER IF EXISTS trg_projects_updated      ON projects;

DROP TABLE IF EXISTS task_timelogs;
DROP TABLE IF EXISTS task_activities;
DROP TABLE IF EXISTS task_attachments;
DROP TABLE IF EXISTS task_comments;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS project_members;
DROP TABLE IF EXISTS projects;

DROP TYPE IF EXISTS task_priority;
DROP TYPE IF EXISTS task_status;
DROP TYPE IF EXISTS project_role;
DROP TYPE IF EXISTS project_status;
