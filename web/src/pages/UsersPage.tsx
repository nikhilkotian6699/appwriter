import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call, type UserRole } from "../api/client";
import { keys, useMe, useUsers } from "../api/hooks";
import { Badge, Button, ErrorBanner, Field, Input, PageHeader, Select, Spinner } from "../components/ui";
import { fmtDateTime } from "../lib/format";

/** UsersPage lets an admin see every account and add one; there is no sign-up. */
export default function UsersPage() {
  const me = useMe();
  const isAdmin = me.data?.user.role === "admin";
  const users = useUsers(isAdmin);
  const qc = useQueryClient();
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<UserRole>("author");
  const [created, setCreated] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => call(api.POST("/api/users", { body: { username: username.trim(), display_name: displayName.trim() || undefined, password, role } })),
    onSuccess: (u) => {
      qc.invalidateQueries({ queryKey: keys.users });
      setCreated(u.username);
      setUsername("");
      setDisplayName("");
      setPassword("");
      setRole("author");
    },
  });

  if (me.data && !isAdmin) {
    return (
      <div>
        <PageHeader title="Users" />
        <p className="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900">Only an admin can see the accounts. Your own account is on the Account page.</p>
      </div>
    );
  }
  return (
    <div>
      <PageHeader title="Users" subtitle="Every account is a private workspace: you can see who has one and what it uses, never their manuscripts." />
      <div className="grid gap-6 lg:grid-cols-[1fr_22rem]">
        <div>
          <ErrorBanner error={users.error} onRetry={() => users.refetch()} />
          {users.isLoading && <Spinner />}
          {users.data && (
            <div className="overflow-x-auto rounded-lg border border-stone-200 bg-white shadow-sm">
              <table className="w-full text-sm">
                <thead className="bg-stone-50 text-left text-xs uppercase tracking-wide text-stone-500">
                  <tr>
                    <th className="px-3 py-2 font-medium">Username</th>
                    <th className="px-3 py-2 font-medium">Display name</th>
                    <th className="px-3 py-2 font-medium">Role</th>
                    <th className="px-3 py-2 font-medium">Created</th>
                  </tr>
                </thead>
                <tbody>
                  {users.data.map((u) => (
                    <tr key={u.id} className="border-t border-stone-100">
                      <td className="px-3 py-2 font-medium text-stone-900">
                        {u.username}
                        {u.id === me.data?.user.id && <span className="ml-2 text-xs text-stone-400">(you)</span>}
                      </td>
                      <td className="px-3 py-2 text-stone-700">{u.display_name || <span className="text-stone-400">—</span>}</td>
                      <td className="px-3 py-2">
                        <Badge tone={u.role === "admin" ? "amber" : "stone"}>{u.role}</Badge>
                      </td>
                      <td className="px-3 py-2 text-stone-500">{fmtDateTime(u.created_at)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
        <form
          className="h-fit space-y-3 rounded-lg border border-stone-200 bg-white p-4 shadow-sm"
          onSubmit={(e) => {
            e.preventDefault();
            if (!create.isPending) create.mutate();
          }}
        >
          <h2 className="font-semibold text-stone-900">Add an account</h2>
          <p className="text-xs text-stone-500">The new account starts with its own settings, the example writers and the system agents. Give the person the first password; they can change it on their Account page.</p>
          <Field label="Username" help="Lower-case, 3 to 32 characters: letters, digits, dots, hyphens, underscores.">
            <Input value={username} onChange={(e) => setUsername(e.target.value.toLowerCase())} autoCapitalize="none" required minLength={3} maxLength={32} />
          </Field>
          <Field label="Display name (optional)">
            <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} maxLength={120} />
          </Field>
          <Field label="First password" help="At least 8 characters.">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" required minLength={8} maxLength={72} />
          </Field>
          <Field label="Role" help="Admins add and manage accounts; authors only see their own workspace.">
            <Select value={role} onChange={(e) => setRole(e.target.value as UserRole)}>
              <option value="author">author</option>
              <option value="admin">admin</option>
            </Select>
          </Field>
          <ErrorBanner error={create.error} />
          {created && <p className="text-sm text-green-700">Account {created} was added.</p>}
          <Button variant="primary" type="submit" loading={create.isPending} disabled={username.trim().length < 3 || password.length < 8}>
            Add account
          </Button>
        </form>
      </div>
    </div>
  );
}
