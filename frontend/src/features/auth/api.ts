import { api } from "@/lib/api-client";
import type { Session } from "./types";

export async function listSessions(): Promise<Session[]> {
  const { data } = await api.get<Session[]>("/auth/sessions");
  return data ?? [];
}

export async function logoutAll(): Promise<void> {
  await api.post("/auth/logout-all");
}

export async function changePassword(
  oldPassword: string,
  newPassword: string,
): Promise<void> {
  await api.post("/auth/change-password", {
    old_password: oldPassword,
    new_password: newPassword,
  });
}
