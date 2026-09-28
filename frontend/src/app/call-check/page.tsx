"use client";

import { AppShell } from "@/components/AppShell";
import { DeviceCheck } from "@/features/call/DeviceCheck";

export default function CallCheckPage() {
  return (
    <AppShell>
      <div className="mb-6">
        <h1 className="text-xl font-semibold">Kiểm tra thiết bị</h1>
        <p className="mt-1 text-sm text-neutral-500">
          Thử camera, micro và loa trước một cuộc gọi quan trọng. Thiết bị chọn
          ở đây được nhớ và dùng cho các cuộc gọi sau.
        </p>
      </div>
      <DeviceCheck />
    </AppShell>
  );
}
