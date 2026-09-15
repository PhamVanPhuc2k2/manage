import { api } from "@/lib/api-client";
import type { Position } from "./types";

export async function listPositions(): Promise<Position[]> {
  const { data } = await api.get<Position[]>("/positions");
  return data ?? [];
}

export async function createPosition(body: unknown): Promise<Position> {
  const { data } = await api.post<Position>("/positions", body);
  return data;
}

export async function updatePosition(id: string, body: unknown): Promise<Position> {
  const { data } = await api.put<Position>(`/positions/${id}`, body);
  return data;
}

export async function deletePosition(id: string): Promise<void> {
  await api.delete(`/positions/${id}`);
}
