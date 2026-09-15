export type Department = {
  id: string;
  parent_id?: string;
  code: string;
  name: string;
  description?: string;
  manager_id?: string;
  manager_name?: string;
  employee_count: number;
  children?: Department[];
  created_at: string;
};
