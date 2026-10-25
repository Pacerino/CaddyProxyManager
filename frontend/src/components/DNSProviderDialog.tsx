import { useEffect, useState } from "react";
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
import type { DNSProvider, DNSProviderType, SchemaField } from "@/lib/types";

// Payload sent to POST/PUT /caddy/dns-providers.
export interface DNSProviderPayload {
  name: string;
  moduleId: string;
  values: Record<string, unknown>;
}

interface DNSProviderDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  types: DNSProviderType[];
  provider: DNSProvider | null;
  submitting: boolean;
  onSubmit: (payload: DNSProviderPayload) => void;
}

const selectClass =
  "flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring";

// Seeds form values from a stored credential. Secrets stay blank.
function seedValues(
  fields: SchemaField[],
  stored: Record<string, unknown>
): FieldValues {
  const out = defaultFieldValues(fields);
  for (const f of fields) {
    const v = stored[f.key];
    if (v == null || f.type === "secret") continue;
    if (f.type === "bool") out[f.key] = Boolean(v);
    else if (f.type === "stringlist")
      out[f.key] = Array.isArray(v) ? v.map(String) : [String(v)];
    else if (f.type === "json") out[f.key] = JSON.stringify(v, null, 2);
    else out[f.key] = String(v);
  }
  return out;
}

// Converts form values to the API shape; throws a user-facing Error on bad
// input. Empty optional values are omitted. A secret that is blank is sent as
// "" only when a value is already stored, which tells the backend to keep it.
function buildValues(
  fields: SchemaField[],
  values: FieldValues,
  secretsSet: string[]
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields) {
    const v = values[f.key];
    if (f.type === "bool") {
      out[f.key] = Boolean(v);
    } else if (f.type === "stringlist") {
      if (Array.isArray(v) && v.length > 0) out[f.key] = v;
    } else if (f.type === "secret") {
      const s = String(v ?? "");
      if (s !== "" || secretsSet.includes(f.key)) out[f.key] = s;
    } else {
      const s = String(v ?? "").trim();
      if (s === "") continue;
      if (f.type === "int") {
        const n = Number(s);
        if (!Number.isInteger(n)) throw new Error(`${f.label} must be a whole number`);
        out[f.key] = n;
      } else if (f.type === "json") {
        try {
          out[f.key] = JSON.parse(s);
        } catch {
          throw new Error(`${f.label} must be valid JSON`);
        }
      } else {
        out[f.key] = s;
      }
    }
  }
  return out;
}

export function DNSProviderDialog({
  open,
  onOpenChange,
  types,
  provider,
  submitting,
  onSubmit,
}: DNSProviderDialogProps) {
  const [name, setName] = useState("");
  const [moduleId, setModuleId] = useState("");
  const [values, setValues] = useState<FieldValues>({});
  const [rawValues, setRawValues] = useState("{}");
  const [error, setError] = useState<string | null>(null);

  const type = types.find((t) => t.moduleId === moduleId) ?? null;
  const secretsSet = provider && provider.moduleId === moduleId ? provider.secretsSet : [];

  // Seeds the values for a type, from the stored credential when it is of the
  // same type.
  const seed = (t: DNSProviderType | null, existing: DNSProvider | null) => {
    const stored = existing && t && existing.moduleId === t.moduleId ? existing : null;
    if (t?.source === "none") {
      // Stored secrets are not returned; list their keys blank so an
      // unchanged secret is preserved by the backend.
      const raw: Record<string, unknown> = { ...(stored?.values ?? {}) };
      for (const key of stored?.secretsSet ?? []) raw[key] = "";
      setRawValues(JSON.stringify(raw, null, 2));
    } else {
      setValues(seedValues(t?.fields ?? [], stored?.values ?? {}));
    }
  };

  // Reseed whenever the dialog opens for a (different) credential.
  useEffect(() => {
    if (!open) return;
    const initial = provider
      ? types.find((t) => t.moduleId === provider.moduleId) ?? null
      : types[0] ?? null;
    setName(provider?.name ?? "");
    setModuleId(provider?.moduleId ?? initial?.moduleId ?? "");
    setError(null);
    seed(initial, provider);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, provider, types]);

  const changeType = (id: string) => {
    setModuleId(id);
    setError(null);
    seed(types.find((t) => t.moduleId === id) ?? null, provider);
  };

  const submit = () => {
    if (!name.trim()) return setError("A name is required");
    if (!type) return setError("Choose a provider type");
    try {
      if (type.source === "none") {
        const parsed = JSON.parse(rawValues);
        if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed))
          return setError("Values must be a JSON object");
        onSubmit({ name: name.trim(), moduleId, values: parsed });
        return;
      }
      onSubmit({
        name: name.trim(),
        moduleId,
        values: buildValues(type.fields, values, secretsSet),
      });
    } catch (e) {
      setError(e instanceof SyntaxError ? "Values must be valid JSON" : (e as Error).message);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{provider ? "Edit DNS provider" : "Add DNS provider"}</DialogTitle>
          <DialogDescription>
            {type?.description ??
              "Credentials Caddy uses to solve DNS-01 ACME challenges for hosts."}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-5">
          <div className="space-y-2">
            <Label htmlFor="dns-name">Name</Label>
            <Input
              id="dns-name"
              placeholder="Cloudflare (production)"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="dns-type">Provider type</Label>
            <select
              id="dns-type"
              className={selectClass}
              value={moduleId}
              onChange={(e) => changeType(e.target.value)}
            >
              {types.map((t) => (
                <option key={t.moduleId} value={t.moduleId}>
                  {t.title}
                </option>
              ))}
            </select>
          </div>

          {type?.source === "none" ? (
            <div className="space-y-2">
              <Label htmlFor="dns-raw">Values (JSON)</Label>
              <textarea
                id="dns-raw"
                className="flex min-h-40 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring"
                value={rawValues}
                onChange={(e) => setRawValues(e.target.value)}
                spellCheck={false}
              />
              <p className="text-xs text-muted-foreground">
                No schema is known for this provider. Secrets already stored
                are listed with an empty value and kept unless you change them.
              </p>
            </div>
          ) : (
            type && (
              <SchemaFields
                fields={type.fields}
                values={values}
                onChange={setValues}
                idPrefix="dns-"
                secretsSet={secretsSet}
              />
            )
          )}

          {error && <p className="text-sm text-destructive">{error}</p>}
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
