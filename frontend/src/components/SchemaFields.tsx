import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { SchemaField } from "@/lib/types";

export type FieldValues = Record<string, string | boolean>;

export function defaultFieldValues(fields: SchemaField[]): FieldValues {
  const seeded: FieldValues = {};
  for (const f of fields) {
    if (f.type === "bool") seeded[f.key] = Boolean(f.default ?? false);
    else seeded[f.key] = f.default != null ? String(f.default) : "";
  }
  return seeded;
}

interface SchemaFieldsProps {
  fields: SchemaField[];
  values: FieldValues;
  onChange: (values: FieldValues) => void;
  idPrefix?: string;
}

// SchemaFields renders typed inputs for a plugin schema's fields. Shared by
// the global Plugins dialog and the per-host plugin editor.
export function SchemaFields({
  fields,
  values,
  onChange,
  idPrefix = "",
}: SchemaFieldsProps) {
  const set = (key: string, value: string | boolean) =>
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
        return (
          <div key={field.key} className="space-y-2">
            <Label htmlFor={id}>
              {field.label}
              {field.required && <span className="text-destructive"> *</span>}
            </Label>
            <Input
              id={id}
              type={field.type === "secret" ? "password" : "text"}
              placeholder={field.placeholder}
              value={String(values[field.key] ?? "")}
              onChange={(e) => set(field.key, e.target.value)}
            />
            {field.description && (
              <p className="text-xs text-muted-foreground">{field.description}</p>
            )}
          </div>
        );
      })}
    </>
  );
}
