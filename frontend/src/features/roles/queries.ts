import { useQuery } from "@tanstack/react-query";

import { listRoles } from "./api";

export const roleKeys = {
  all: ["roles"] as const,
};

export function useRoles() {
  return useQuery({
    queryKey: roleKeys.all,
    queryFn: listRoles,
    // Danh mục vai trò là dữ liệu hệ thống, gần như không bao giờ đổi.
    staleTime: 30 * 60_000,
  });
}
