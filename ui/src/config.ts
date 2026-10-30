export const modes = {
  self_hosted: ["anonymous", "basic"],
  aws_managed: ["aws_default", "aws_profile", "aws_static", "basic"],
  aws_serverless: ["aws_default", "aws_profile", "aws_static"],
} as const;

export type Deployment = keyof typeof modes;
export type AuthMode = (typeof modes)[Deployment][number];

export function isDeployment(value: string): value is Deployment {
  return Object.hasOwn(modes, value);
}

export function isAuthMode(deployment: Deployment, value: string): value is AuthMode {
  return (modes[deployment] as readonly string[]).includes(value);
}

export function validate(extra: Record<string, string>): string[] {
  const errors: string[] = [];
  const deployment = extra.deployment || "self_hosted";
  const auth = extra.auth_mode || "anonymous";
  if (!isDeployment(deployment)) return ["errors.deployment"];
  if (!isAuthMode(deployment, auth)) errors.push("errors.authentication");
  const endpoint = extra.endpoint || "";
  try {
    const url = new URL(endpoint);
    if (!["http:", "https:"].includes(url.protocol) || !url.hostname || url.username || url.password || endpoint.includes("?") || endpoint.includes("#")) {
      errors.push("errors.endpoint_format");
    } else if (deployment !== "self_hosted" && url.protocol !== "https:") {
      errors.push("errors.aws_https");
    }
  } catch {
    errors.push("errors.endpoint_required");
  }
  if (deployment !== "self_hosted" && !extra.aws_region?.trim()) errors.push("errors.region");
  if (auth === "aws_profile" && !extra.aws_profile?.trim()) errors.push("errors.profile");
  if (extra.pagination_mode && extra.pagination_mode !== "offset" && extra.pagination_mode !== "pit") errors.push("errors.pagination_mode");
  for (const [key, low, high] of [["page_size", 1, 1000], ["request_timeout_ms", 1000, 300000]] as const) {
    const value = extra[key];
    if (value && (!/^\d+$/.test(value) || Number(value) < low || Number(value) > high)) {
      errors.push(`errors.${key}`);
    }
  }
  return errors;
}
