import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { Settings2, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { api, unwrap, type ApiEnvelope } from "@/lib/api";
import type {
  CaddyModule,
  CaddyModulesResponse,
  ModuleConfig,
  PluginSchema,
} from "@/lib/types";
import {
  PluginDialog,
  type ModuleConfigPayload,
} from "@/components/PluginDialog";

export function PluginsPage() {
  const [plugins, setPlugins] = useState<CaddyModule[]>([]);
  const [schemas, setSchemas] = useState<PluginSchema[]>([]);
  const [configs, setConfigs] = useState<ModuleConfig[]>([]);
  const [loading, setLoading] = useState(true);
  const [unavailable, setUnavailable] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [selected, setSelected] = useState<CaddyModule | null>(null);

  const schemaByModule = useMemo(() => {
    const map = new Map<string, PluginSchema>();
    for (const s of schemas) map.set(s.moduleId, s);
    return map;
  }, [schemas]);

  const configByModule = useMemo(() => {
    const map = new Map<string, ModuleConfig>();
    for (const c of configs) map.set(c.moduleId, c);
    return map;
  }, [configs]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [mods, schemaRes] = await Promise.all([
        api.get<ApiEnvelope<CaddyModulesResponse>>("/caddy/modules"),
        api.get<ApiEnvelope<PluginSchema[]>>("/caddy/schemas"),
      ]);
      setPlugins(unwrap(mods.data)?.plugins ?? []);
      setSchemas(unwrap(schemaRes.data) ?? []);
    } catch {
      toast.error("Failed to detect Caddy modules");
    }

    // Module configs require API mode; treat a failure as "unavailable".
    try {
      const cfg = await api.get<ApiEnvelope<ModuleConfig[]>>(
        "/caddy/module-configs"
      );
      setConfigs(unwrap(cfg.data) ?? []);
      setUnavailable(false);
    } catch {
      setConfigs([]);
      setUnavailable(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const openConfigure = (module: CaddyModule) => {
    setSelected(module);
    setDialogOpen(true);
  };

  const saveConfig = async (payload: ModuleConfigPayload) => {
    setSubmitting(true);
    try {
      await api.put("/caddy/module-configs", payload);
      toast.success("Module configuration saved");
      setDialogOpen(false);
      load();
    } catch (err) {
      const msg =
        (err as { response?: { data?: { error?: { message?: string } } } })
          .response?.data?.error?.message ?? "Failed to save configuration";
      toast.error(msg);
    } finally {
      setSubmitting(false);
    }
  };

  const deleteConfig = async (id: number) => {
    try {
      await api.delete(`/caddy/module-configs/${id}`);
      toast.success("Configuration removed");
      load();
    } catch {
      toast.error("Failed to remove configuration");
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Plugins</h1>
        <p className="text-sm text-muted-foreground">
          Modules detected in your Caddy build. Configure global plugin settings
          here; per-host plugins (e.g. authentication) are set in the host editor.
        </p>
      </div>

      {unavailable && (
        <div className="rounded-lg border border-border bg-muted/40 px-4 py-3 text-sm text-muted-foreground">
          Module configuration requires Caddy API mode (CPM_CADDY_MODE=api).
          Detected plugins are shown read-only.
        </div>
      )}

      <div className="rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Module</TableHead>
              <TableHead>Package</TableHead>
              <TableHead className="w-28">Configurable</TableHead>
              <TableHead className="w-24">Status</TableHead>
              <TableHead className="w-28 text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">
                  Loading…
                </TableCell>
              </TableRow>
            ) : plugins.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground">
                  No plugins detected in this Caddy build.
                </TableCell>
              </TableRow>
            ) : (
              plugins.map((module) => {
                const cfg = configByModule.get(module.id);
                const typed = schemaByModule.has(module.id);
                return (
                  <TableRow key={module.id}>
                    <TableCell className="font-medium">{module.id}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {module.package || "—"}
                    </TableCell>
                    <TableCell>{typed ? "Form" : "Raw JSON"}</TableCell>
                    <TableCell>
                      {cfg ? (cfg.enabled ? "Enabled" : "Disabled") : "—"}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          disabled={unavailable}
                          onClick={() => openConfigure(module)}
                        >
                          <Settings2 className="size-4" />
                        </Button>
                        {cfg && (
                          <Button
                            variant="ghost"
                            size="icon"
                            disabled={unavailable}
                            onClick={() => deleteConfig(cfg.ID)}
                          >
                            <Trash2 className="size-4" />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })
            )}
          </TableBody>
        </Table>
      </div>

      <PluginDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        module={selected}
        schema={selected ? schemaByModule.get(selected.id) ?? null : null}
        existing={selected ? configByModule.get(selected.id) ?? null : null}
        submitting={submitting}
        onSubmit={saveConfig}
      />
    </div>
  );
}
