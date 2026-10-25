import { useEffect, useState } from "react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { SchemaField } from "@/lib/types";

export type FieldValue = string | boolean | string[];
export type FieldValues = Record<string, FieldValue>;

export function defaultFieldValues(fields: SchemaField[]): FieldValues {
  const seeded: FieldValues = {};
  for (const f of fields) {
    if (f.type === "bool") seeded[f.key] = Boolean(f.default ?? false);
    else if (f.type === "stringlist")
      seeded[f.key] = Array.isArray(f.default) ? f.default.map(String) : [];
    else seeded[f.key] = f.default != null ? String(f.default) : "";
  }
  return seeded;
}

const parseList = (text: string) =>
  text
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

// StringListInput edits a string[] as one comma-separated input. It keeps the
// raw text locally so separators can be typed, and only re-syncs from the
// value when it changed from outside.
function StringListInput({
  id,
  value,
  placeholder,
  onChange,
}: {
  id: string;
  value: string[];
  placeholder?: string;
  onChange: (value: string[]) => void;
}) {
  const [text, setText] = useState(value.join(", "));
  useEffect(() => {
    setText((current) =>
      parseList(current).join(",") === value.join(",")
        ? current
        : value.join(", ")
    );
  }, [value]);

  return (
    <Input
      id={id}
      placeholder={placeholder}
      value={text}
      onChange={(e) => {
        setText(e.target.value);
        onChange(parseList(e.target.value));
      }}
    />
  );
}

interface SchemaFieldsProps {
  fields: SchemaField[];
  values: FieldValues;
  onChange: (values: FieldValues) => void;
  idPrefix?: string;
  // Secret keys that already have a stored value; their inputs are left blank
  // with an "unchanged" placeholder.
  secretsSet?: string[];
}

// SchemaFields renders typed inputs for a plugin schema's fields. Shared by
// the global Plugins dialog, the per-host plugin editor and the DNS provider
// dialog.
export function SchemaFields({
  fields,
  values,
  onChange,
  idPrefix = "",
  secretsSet = [],
}: SchemaFieldsProps) {
  const set = (key: string, value: FieldValue) =>
    onChange({ ...values, [key]: value });

  return (
    <>
      {fields.map((field) => {
        const id = `${idPrefix}${field.key}`;
        if (field.type === "bool") {
          return (
            <label key={field.key} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={Boolean(values[field.key])}
                onChange={(e) => set(field.key, e.target.checked)}
              />
              {field.label}
            </label>
          );
        }
        const current = values[field.key];
        const placeholder =
          field.type === "secret" && secretsSet.includes(field.key)
            ? "•••••• (unchanged)"
            : field.placeholder;
        return (
          <div key={field.key} className="space-y-2">
            <Label htmlFor={id}>
              {field.label}
              {field.required && <span className="text-destructive"> *</span>}
            </Label>
            {field.type === "stringlist" ? (
              <StringListInput
                id={id}
                placeholder={placeholder}
                value={Array.isArray(current) ? current : []}
                onChange={(v) => set(field.key, v)}
              />
            ) : field.type === "json" ? (
              <textarea
                id={id}
                className="flex min-h-24 w-full rounded-md border border-input bg-transparent px-3 py-2 font-mono text-sm shadow-sm outline-none focus-visible:ring-1 focus-visible:ring-ring"
                placeholder={placeholder}
                value={String(current ?? "")}
                onChange={(e) => set(field.key, e.target.value)}
                spellCheck={false}
              />
            ) : (
              <Input
                id={id}
                type={field.type === "secret" ? "password" : "text"}
                placeholder={placeholder}
                value={String(current ?? "")}
                onChange={(e) => set(field.key, e.target.value)}
              />
            )}
            {field.description && (
              <p className="text-xs text-muted-foreground">{field.description}</p>
            )}
          </div>
        );
      })}
    </>
  );
}
