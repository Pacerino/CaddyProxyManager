import { describe, it, expect } from "vitest";
import { unwrap, type ApiEnvelope } from "./api";

describe("unwrap", () => {
  it("returns the result field", () => {
    const env: ApiEnvelope<{ a: number }> = { result: { a: 1 } };
    expect(unwrap(env)).toEqual({ a: 1 });
  });

  it("passes arrays through", () => {
    const env: ApiEnvelope<number[]> = { result: [1, 2, 3] };
    expect(unwrap(env)).toEqual([1, 2, 3]);
  });
});
