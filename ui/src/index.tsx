import { defineSlot, usePluginTheme, usePluginTranslation, type PluginTranslator, type SlotContextMap, type ThemeColors } from "@tabularis/plugin-api";
import { useEffect, useId, useState, type CSSProperties, type ReactNode } from "react";
import { isAuthMode, isDeployment, modes, validate, type Deployment } from "./config";

const secretKeys = ["aws_access_key_id", "aws_secret_access_key", "aws_session_token"] as const;
const newLabels: Record<string, string> = { advanced_settings: "Advanced settings", advanced_hint: "Index scope, pagination, and timeout", endpoint_hint: "Enter the full HTTP or HTTPS endpoint." };

export function ConnectionForm({ context, colors, translate }: { context: SlotContextMap["connection-modal.extra_fields"]; colors: ThemeColors | null; translate: PluginTranslator }) {
  const t = (key: string) => {
    const result = translate(key);
    return (result === key || result.endsWith(`:${key}`)) && newLabels[key] ? newLabels[key] : result;
  };
  const { extra, setExtraField, setSecretField, secretFields, setCredentialFieldsHidden, credentialFieldsHidden } = context;
  const deployment = isDeployment(extra.deployment) ? extra.deployment : "self_hosted";
  const auth = extra.auth_mode || "anonymous";
  const hideCredentials = auth !== "basic";
  const [touched, setTouched] = useState<Record<string, boolean>>({});
  const idPrefix = useId();

  useEffect(() => {
    if (!extra.deployment) setExtraField("deployment", "self_hosted");
    if (!extra.auth_mode) setExtraField("auth_mode", "anonymous");
    if (credentialFieldsHidden !== hideCredentials) setCredentialFieldsHidden(hideCredentials);
  }, [credentialFieldsHidden, extra.auth_mode, extra.deployment, hideCredentials, setCredentialFieldsHidden, setExtraField]);

  const clearStaticSecrets = () => {
    for (const key of secretKeys) {
      if (secretFields?.[key]?.hasStoredValue || secretFields?.[key]?.value) setSecretField?.(key, "");
    }
  };
  const changeAuth = (next: string) => {
    if (auth === "aws_static" && next !== "aws_static") clearStaticSecrets();
    if (auth === "aws_profile" && next !== "aws_profile") setExtraField("aws_profile", "");
    setExtraField("auth_mode", next);
  };
  const changeDeployment = (next: Deployment) => {
    setExtraField("deployment", next);
    if (!isAuthMode(next, auth)) changeAuth(modes[next][0]);
  };

  const inputStyle: CSSProperties = {
    width: "100%", minWidth: 0, boxSizing: "border-box", padding: "8px 12px",
    backgroundColor: colors?.bg.input ?? "var(--color-bg-base, #131929)",
    color: colors?.text.primary ?? "var(--color-text-primary, #e2e8f0)",
    border: `1px solid ${colors?.border.strong ?? "rgba(255,255,255,0.15)"}`,
    borderRadius: 6, fontSize: 13, lineHeight: 1.4,
  };
  const labelStyle: CSSProperties = { fontSize: 10, fontWeight: 600, letterSpacing: "0.05em", textTransform: "uppercase", color: colors?.text.muted ?? "var(--color-text-muted, #94a3b8)" };
  const hintStyle: CSSProperties = { margin: 0, fontSize: 11, lineHeight: 1.5, color: colors?.text.muted ?? "var(--color-text-muted, #94a3b8)" };
  const fieldStyle: CSSProperties = { display: "grid", gap: 4, minWidth: 0 };
  const rowStyle: CSSProperties = { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 180px), 1fr))", gap: 12 };
  const errors = validate(extra);
  const aws = deployment !== "self_hosted";
  const secureStorageUnavailable = auth === "aws_static" && (!setSecretField || !secretFields);
  const errorFor = (field: string, keys: string[]) => touched[field] ? keys.find((key) => errors.includes(`errors.${key}`)) : undefined;
  const field = (name: string, label: string, control: (id: string, describedBy?: string, invalid?: boolean) => ReactNode, options: { errors?: string[]; hint?: string } = {}) => {
    const id = `${idPrefix}-${name}`;
    const error = errorFor(name, options.errors ?? []);
    const description = [options.hint && `${id}-hint`, error && `${id}-error`].filter(Boolean).join(" ") || undefined;
    return <div style={fieldStyle} key={name}>
      <label htmlFor={id} style={labelStyle}>{t(label)}</label>
      {control(id, description, !!error)}
      {options.hint && <p id={`${id}-hint`} style={hintStyle}>{options.hint}</p>}
      {error && <p id={`${id}-error`} role="alert" style={{ ...hintStyle, color: colors?.accent.error ?? "#ef4444" }}>{t(`errors.${error}`)}</p>}
    </div>;
  };
  const common = (name: string, id: string, description?: string, invalid?: boolean) => ({
    id, style: inputStyle, "aria-describedby": description, "aria-invalid": invalid || undefined,
    onBlur: () => setTouched((old) => ({ ...old, [name]: true })),
  });

  return <div style={{ display: "grid", gap: 12, color: colors?.text.primary }} data-tabularis-plugin="opensearch">
    <div style={rowStyle}>
      {field("deployment", "deployment", (id, desc, invalid) => <select {...common("deployment", id, desc, invalid)} value={deployment} onChange={(event) => changeDeployment(event.target.value as Deployment)}>
        <option value="self_hosted">{t("self_hosted")}</option><option value="aws_managed">{t("aws_managed")}</option><option value="aws_serverless">{t("aws_serverless")}</option>
      </select>)}
      {field("auth_mode", "authentication", (id, desc, invalid) => <select {...common("auth_mode", id, desc, invalid)} value={auth} onChange={(event) => changeAuth(event.target.value)}>
        {modes[deployment].map((mode) => <option key={mode} value={mode}>{t(`auth.${mode}`)}</option>)}
      </select>, { errors: ["authentication"] })}
    </div>
    {field("endpoint", "endpoint", (id, desc, invalid) => <input {...common("endpoint", id, desc, invalid)} type="url" required value={extra.endpoint ?? ""} placeholder={aws ? "https://search-example.us-east-1.es.amazonaws.com" : "https://localhost:9200"} autoCorrect="off" autoCapitalize="off" spellCheck={false} onChange={(event) => setExtraField("endpoint", event.target.value)} />, { errors: ["endpoint_required", "endpoint_format", "aws_https"], hint: t("endpoint_hint") })}
    {auth === "basic" && extra.endpoint?.toLowerCase().startsWith("http://") && <p role="alert" style={{ ...hintStyle, color: colors?.accent.warning ?? "#d97706" }}>{t("insecure_basic")}</p>}
    {aws && <div style={rowStyle}>
      {field("aws_region", "region", (id, desc, invalid) => <input {...common("aws_region", id, desc, invalid)} required value={extra.aws_region ?? ""} placeholder="us-east-1" autoCorrect="off" autoCapitalize="off" spellCheck={false} onChange={(event) => setExtraField("aws_region", event.target.value)} />, { errors: ["region"] })}
      {auth === "aws_profile" && field("aws_profile", "profile", (id, desc, invalid) => <input {...common("aws_profile", id, desc, invalid)} required value={extra.aws_profile ?? ""} placeholder="default" autoCorrect="off" autoCapitalize="off" spellCheck={false} onChange={(event) => setExtraField("aws_profile", event.target.value)} />, { errors: ["profile"], hint: t("sso_hint") })}
    </div>}
    {auth === "aws_static" && secretKeys.map((key) => {
      const secret = secretFields?.[key];
      const stored = secret?.hasStoredValue && !secret.dirty;
      return field(key, key, (id, desc, invalid) => <div style={{ display: "flex", gap: 8 }}><input {...common(key, id, desc, invalid)} type="password" autoComplete="off" disabled={secureStorageUnavailable} value={secret?.value ?? ""} placeholder={stored ? t("stored_secret") : ""} onChange={(event) => setSecretField?.(key, event.target.value)} />{stored && <button type="button" onClick={() => setSecretField?.(key, "")}>{t("clear_secret")}</button>}</div>);
    })}
    {secureStorageUnavailable && <p role="alert" style={{ ...hintStyle, color: colors?.accent.error ?? "#ef4444" }}>{t("secure_storage_required")}</p>}
    <details style={{ border: `1px solid ${colors?.border.default ?? "rgba(255,255,255,0.15)"}`, borderRadius: 6 }}>
      <summary style={{ cursor: "pointer", padding: "10px 12px", fontSize: 12, fontWeight: 600 }}>{t("advanced_settings")} <span style={{ ...hintStyle, fontWeight: 400 }}>— {t("advanced_hint")}</span></summary>
      <div style={{ display: "grid", gap: 12, padding: "0 12px 12px" }}>
        {field("index_pattern", "index_pattern", (id, desc, invalid) => <input {...common("index_pattern", id, desc, invalid)} value={extra.index_pattern ?? ""} placeholder="*" onChange={(event) => setExtraField("index_pattern", event.target.value)} />)}
        <div style={rowStyle}>
          {field("page_size", "page_size", (id, desc, invalid) => <input {...common("page_size", id, desc, invalid)} type="number" min={1} max={1000} value={extra.page_size ?? ""} placeholder="100" onChange={(event) => setExtraField("page_size", event.target.value)} />, { errors: ["page_size"] })}
          {field("request_timeout_ms", "request_timeout_ms", (id, desc, invalid) => <input {...common("request_timeout_ms", id, desc, invalid)} type="number" min={1000} max={300000} value={extra.request_timeout_ms ?? ""} placeholder="30000" onChange={(event) => setExtraField("request_timeout_ms", event.target.value)} />, { errors: ["request_timeout_ms"] })}
        </div>
        {field("pagination_mode", "pagination_mode", (id, desc, invalid) => <select {...common("pagination_mode", id, desc, invalid)} value={extra.pagination_mode || "offset"} onChange={(event) => setExtraField("pagination_mode", event.target.value)}><option value="offset">{t("pagination_offset")}</option><option value="pit">{t("pagination_pit")}</option></select>, { errors: ["pagination_mode"], hint: extra.pagination_mode === "pit" ? t("pagination_pit_hint") : undefined })}
        {field("verified_exact_fields", "verified_exact_fields", (id, desc, invalid) => <input {...common("verified_exact_fields", id, desc, invalid)} value={extra.verified_exact_fields ?? ""} placeholder="count, status" onChange={(event) => setExtraField("verified_exact_fields", event.target.value)} />, { errors: ["verified_exact_fields"], hint: t("verified_exact_fields_warning") })}
      </div>
    </details>
  </div>;
}

const ConnectionFields = defineSlot("connection-modal.extra_fields", ({ context, pluginId }) => {
  const translate = usePluginTranslation(pluginId);
  const { colors } = usePluginTheme();
  return <ConnectionForm context={context} colors={colors} translate={translate} />;
});

export default ConnectionFields.component;
