import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { rawSource, RawSourceView } from "../src/raw-source-view";

describe("raw source inspection", () => {
  it("retains the exact numeric text when rendering read-only source", () => {
    const source = '{"big":9007199254740993,"nested":{"value":12345678901234567890}}';
    expect(rawSource({ _source: source }, "connection-a", "connection-a")).toBe(source);
    const html = renderToStaticMarkup(<RawSourceView text={source} label="Raw JSON" />);
    expect(html).toContain("9007199254740993");
    expect(html).toContain("12345678901234567890");
    expect(html).toContain('readOnly=""');
  });

  it("does not offer a raw view for missing or already decoded source", () => {
    expect(rawSource(undefined, "connection-a", "connection-a")).toBeNull();
    expect(rawSource({ _source: null }, "connection-a", "connection-a")).toBeNull();
    expect(rawSource({ _source: { big: 9007199254740992 } }, "connection-a", "connection-a")).toBeNull();
  });

  it("does not expose another connection's row", () => {
    expect(rawSource({ _source: "{}" }, "connection-a", "connection-b")).toBeNull();
    expect(rawSource({ _source: "{}" }, null, null)).toBeNull();
  });
});
