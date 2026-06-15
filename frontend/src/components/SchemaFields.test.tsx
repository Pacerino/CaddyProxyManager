import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import {
  SchemaFields,
  defaultFieldValues,
  type FieldValues,
} from "./SchemaFields";
import type { SchemaField } from "@/lib/types";

const fields: SchemaField[] = [
  { key: "username", label: "Username", type: "string", required: true },
  { key: "password", label: "Password", type: "secret", required: true },
  { key: "enabled", label: "Enabled", type: "bool", default: true },
];

describe("defaultFieldValues", () => {
  it("seeds strings empty and bools from default", () => {
    const v = defaultFieldValues(fields);
    expect(v.username).toBe("");
    expect(v.password).toBe("");
    expect(v.enabled).toBe(true);
  });
});

describe("SchemaFields", () => {
  it("renders inputs with correct types and required markers", () => {
    render(
      <SchemaFields fields={fields} values={defaultFieldValues(fields)} onChange={() => {}} />
    );
    // secret -> password input
    const pw = screen.getByLabelText(/Password/i) as HTMLInputElement;
    expect(pw.type).toBe("password");
    // bool -> checkbox
    expect(screen.getByRole("checkbox")).toBeInTheDocument();
  });

  it("emits changes for text and checkbox fields", () => {
    const onChange = vi.fn();
    const values: FieldValues = defaultFieldValues(fields);
    render(<SchemaFields fields={fields} values={values} onChange={onChange} />);

    fireEvent.change(screen.getByLabelText(/Username/i), {
      target: { value: "bob" },
    });
    expect(onChange).toHaveBeenLastCalledWith({ ...values, username: "bob" });

    fireEvent.click(screen.getByRole("checkbox"));
    expect(onChange).toHaveBeenLastCalledWith({ ...values, enabled: false });
  });
});
