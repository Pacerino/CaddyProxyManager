import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  SchemaFields,
  defaultFieldValues,
  type FieldValues,
} from "@/components/SchemaFields";
import { api, unwrap, type ApiEnvelope } from "@/lib/api";
import type {
  CaddyModulesResponse,
  Host,
  HostPlugin,
  PluginSchema,
} from "@/lib/types";

interface HostPluginsSectionProps {
  host: Host;
}

// HostPluginsSection lets the user configure per-host plugins (e.g. basic
// auth) inside the host editor. It only offers plugins whose module is present
// in the running Caddy build and that declare host scope.
export function HostPluginsSection({ host }: HostPluginsSectionProps) {
  const [schemas, setSchemas] = useState<PluginSchema[]>([]);
  const [available, setAvailable] = useState<Set<string>>(new Set());
  const [configured, setConfigured] = useState<HostPlugin[]>(host.Plugins ?? []);
  const [selected, setSelected] = useState<string>("");
  const [values, setValues] = useState<FieldValues>({});
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    try {
      const [schemaRes, modRes] = await Promise.all([
        api.get<ApiEnvelope<PluginSchema[]>>("/caddy/host-schemas"),
        api.get<ApiEnvelope<CaddyModulesResponse>>("/caddy/modules"),
      ]);
      setSchemas(unwrap(schemaRes.data) ?? []);
      const mods = unwrap(modRes.data);
      const ids = new Set<string>(
        (mods?.modules ?? []).map((m) => m.id)
      );
      setAvailable(ids);
    } catch {
      // Non-fatal: section simply shows nothing configurable.
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // Schemas usable for this host: host-scoped AND present in the build.
  const usable = useMemo(
    () => schemas.filter((s) => available.has(s.moduleId)),
    [schemas, available]
  );

  const selectedSchema = usable.find((s) => s.moduleId === selected) ?? null;

  useEffect(() => {
    if (selectedSchema) setValues(defaultFieldValues(selectedSchema.fields));
  }, [selectedSchema]);

  const refreshConfigured = useCallback(async () => {
    try {
      const res = await api.get<ApiEnvelope<Host>>(`/hosts/${host.ID}`);
      setConfigured(unwrap(res.data)?.Plugins ?? []);
    } catch {
      /* ignore */
    }
  }, [host.ID]);

  const save = async () => {
    if (!selectedSchema) return;
    setSaving(true);
    try {
      await api.put(`/hosts/${host.ID}/plugins`, {
        moduleId: selectedSchema.moduleId,
        values,
      });
      toast.success("Plugin configured");
      setSelected("");
      refreshConfigured();
    } catch (err) {
      const msg =
        (err as { response?: { data?: { error?: { message?: string } } } })
          .response?.data?.error?.message ?? "Failed to configure plugin";
      toast.error(msg);
    } finally {
      setSaving(false);
    }
  };

  const remove = async (pluginID: number) => {
    try {
      await api.delete(`/hosts/${host.ID}/plugins/${pluginID}`);
      toast.success("Plugin removed");
      refreshConfigured();
    } catch {
      toast.error("Failed to remove plugin");
    }
  };

  if (usable.length === 0) {
    return (
      <div className="space-y-2 border-t border-border pt-4">
        <p className="text-sm font-medium">Plugins</p>
        <p className="text-xs text-muted-foreground">
          No host-configurable plugins are available in this Caddy build.
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-3 border-t border-border pt-4">
      <p className="text-sm font-medium">Plugins</p>

      {configured.length > 0 && (
        <div className="space-y-1">
          {configured.map((p) => (
            <div
              key={p.ID}
              className="flex items-center justify-between rounded-md bg-muted/40 px-3 py-1.5 text-sm"
            >
              <span>
                {schemas.find((s) => s.moduleId === p.moduleId)?.title ??
                  p.moduleId}
                {!p.enabled && " (disabled)"}
              </span>
              <Button
                type="button"
                variant="ghost"
                size="icon"
                onClick={() => remove(p.ID)}
              >
                <Trash2 className="size-4" />
              </Button>
            </div>
          ))}
        </div>
      )}

      <div className="space-y-2">
        <select
          className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring"
          value={selected}
          onChange={(e) => setSelected(e.target.value)}
        >
          <option value="">Add a plugin…</option>
          {usable.map((s) => (
            <option key={s.moduleId} value={s.moduleId}>
              {s.title}
            </option>
          ))}
        </select>

        {selectedSchema && (
          <div className="space-y-4 rounded-md border border-border p-3">
            {selectedSchema.description && (
              <p className="text-xs text-muted-foreground">
                {selectedSchema.description}
              </p>
            )}
            <SchemaFields
              fields={selectedSchema.fields}
              values={values}
              onChange={setValues}
              idPrefix="hostplugin-"
            />
            <Button type="button" size="sm" onClick={save} disabled={saving}>
              {saving ? "Saving…" : "Save plugin"}
            </Button>
          </div>
        )}
      </div>
    </div>
  );
}
