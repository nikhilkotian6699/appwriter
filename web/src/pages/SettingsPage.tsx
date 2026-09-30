import { useEffect, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, call } from "../api/client";
import { keys, useSettings } from "../api/hooks";
import { Button, ErrorBanner, Field, Input, PageHeader, Spinner } from "../components/ui";

export default function SettingsPage() {
  const settings = useSettings();
  const qc = useQueryClient();
  const [limit, setLimit] = useState(6000);
  const [minutes, setMinutes] = useState(10);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    if (settings.data) {
      setLimit(settings.data.scene_token_limit);
      setMinutes(settings.data.autosave_snapshot_minutes);
    }
  }, [settings.data]);

  const save = useMutation({
    mutationFn: () => call(api.PUT("/api/settings", { body: { scene_token_limit: limit, autosave_snapshot_minutes: minutes } })),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.settings });
      setSaved(true);
      setTimeout(() => setSaved(false), 2000);
    },
  });

  return (
    <div className="max-w-xl">
      <PageHeader title="Settings" subtitle="These apply to your workspace only." />
      <ErrorBanner error={settings.error} onRetry={() => settings.refetch()} />
      {settings.isLoading && <Spinner />}
      {settings.data && (
        <form
          className="space-y-4 rounded-lg border border-stone-200 bg-white p-5 shadow-sm"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label="Scene token limit" help="Chapters estimated above this many tokens are split into scenes at scene breaks and headings before the writers read them.">
            <Input type="number" min={500} max={200000} value={limit} onChange={(e) => setLimit(Number(e.target.value))} />
          </Field>
          <Field label="Autosave snapshot interval (minutes)" help="While you write, the editor saves continuously and keeps a snapshot at most this often.">
            <Input type="number" min={1} max={1440} value={minutes} onChange={(e) => setMinutes(Number(e.target.value))} />
          </Field>
          <ErrorBanner error={save.error} />
          <div className="flex items-center gap-3">
            <Button variant="primary" type="submit" loading={save.isPending}>
              Save settings
            </Button>
            {saved && <span className="text-sm text-green-700">Saved.</span>}
          </div>
        </form>
      )}
    </div>
  );
}
