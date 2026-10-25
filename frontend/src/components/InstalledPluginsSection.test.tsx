import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { render, screen, fireEvent, waitFor, act } from "@testing-library/react";
import { InstalledPluginsSection } from "./InstalledPluginsSection";
import { AddPluginDialog, CADDY_UPDATE_NOTE } from "./AddPluginDialog";
import { PluginOperationProgress } from "./PluginOperationProgress";
import { api } from "@/lib/api";
import type { PluginOperation, PluginsState } from "@/lib/types";

vi.mock("@/lib/api", async (orig) => ({
  ...(await orig<typeof import("@/lib/api")>()),
  api: { get: vi.fn(), put: vi.fn(), post: vi.fn(), delete: vi.fn() },
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

const get = api.get as unknown as ReturnType<typeof vi.fn>;
const post = api.post as unknown as ReturnType<typeof vi.fn>;
const del = api.delete as unknown as ReturnType<typeof vi.fn>;

const baseState: PluginsState = {
  installed: [
    { path: "github.com/caddy-dns/cloudflare", version: "v0.2.1", modules: ["dns.providers.cloudflare"], tested: true },
  ],
  installEnabled: true,
  setup: { state: "supervised" },
  update: { available: false, managed: "", shipped: "" },
  autoUpdate: false,
};
const op = (over: Partial<PluginOperation> = {}): PluginOperation => ({
  id: "op1",
  kind: "add",
  package: "github.com/x/y",
  step: "downloading",
  startedAt: "2026-01-01T00:00:00Z",
  ...over,
});
const ok = (result: unknown) => Promise.resolve({ data: { result } });

function mockGet(state: PluginsState, catalog: unknown[] = []) {
  get.mockImplementation((url: string) =>
    url === "/caddy/plugins/catalog" ? ok(catalog) : ok(state)
  );
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.useRealTimers());

describe("AddPluginDialog", () => {
  const catalog = [
    { path: "github.com/a/tested", repo: "", modules: ["a.mod"], tested: true, installed: false },
    { path: "github.com/b/untested", repo: "", modules: ["b.mod"], tested: false, installed: false },
  ];

  it("warns only for untested plugins and posts the path", async () => {
    mockGet(baseState, catalog);
    post.mockReturnValue(ok(op()));
    const onStarted = vi.fn();
    render(<AddPluginDialog open onOpenChange={() => {}} onStarted={onStarted} />);

    fireEvent.click(await screen.findByText("github.com/a/tested"));
    expect(screen.queryByText(/hasn't been tested with CPM/)).toBeNull();
    fireEvent.click(screen.getByText("github.com/b/untested"));
    expect(screen.getByText(/hasn't been tested with CPM/)).toBeTruthy();

    fireEvent.click(screen.getByRole("button", { name: /install & restart caddy/i }));
    await waitFor(() => expect(onStarted).toHaveBeenCalled());
    expect(post).toHaveBeenCalledWith("/caddy/plugins", { path: "github.com/b/untested" });
  });

  it("links each catalog entry to its repository", async () => {
    mockGet(baseState, [
      { path: "github.com/c/linked", repo: "https://github.com/c/linked", modules: ["c.mod"], tested: false, installed: false },
      { path: "github.com/d/norepo", repo: "", modules: ["d.mod"], tested: false, installed: false },
    ]);
    render(<AddPluginDialog open onOpenChange={() => {}} onStarted={() => {}} />);
    await screen.findByText("github.com/c/linked");
    const links = screen.getAllByRole("link", { name: /repository/i });
    expect(links).toHaveLength(1);
    expect(links[0].getAttribute("href")).toBe("https://github.com/c/linked");
    expect(links[0].getAttribute("target")).toBe("_blank");
    // The link must not be nested inside the selectable row button.
    expect(links[0].closest("button")).toBeNull();
  });

  it("says that Caddy itself may be updated", async () => {
    mockGet(baseState, catalog);
    render(<AddPluginDialog open onOpenChange={() => {}} onStarted={() => {}} />);
    expect(await screen.findByText(CADDY_UPDATE_NOTE)).toBeTruthy();
    expect(CADDY_UPDATE_NOTE).toBe(
      "If a newer Caddy release is available, Caddy is updated to it as part of this change."
    );
  });

  it("shows an unavailable state when the catalog is unreachable", async () => {
    get.mockRejectedValue({ response: { status: 503, data: { error: { message: "build service down" } } } });
    render(<AddPluginDialog open onOpenChange={() => {}} onStarted={() => {}} />);
    expect(await screen.findByText("Plugin catalog unavailable: build service down")).toBeTruthy();
  });

  it("debounces the search by 300 ms", async () => {
    vi.useFakeTimers();
    mockGet(baseState, catalog);
    render(<AddPluginDialog open onOpenChange={() => {}} onStarted={() => {}} />);
    await act(async () => { await vi.advanceTimersByTimeAsync(0); });
    get.mockClear();
    fireEvent.change(screen.getByLabelText("Search plugins"), { target: { value: "ab" } });
    await act(async () => { await vi.advanceTimersByTimeAsync(299); });
    expect(get).not.toHaveBeenCalled();
    await act(async () => { await vi.advanceTimersByTimeAsync(1); });
    expect(get).toHaveBeenCalledWith("/caddy/plugins/catalog", { params: { q: "ab" } });
  });
});

describe("InstalledPluginsSection", () => {
  it("renders usages when removal is blocked", async () => {
    mockGet(baseState);
    del.mockRejectedValue({
      response: {
        status: 409,
        data: { error: { message: "in use", invalid: [{ kind: "dnsProvider", id: 1, name: "CF" }] } },
      },
    });
    render(<InstalledPluginsSection />);
    fireEvent.click(await screen.findByRole("button", { name: /remove github.com\/caddy-dns/i }));
    expect(screen.getByText(CADDY_UPDATE_NOTE)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /remove & restart caddy/i }));
    expect(await screen.findByText("Used by DNS provider 'CF'")).toBeTruthy();
  });

  it("keeps showing the last failed change", async () => {
    mockGet({
      ...baseState,
      lastFailed: op({ kind: "fallback", package: "", step: "failed", error: "exec format error" }),
    });
    render(<InstalledPluginsSection />);
    const banner = await screen.findByRole("alert");
    expect(banner.textContent).toMatch(/exec format error/);
  });

  it("does not repeat a failed update in a second banner", async () => {
    const failed = op({ kind: "update", step: "rolledBack", error: "boom" });
    mockGet({
      ...baseState,
      update: { available: true, managed: "2.8.0", shipped: "2.9.0" },
      lastUpdate: failed,
      lastFailed: failed,
    });
    render(<InstalledPluginsSection />);
    expect(await screen.findByText(/was rolled back: boom/)).toBeTruthy();
    expect(screen.getAllByText(/boom/)).toHaveLength(1);
  });

  it("shows the drop-in when setup is needed", async () => {
    mockGet({
      ...baseState,
      installEnabled: false,
      disabledReason: "Run this once",
      setup: { state: "setup-needed", reason: "Run this once", dropIn: "[Service]\nFoo=bar" },
    });
    render(<InstalledPluginsSection />);
    expect(await screen.findByText("Run this once")).toBeTruthy();
    expect(screen.getAllByText("Run this once")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Copy" })).toBeTruthy();
    expect(screen.getByText(/Foo=bar/).tagName).toBe("PRE");
  });

  it("renders no empty drop-in block when setup-needed arrives without a drop-in", async () => {
    mockGet({
      ...baseState,
      installEnabled: false,
      setup: { state: "setup-needed", reason: "Run this once", dropIn: "" },
    });
    const { container } = render(<InstalledPluginsSection />);
    expect(await screen.findByText("Run this once")).toBeTruthy();
    expect(container.querySelector("pre")).toBeNull();
    expect(screen.queryByRole("button", { name: "Copy" })).toBeNull();
  });

  it("hides the buttons and shows the reason when install is disabled", async () => {
    mockGet({ ...baseState, installEnabled: false, disabledReason: "Disabled by CPM_PLUGIN_INSTALL" });
    render(<InstalledPluginsSection />);
    expect(await screen.findByText("Disabled by CPM_PLUGIN_INSTALL")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /add plugin/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /remove/i })).toBeNull();
  });

  it("shows the API-mode notice on 400", async () => {
    get.mockRejectedValue({ response: { status: 400 } });
    render(<InstalledPluginsSection />);
    expect(await screen.findByText(/requires Caddy API mode/)).toBeTruthy();
  });

  it("shows a running operation on load and the update banner", async () => {
    mockGet({
      ...baseState,
      operation: op({ step: "verifying" }),
      update: { available: true, managed: "2.8.0", shipped: "2.9.0" },
      lastUpdate: op({ kind: "update", step: "rolledBack", error: "boom" }),
    });
    render(<InstalledPluginsSection />);
    expect(await screen.findByText("Verifying binary")).toBeTruthy();
    expect(screen.getByText("Caddy 2.9.0 is available (running 2.8.0)")).toBeTruthy();
    expect(screen.getByText(/was rolled back: boom/)).toBeTruthy();
  });
});

describe("PluginOperationProgress", () => {
  it("says when Caddy itself was updated", async () => {
    const onFinished = vi.fn();
    render(
      <PluginOperationProgress
        operation={op({ step: "done", caddyVersion: "v2.12.0", previousCaddyVersion: "v2.11.7" })}
        onFinished={onFinished}
      />
    );
    expect(screen.getByRole("status").textContent).toMatch(/Caddy updated to v2\.12\.0/);
  });

  it("does not mention a Caddy update when the version stayed", async () => {
    render(
      <PluginOperationProgress
        operation={op({ step: "done", caddyVersion: "v2.11.7", previousCaddyVersion: "v2.11.7" })}
        onFinished={() => {}}
      />
    );
    expect(screen.getByRole("status").textContent).not.toMatch(/Caddy updated/);
  });

  it("stops polling when the operation is gone", async () => {
    vi.useFakeTimers();
    get.mockRejectedValue({ response: { status: 404 } });
    const onFinished = vi.fn();
    render(<PluginOperationProgress operation={op()} onFinished={onFinished} />);
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(screen.getByRole("status").textContent).toBe(
      "Operation no longer available (CPM restarted?)"
    );
    expect(onFinished).toHaveBeenCalledTimes(1);
    await act(async () => { await vi.advanceTimersByTimeAsync(5000); });
    expect(get).toHaveBeenCalledTimes(1);
  });

  it("keeps polling through other errors", async () => {
    vi.useFakeTimers();
    get.mockRejectedValueOnce({ response: { status: 502 } }).mockReturnValue(ok(op({ step: "done" })));
    const onFinished = vi.fn();
    render(<PluginOperationProgress operation={op()} onFinished={onFinished} />);
    await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
    expect(get).toHaveBeenCalledTimes(2);
    expect(onFinished).toHaveBeenCalledTimes(1);
  });

  it("polls and renders rolledBack with the error, then calls onFinished", async () => {
    vi.useFakeTimers();
    get.mockReturnValue(ok(op({ step: "rolledBack", error: "caddy did not start" })));
    const onFinished = vi.fn();
    render(<PluginOperationProgress operation={op()} onFinished={onFinished} />);
    expect(screen.getByText("Downloading Caddy").closest("li")?.getAttribute("aria-current")).toBe("step");
    await act(async () => { await vi.advanceTimersByTimeAsync(1000); });
    expect(get).toHaveBeenCalledWith("/caddy/plugins/operations/op1");
    expect(screen.getByRole("status").textContent).toMatch(/Rolled back.*caddy did not start/);
    expect(onFinished).toHaveBeenCalledTimes(1);
  });
});
