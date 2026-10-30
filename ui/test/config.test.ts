import { describe, expect, it } from "vitest";
import { isAuthMode, validate } from "../src/config";

describe("connection configuration", () => {
  it("accepts a self-hosted anonymous endpoint and an AWS profile", () => {
    expect(validate({ endpoint: "http://localhost:9200", deployment: "self_hosted", auth_mode: "anonymous" })).toEqual([]);
    expect(validate({ endpoint: "https://example.com", deployment: "aws_managed", auth_mode: "aws_profile", aws_region: "us-east-1", aws_profile: "dev" })).toEqual([]);
  });

  it("rejects credential-bearing endpoints, insecure AWS, unsupported modes and invalid limits", () => {
    expect(validate({ endpoint: "https://user:pass@example.com", deployment: "self_hosted", auth_mode: "basic" })).not.toEqual([]);
    expect(validate({ endpoint: "http://example.com", deployment: "aws_serverless", auth_mode: "aws_default", aws_region: "us-east-1" })).not.toEqual([]);
    expect(isAuthMode("aws_serverless", "basic")).toBe(false);
    expect(validate({ endpoint: "https://example.com", page_size: "0", request_timeout_ms: "999" })).toHaveLength(2);
  });

  it("requires a region for a managed domain using Basic authentication", () => {
    expect(validate({ endpoint: "https://example.com", deployment: "aws_managed", auth_mode: "basic" })).toContain("errors.region");
    expect(validate({ endpoint: "https://example.com", deployment: "aws_managed", auth_mode: "basic", aws_region: "us-east-1" })).toEqual([]);
  });

  it("accepts supported pagination modes and rejects unknown modes", () => {
    const config = { endpoint: "https://example.com", deployment: "self_hosted", auth_mode: "anonymous" };
    expect(validate({ ...config, pagination_mode: "offset" })).toEqual([]);
    expect(validate({ ...config, pagination_mode: "pit" })).toEqual([]);
    expect(validate({ ...config, pagination_mode: "unknown" })).toContain("errors.pagination_mode");
  });
});
