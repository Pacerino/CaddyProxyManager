import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  SchemaFields,
  defaultFieldValues,
  type FieldValues,
} from "@/components/SchemaFields";
import type { CaddyModule, ModuleConfig, PluginSchema } from "@/lib/types";

// Payload sent to PUT /caddy/module-configs.
export interface ModuleConfigPayload {
  moduleId: string;
  path?: string;
  enabled: boolean;
  // Either typed form values (when a schema exists) or a raw JSON config.
  values?: Record<string, unknown>;
  config?: unknown;
}

interface PluginDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  module: CaddyModule | null;
  schema: PluginSchema | null;
  existing: ModuleConfig | null;
  submitting: boolean;
  onSubmit: (payload: ModuleConfigPayload) => void;
}

export function PluginDialog({
  open,
  onOpenChange,
  module,
  schema,
  existing,
  submitting,
  onSubmit,
}: PluginDialogProps) {
  const [values, setValues] = useState<FieldValues>({});
  const [rawConfig, setRawConfig] = useState("{}");
  const [rawPath, setRawPath] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [jsonError, setJsonError] = useState<string | null>(null);

  const moduleId = module?.id ?? existing?.moduleId ?? "";

  // Seed the form whenever the dialog opens for a (different) module.
  useEffect(() => {
    if (!open) return;
    setEnabled(existing?.enabled ?? true);
    setJsonError(null);

    if (schema) {
      // Secrets are never returned by the API, so they start blank.
      setValues(defaultFieldValues(schema.fields));
    } else {
      setRawPath(existing?.path ?? "");
      setRawConfig(
        existing?.config != null
          ? JSON.stringify(existing.config, null, 2)
          : "{}"
      );
    }
  }, [open, schema, existing]);

  const title = useMemo(() => {
    if (schema) return `Configure ${schema.title}`;
    return `Configure ${moduleId}`;
  }, [schema, moduleId]);

  const submit = () => {
    if (schema) {
      onSubmit({ moduleId, enabled, values });
      return;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(rawConfig);
    } catch {
      setJsonError("Config must be valid JSON");
      return;
    }
    if (!rawPath.trim()) {
      setJsonError("A config path is required");
      return;
    }
    onSubmit({ moduleId, enabled, path: rawPath.trim(), config: parsed });
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {schema?.description ??
              "No typed schema is available for this module — provide a raw JSON config fragment that will be applied to Caddy via its admin API."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-5">
          {schema ? (
            <SchemaFields
              fields={schema.fields}
              values={values}
              onChange={setValues}
            />
          ) : (
            <>
              <div className="space-y-2">
                <Label htmlFor="config-path">Config path</Label>
                <Input
                  id="config-path"
                  placeholder="apps/http/servers/srv0"
                  value={rawPath}
                  onChange={(e) => setRawPath(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">
                  Path within Caddy's config (relative to /config/).
                </p>
              </div>
              <div className="space-y-2">
                <Label htmlFor="config-json">Config (JSON)</Label>
                <textarea
                  id="config-json"
                  className="flex min-h-40 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  value={rawConfig}
                  onChange={(e) => setRawConfig(e.target.value)}
                  spellCheck={false}
                />
              </div>
            </>
          )}

          <label className="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
            />
            Enabled
          </label>

          {jsonError && <p className="text-sm text-destructive">{jsonError}</p>}
        </div>

        <DialogFooter>
          <Button type="button" onClick={submit} disabled={submitting}>
            {submitting ? "Saving…" : "Save"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
