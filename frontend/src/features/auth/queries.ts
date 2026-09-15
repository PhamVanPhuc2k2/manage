import { useMutation, useQuery } from "@tanstack/react-query";

import { changePassword, listSessions } from "./api";

export const sessionKeys = {
  all: ["sessions"] as const,
};

export function useSessions() {
  return useQuery({
    queryKey: sessionKeys.all,
    queryFn: listSessions,
    // Danh sách thiết bị thay đổi thường xuyên hơn dữ liệu danh mục —
    // dùng staleTime mặc định 30 giây.
  });
}

export function useChangePassword() {
  return useMutation({
    mutationFn: (v: { oldPassword: string; newPassword: string }) =>
      changePassword(v.oldPassword, v.newPassword),
  });
}
