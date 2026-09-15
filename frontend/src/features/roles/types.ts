export type Role = {
  code: string;
  name: string;
  description?: string;
  scope: "all" | "department" | "self";
  is_system: boolean;
  permissions: string[];
};

export const SCOPE_LABEL: Record<string, string> = {
  all: "Toàn công ty",
  department: "Phòng ban",
  self: "Cá nhân",
};
