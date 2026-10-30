// @vitest-environment jsdom
import { useState } from "react";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { SlotContextMap } from "@tabularis/plugin-api";
import { ConnectionForm } from "../src/index";

let container: HTMLDivElement;
let root: Root;

function Harness({ initial }: { initial: Record<string, string> }) {
  const [extra, setExtra] = useState(initial);
  const [hidden, setHidden] = useState(false);
  const [secrets, setSecrets] = useState({ aws_secret_access_key: { value: "", hasStoredValue: true, dirty: false } });
  const context: SlotContextMap["connection-modal.extra_fields"] = {
    driver: "opensearch", extra, credentialFieldsHidden: hidden, setCredentialFieldsHidden: setHidden,
    setExtraField: (key, value) => setExtra((old) => ({ ...old, [key]: value })),
    secretFields: secrets,
    setSecretField: (key, value) => setSecrets((old) => ({ ...old, [key]: { value, hasStoredValue: true, dirty: true } })),
  };
  return <><ConnectionForm context={context} colors={null} translate={(key) => key} /><output data-hidden={hidden} data-extra={JSON.stringify(extra)} data-secrets={JSON.stringify(secrets)} /></>;
}

beforeEach(() => {
  Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
});
afterEach(async () => {
  await act(async () => root.unmount());
  container.remove();
});

function control(label: string): HTMLInputElement | HTMLSelectElement {
  const element = Array.from(container.querySelectorAll("label")).find((item) => item.textContent === label);
  if (!element || !element.htmlFor) throw new Error(`Missing control: ${label}`);
  const input = document.getElementById(element.htmlFor);
  if (!(input instanceof HTMLInputElement || input instanceof HTMLSelectElement)) throw new Error(`Invalid control: ${label}`);
  return input;
}
function state() {
  const output = container.querySelector("output");
  if (!output) throw new Error("Missing state output");
  return output;
}
async function select(label: string, value: string) {
  const input = control(label);
  await act(async () => { input.value = value; input.dispatchEvent(new Event("change", { bubbles: true })); });
}

describe("connection form interactions", () => {
  it("updates deployment and profile fields and preserves advanced settings", async () => {
    await act(async () => root.render(<Harness initial={{ index_pattern: "logs-*", page_size: "25" }} />));
    expect(state().dataset.hidden).toBe("true");
    await select("deployment", "aws_managed");
    expect(control("region")).toBeTruthy();
    expect(JSON.parse(state().dataset.extra ?? "{}")).toMatchObject({ deployment: "aws_managed", auth_mode: "aws_default", index_pattern: "logs-*", page_size: "25" });
    await select("authentication", "aws_profile");
    expect(control("profile")).toBeTruthy();
    await select("authentication", "basic");
    expect(state().dataset.hidden).toBe("false");
  });

  it("clears a stored static secret when switching authentication", async () => {
    await act(async () => root.render(<Harness initial={{ deployment: "aws_serverless", auth_mode: "aws_static" }} />));
    await select("authentication", "aws_default");
    const secrets = JSON.parse(state().dataset.secrets ?? "{}");
    expect(secrets.aws_secret_access_key).toMatchObject({ value: "", dirty: true });
  });

  it("shows endpoint validation on blur, not on initial render", async () => {
    await act(async () => root.render(<Harness initial={{}} />));
    expect(container.textContent).not.toContain("errors.endpoint_required");
    await act(async () => { control("endpoint").dispatchEvent(new FocusEvent("focusout", { bubbles: true })); });
    expect(container.textContent).toContain("errors.endpoint_required");
  });
});
