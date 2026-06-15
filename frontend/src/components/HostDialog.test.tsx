import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { HostDialog } from "./HostDialog";

// HostPluginsSection fetches on mount; stub it out so this test focuses on the
// host form payload.
vi.mock("./HostPluginsSection", () => ({
  HostPluginsSection: () => null,
}));

describe("HostDialog", () => {
  beforeEach(() => vi.clearAllMocks());

  it("submits joined domains and upstreams for a new host", async () => {
    const onSubmit = vi.fn();
    render(
      <HostDialog
        open
        onOpenChange={() => {}}
        host={null}
        submitting={false}
        onSubmit={onSubmit}
      />
    );

    const domainInputs = screen.getAllByPlaceholderText("example.com");
    fireEvent.change(domainInputs[0], { target: { value: "a.com" } });

    const upstreamInputs = screen.getAllByPlaceholderText("127.0.0.1:8080");
    fireEvent.change(upstreamInputs[0], { target: { value: "127.0.0.1:9000" } });

    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit).toHaveBeenCalledWith({
      matcher: "",
      domains: "a.com",
      upstreams: [{ backend: "127.0.0.1:9000" }],
    });
  });

  it("requires at least one domain and upstream", async () => {
    const onSubmit = vi.fn();
    render(
      <HostDialog
        open
        onOpenChange={() => {}}
        host={null}
        submitting={false}
        onSubmit={onSubmit}
      />
    );
    // Submit with empty fields -> zod validation blocks submission.
    fireEvent.click(screen.getByRole("button", { name: /^save$/i }));
    await waitFor(() =>
      expect(screen.getByText(/at least one valid domain/i)).toBeInTheDocument()
    );
    expect(onSubmit).not.toHaveBeenCalled();
  });
});
