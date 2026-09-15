"use client";

import { useRef, useState } from "react";
import { useParams, useRouter } from "next/navigation";
import Link from "next/link";

import { AppShell } from "@/components/AppShell";
import { FormError } from "@/components/form";
import {
  useCreateAccount,
  useDeactivateEmployee,
  useEmployee,
  useEmployeeRoles,
  useRemoveAvatar,
  useSetAccountActive,
  useSetEmployeeRoles,
  useUploadAvatar,
} from "@/features/employees/queries";
import {
  STATUS_LABEL,
  WORK_MODE_LABEL,
} from "@/features/employees/types";
import { useRoles } from "@/features/roles/queries";
import { SCOPE_LABEL } from "@/features/roles/types";
import { ApiError } from "@/lib/api-client";
import { usePermission } from "@/lib/auth/useAuth";

const formatDate = (s?: string) =>
  s ? new Date(s).toLocaleDateString("vi-VN") : "—";

const errMsg = (e: unknown, fallback: string) =>
  e instanceof ApiError ? e.message : e ? fallback : null;

export default function EmployeeDetailPage() {
  const { id } = useParams<{ id: string }>();
  const router = useRouter();
  const { can } = usePermission();

  const { data: employee, isPending, error } = useEmployee(id);

  if (isPending) {
    return (
      <AppShell>
        <p className="text-sm text-neutral-500">Đang tải...</p>
      </AppShell>
    );
  }
  if (error || !employee) {
    return (
      <AppShell>
        <FormError
          message={errMsg(error, "Không tìm thấy nhân viên") ?? "Không tìm thấy nhân viên"}
        />
        <Link href="/employees" className="mt-4 inline-block text-sm underline">
          Quay lại danh sách
        </Link>
      </AppShell>
    );
  }

  return (
    <AppShell>
      <div className="mb-6 flex items-start justify-between">
        <div>
          <Link
            href="/employees"
            className="text-sm text-neutral-500 underline-offset-4 hover:underline"
          >
            ← Danh sách nhân viên
          </Link>
          <h1 className="mt-2 text-xl font-semibold">{employee.full_name}</h1>
          <p className="mt-1 text-sm text-neutral-500">
            {employee.employee_code} · {STATUS_LABEL[employee.status]}
          </p>
        </div>

        <div className="flex gap-2">
          {can("employee:update") && (
            <Link
              href={`/employees/${id}/edit`}
              className="rounded border border-neutral-300 px-3 py-1.5 text-sm hover:bg-neutral-100 dark:border-neutral-700 dark:hover:bg-neutral-800"
            >
              Sửa hồ sơ
            </Link>
          )}
          {can("employee:delete") && (
            <DeactivateButton
              employeeId={id}
              name={employee.full_name}
              onDone={() => router.push("/employees")}
            />
          )}
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <div className="space-y-6 lg:col-span-2">
          <section className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
            <h2 className="mb-3 font-medium">Thông tin cá nhân</h2>
            <dl className="grid gap-y-2 text-sm sm:grid-cols-2">
              <Row label="Email" value={employee.email} />
              <Row label="Điện thoại" value={employee.phone || "—"} />
              <Row label="Ngày sinh" value={formatDate(employee.date_of_birth)} />
              <Row
                label="Giới tính"
                value={
                  { nam: "Nam", nu: "Nữ", khac: "Khác" }[employee.gender ?? ""] ?? "—"
                }
              />
              <Row label="Địa chỉ" value={employee.address || "—"} wide />
            </dl>
          </section>

          <section className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
            <h2 className="mb-3 font-medium">Công việc</h2>
            <dl className="grid gap-y-2 text-sm sm:grid-cols-2">
              <Row label="Phòng ban" value={employee.department_name || "—"} />
              <Row label="Chức vụ" value={employee.position_name || "—"} />
              <Row label="Cấp trên" value={employee.manager_name || "—"} />
              <Row
                label="Hình thức"
                value={WORK_MODE_LABEL[employee.work_mode]}
              />
              <Row label="Ngày vào làm" value={formatDate(employee.joined_at)} />
              <Row label="Ngày nghỉ" value={formatDate(employee.resigned_at)} />
            </dl>
          </section>

          <AccountSection
            employeeId={id}
            hasAccount={employee.has_account}
            email={employee.email}
          />
        </div>

        <div className="space-y-6">
          <AvatarSection
            employeeId={id}
            avatarUrl={employee.avatar_url}
            name={employee.full_name}
            canEdit={can("employee:update")}
          />
        </div>
      </div>
    </AppShell>
  );
}

function Row({
  label,
  value,
  wide,
}: {
  label: string;
  value: string;
  wide?: boolean;
}) {
  return (
    <div className={wide ? "sm:col-span-2" : undefined}>
      <dt className="text-neutral-500">{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

/* ------------------------------------------------------------------ avatar */

function AvatarSection({
  employeeId,
  avatarUrl,
  name,
  canEdit,
}: {
  employeeId: string;
  avatarUrl?: string;
  name: string;
  canEdit: boolean;
}) {
  const fileRef = useRef<HTMLInputElement>(null);
  const upload = useUploadAvatar(employeeId);
  const remove = useRemoveAvatar(employeeId);

  const message =
    errMsg(upload.error, "Không tải được ảnh lên") ??
    errMsg(remove.error, "Không gỡ được ảnh");

  function handlePick(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;

    // Kiểm tra ở client chỉ để báo lỗi sớm. Server vẫn kiểm tra lại bằng
    // magic bytes — Content-Type do trình duyệt khai là sửa được.
    if (file.size > 2 * 1024 * 1024) {
      alert("Ảnh vượt quá 2MB");
      e.target.value = "";
      return;
    }
    upload.mutate(file, { onSettled: () => (e.target.value = "") });
  }

  return (
    <section className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
      <h2 className="mb-3 font-medium">Ảnh đại diện</h2>

      <div className="flex flex-col items-center gap-3">
        {avatarUrl ? (
          /*
            Dùng <img> chứ không phải next/image.

            Ảnh đến từ presigned URL của Cloudflare R2 — URL có chữ ký và đổi
            mỗi giờ, nên next/image không cache được gì và còn phải cấu hình
            remotePatterns cho một tên miền động. Với ảnh đại diện 2MB đổ lại
            thì tối ưu hoá không đáng công.
          */
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={avatarUrl}
            alt={name}
            className="h-32 w-32 rounded-full object-cover"
          />
        ) : (
          <div className="flex h-32 w-32 items-center justify-center rounded-full bg-neutral-200 text-3xl font-medium text-neutral-500 dark:bg-neutral-800">
            {name.trim().slice(-1).toUpperCase() || "?"}
          </div>
        )}

        {canEdit && (
          <>
            <input
              ref={fileRef}
              type="file"
              accept="image/jpeg,image/png,image/webp"
              onChange={handlePick}
              className="hidden"
            />
            <div className="flex gap-2">
              <button
                type="button"
                disabled={upload.isPending}
                onClick={() => fileRef.current?.click()}
                className="rounded border border-neutral-300 px-3 py-1.5 text-sm hover:bg-neutral-100 disabled:opacity-50 dark:border-neutral-700 dark:hover:bg-neutral-800"
              >
                {upload.isPending ? "Đang tải..." : "Đổi ảnh"}
              </button>
              {avatarUrl && (
                <button
                  type="button"
                  disabled={remove.isPending}
                  onClick={() => remove.mutate()}
                  className="rounded border border-neutral-300 px-3 py-1.5 text-sm text-red-700 hover:bg-red-50 disabled:opacity-50 dark:border-neutral-700 dark:text-red-400 dark:hover:bg-red-950"
                >
                  Gỡ
                </button>
              )}
            </div>
            <p className="text-center text-xs text-neutral-500">
              JPEG, PNG hoặc WebP · tối đa 2MB
            </p>
          </>
        )}
      </div>

      {message && (
        <div className="mt-3">
          <FormError message={message} />
        </div>
      )}
    </section>
  );
}

/* ----------------------------------------------------- tài khoản và vai trò */

function AccountSection({
  employeeId,
  hasAccount,
  email,
}: {
  employeeId: string;
  hasAccount: boolean;
  email: string;
}) {
  const { can } = usePermission();
  const createAccount = useCreateAccount(employeeId);
  const setActive = useSetAccountActive(employeeId);
  const [tempPassword, setTempPassword] = useState<string | null>(null);

  const canCreate = can("employee:create");
  const canAssignRoles = can("role:assign");

  if (!hasAccount) {
    return (
      <section className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
        <h2 className="mb-1 font-medium">Tài khoản đăng nhập</h2>
        <p className="mb-3 text-sm text-neutral-500">
          Nhân viên này chưa có tài khoản để đăng nhập hệ thống.
        </p>

        <FormError message={errMsg(createAccount.error, "Không tạo được tài khoản")} />

        {tempPassword ? (
          <div className="mt-3 rounded border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-800 dark:bg-amber-950">
            <p className="font-medium">Đã tạo tài khoản</p>
            <p className="mt-1">
              Mật khẩu tạm:{" "}
              <code className="rounded bg-white px-1.5 py-0.5 font-mono dark:bg-neutral-900">
                {tempPassword}
              </code>
            </p>
            {/* Cảnh báo này quan trọng: mật khẩu không lưu ở đâu khác, đóng
                trang là mất. Mail cũng đã gửi nhưng có thể vào hộp thư rác. */}
            <p className="mt-2 text-xs text-amber-800 dark:text-amber-300">
              Mật khẩu chỉ hiện MỘT LẦN. Chép lại ngay nếu cần đưa tay.
              Hệ thống cũng đã gửi mail tới {email}.
            </p>
          </div>
        ) : (
          canCreate && (
            <button
              type="button"
              disabled={createAccount.isPending}
              onClick={() =>
                createAccount.mutate(undefined, {
                  onSuccess: (r) => setTempPassword(r.temp_password),
                })
              }
              className="rounded bg-neutral-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
            >
              {createAccount.isPending ? "Đang tạo..." : "Tạo tài khoản đăng nhập"}
            </button>
          )
        )}
      </section>
    );
  }

  return (
    <section className="rounded border border-neutral-200 p-4 dark:border-neutral-800">
      <h2 className="mb-3 font-medium">Tài khoản đăng nhập</h2>
      <p className="mb-4 text-sm text-neutral-500">Email đăng nhập: {email}</p>

      <FormError message={errMsg(setActive.error, "Không đổi được trạng thái")} />

      {canAssignRoles && <RoleEditor employeeId={employeeId} />}

      {canCreate && (
        <div className="mt-4 border-t border-neutral-200 pt-4 dark:border-neutral-800">
          <button
            type="button"
            disabled={setActive.isPending}
            onClick={() => setActive.mutate(false)}
            className="rounded border border-red-300 px-3 py-1.5 text-sm text-red-700 hover:bg-red-50 disabled:opacity-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950"
          >
            Vô hiệu hoá tài khoản
          </button>
          <p className="mt-1 text-xs text-neutral-500">
            Người này sẽ bị đăng xuất ngay và không đăng nhập lại được.
            Hồ sơ nhân viên vẫn giữ nguyên.
          </p>
        </div>
      )}
    </section>
  );
}

function RoleEditor({ employeeId }: { employeeId: string }) {
  const { data: allRoles = [] } = useRoles();
  const { data: current, isPending } = useEmployeeRoles(employeeId);
  const setRoles = useSetEmployeeRoles(employeeId);

  const [draft, setDraft] = useState<string[] | null>(null);
  const selected = draft ?? current?.roles ?? [];
  const dirty = draft !== null;

  if (isPending) {
    return <p className="text-sm text-neutral-500">Đang tải vai trò...</p>;
  }

  function toggle(code: string) {
    setDraft(
      selected.includes(code)
        ? selected.filter((c) => c !== code)
        : [...selected, code],
    );
  }

  return (
    <div>
      <h3 className="mb-2 text-sm font-medium">Vai trò</h3>

      <FormError message={errMsg(setRoles.error, "Không đổi được vai trò")} />

      <div className="space-y-2">
        {allRoles.map((role) => (
          <label
            key={role.code}
            className="flex cursor-pointer items-start gap-2 text-sm"
          >
            <input
              type="checkbox"
              checked={selected.includes(role.code)}
              onChange={() => toggle(role.code)}
              className="mt-1"
            />
            <span>
              <span className="font-medium">{role.name}</span>
              <span className="ml-2 text-xs text-neutral-500">
                phạm vi: {SCOPE_LABEL[role.scope] ?? role.scope}
              </span>
              {role.description && (
                <span className="block text-xs text-neutral-500">
                  {role.description}
                </span>
              )}
            </span>
          </label>
        ))}
      </div>

      {dirty && (
        <div className="mt-3 flex items-center gap-2">
          <button
            type="button"
            disabled={setRoles.isPending || selected.length === 0}
            onClick={() => setRoles.mutate(selected, { onSuccess: () => setDraft(null) })}
            className="rounded bg-neutral-900 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-50 dark:bg-white dark:text-neutral-900"
          >
            {setRoles.isPending ? "Đang lưu..." : "Lưu vai trò"}
          </button>
          <button
            type="button"
            onClick={() => setDraft(null)}
            className="rounded border border-neutral-300 px-3 py-1.5 text-sm dark:border-neutral-700"
          >
            Huỷ
          </button>
          {/* Đổi vai trò cắt phiên của người đó — nói trước để người quản trị
              không bất ngờ khi nhân viên báo bị đăng xuất. */}
          <span className="text-xs text-neutral-500">
            Người này sẽ bị đăng xuất để quyền mới có hiệu lực ngay.
          </span>
        </div>
      )}
    </div>
  );
}

/* ------------------------------------------------------------ vô hiệu hoá */

function DeactivateButton({
  employeeId,
  name,
  onDone,
}: {
  employeeId: string;
  name: string;
  onDone: () => void;
}) {
  const deactivate = useDeactivateEmployee();
  const [confirming, setConfirming] = useState(false);

  if (!confirming) {
    return (
      <button
        type="button"
        onClick={() => setConfirming(true)}
        className="rounded border border-red-300 px-3 py-1.5 text-sm text-red-700 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950"
      >
        Vô hiệu hoá
      </button>
    );
  }

  return (
    <div className="flex items-center gap-2 rounded border border-red-300 bg-red-50 px-3 py-1.5 text-sm dark:border-red-800 dark:bg-red-950">
      <span>Vô hiệu hoá {name}?</span>
      <button
        type="button"
        disabled={deactivate.isPending}
        onClick={() => deactivate.mutate(employeeId, { onSuccess: onDone })}
        className="font-medium text-red-700 underline disabled:opacity-50 dark:text-red-400"
      >
        {deactivate.isPending ? "Đang xử lý..." : "Xác nhận"}
      </button>
      <button type="button" onClick={() => setConfirming(false)}>
        Huỷ
      </button>
    </div>
  );
}
