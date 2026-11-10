import { describe, expect, it, vi } from "vitest";
import { APIError, requestJSON } from "./client";
import { collectFormFields } from "./forms";

describe("shared API transport", () => {
  it("uses the caller's URL, cookies, CSRF token and cancellation signal", async () => {
    const controller = new AbortController();
    const fetch = vi
      .fn()
      .mockResolvedValue(new Response(JSON.stringify({ location: "/home" })));
    vi.stubGlobal("fetch", fetch);
    await expect(
      requestJSON(
        "/caller/action",
        { action: "save", ids: ["7"] },
        "caller-csrf",
        controller.signal,
      ),
    ).resolves.toEqual({ location: "/home" });
    expect(fetch).toHaveBeenCalledWith("/caller/action", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "X-CSRF-Token": "caller-csrf",
      },
      body: JSON.stringify({ action: "save", ids: ["7"] }),
      signal: controller.signal,
    });
  });

  it("preserves structured errors including replacement CSRF tokens", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(
            JSON.stringify({ error: "Denied", CSRF: "replacement" }),
            { status: 403 },
          ),
        ),
    );
    try {
      await requestJSON("/caller/session");
      expect.unreachable("expected an API error");
    } catch (error) {
      expect(error).toBeInstanceOf(APIError);
      expect(error).toMatchObject({
        status: 403,
        message: "Denied",
        result: { CSRF: "replacement" },
      });
    }
  });

  it("propagates cancellation without converting it into an API error", async () => {
    const cancelled = new DOMException("Aborted", "AbortError");
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(cancelled));
    await expect(requestJSON("/caller/page")).rejects.toBe(cancelled);
  });

  it("reports unreadable server errors without exposing response contents", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(new Response("internal detail", { status: 500 })),
    );
    await expect(requestJSON("/caller/page")).rejects.toMatchObject({
      status: 500,
      message: "The server returned an unreadable response. Please retry.",
    });
  });
});

it("collects successful controls and the submit action without app-specific array rules", () => {
  const form = document.createElement("form");
  form.innerHTML =
    '<input name="db" value="7"><input name="permission" value="view_only"><input name="permission" value="view_edit"><input name="ignored" disabled value="no"><button name="action" value="save">Save</button>';
  expect(collectFormFields(form, form.querySelector("button"))).toEqual({
    db: "7",
    permission: ["view_only", "view_edit"],
    action: "save",
  });
});
