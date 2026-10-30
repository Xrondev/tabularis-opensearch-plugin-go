import { readFileSync } from "node:fs";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { SlotContextMap } from "@tabularis/plugin-api";
import { ConnectionForm } from "../src/index";

const strings = JSON.parse(readFileSync(new URL("../../locales/en.json", import.meta.url), "utf8")) as Record<string, unknown>;
const translate = (key: string) => {
  const value = key.split(".").reduce<unknown>((current, part) => current && typeof current === "object" ? (current as Record<string, unknown>)[part] : undefined, strings);
  return typeof value === "string" ? value : key;
};
const render = (extra: Record<string, string>) => {
  const context: SlotContextMap["connection-modal.extra_fields"] = {
    driver: "opensearch", extra, credentialFieldsHidden: false,
    setExtraField: () => {}, setCredentialFieldsHidden: () => {},
  };
  return renderToStaticMarkup(<ConnectionForm context={context} colors={null} translate={translate} />);
};

describe("connection form", () => {
  it("starts with compact essentials and closed advanced settings without required-field errors", () => {
    const html = render({});
    expect(html).toContain("Self-hosted OpenSearch");
    expect(html).toContain("Endpoint URL");
    expect(html).toContain("<details ");
    expect(html).not.toMatch(/<details[^>]*\sopen/);
    expect(html).toContain("Advanced settings");
    expect(html).toContain("Every non-null value must be visible to exists");
    expect(html).not.toContain("Enter a valid endpoint URL.");
    expect(html).not.toContain("AWS region");
  });

  it("renders the region and profile only for the selected AWS authentication", () => {
    const profile = render({ deployment: "aws_managed", auth_mode: "aws_profile", aws_region: "us-east-1", aws_profile: "dev" });
    expect(profile).toContain("AWS region");
    expect(profile).toContain("AWS profile name");
    expect(profile).toContain('value="dev"');
    expect(profile).toContain("SSO profiles require an active AWS login");
    expect(profile).not.toContain("AWS secret access key");
    const chain = render({ deployment: "aws_serverless", auth_mode: "aws_default" });
    expect(chain).toContain("AWS region");
    expect(chain).not.toContain("AWS profile name");
  });

  it("retains saved advanced values and the PIT safety guidance", () => {
    const html = render({ endpoint: "https://example.com", index_pattern: "logs-*", pagination_mode: "pit", page_size: "50", verified_exact_fields: "count" });
    expect(html).toContain('value="logs-*"');
    expect(html).toContain('value="50"');
    expect(html).toContain('value="count"');
    expect(html).toContain("Requires one concrete index with a single shard");
  });

  it("provides labels when the host has a cached older translation namespace", () => {
    const context: SlotContextMap["connection-modal.extra_fields"] = {
      driver: "opensearch", extra: {}, credentialFieldsHidden: false,
      setExtraField: () => {}, setCredentialFieldsHidden: () => {},
    };
    const html = renderToStaticMarkup(<ConnectionForm context={context} colors={null} translate={(key) => `opensearch:${key}`} />);
    expect(html).toContain("Advanced settings");
    expect(html).toContain("Enter the full HTTP or HTTPS endpoint.");
  });
});
