import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import {
  createPosition,
  deletePosition,
  listPositions,
  updatePosition,
} from "./api";

export const positionKeys = {
  all: ["positions"] as const,
  list: () => [...positionKeys.all, "list"] as const,
};

export function usePositions() {
  return useQuery({
    queryKey: positionKeys.list(),
    queryFn: listPositions,
    // Chức vụ gần như không đổi. Xem ghi chú ở departments/queries.ts.
    staleTime: 5 * 60_000,
  });
}

function usePositionMutation<TArgs, TResult>(fn: (args: TArgs) => Promise<TResult>) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: positionKeys.all });
    },
  });
}

export function useCreatePosition() {
  return usePositionMutation(createPosition);
}

export function useUpdatePosition(id: string) {
  return usePositionMutation((body: unknown) => updatePosition(id, body));
}

export function useDeletePosition() {
  return usePositionMutation(deletePosition);
}
