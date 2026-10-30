import { defineSlot, usePluginTheme, usePluginTranslation } from "@tabularis/plugin-api";
import { useEffect } from "react";
import { isAuthMode, isDeployment, modes, validate, type Deployment } from "./config";

const secretKeys = ["aws_access_key_id", "aws_secret_access_key", "aws_session_token"] as const;

const ConnectionFields = defineSlot("connection-modal.extra_fields", ({ context, pluginId }) => {
  const t = usePluginTranslation(pluginId);
  const { colors } = usePluginTheme();
  const { extra, setExtraField, setSecretField, secretFields, setCredentialFieldsHidden, credentialFieldsHidden } = context;
  const deployment = isDeployment(extra.deployment) ? extra.deployment : "self_hosted";
  const auth = extra.auth_mode || "anonymous";
  const hideCredentials = auth !== "basic";

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

  const inputStyle = {
    width: "100%",
    boxSizing: "border-box" as const,
    backgroundColor: colors?.bg.input,
    color: colors?.text.primary,
    border: `1px solid ${colors?.border.default ?? "currentColor"}`,
    borderRadius: 4,
    padding: "6px 8px",
  };
  const labelStyle = { display: "grid", gap: 4 };
  const errors = validate(extra);
  const aws = deployment !== "self_hosted";
  const secureStorageUnavailable = auth === "aws_static" && (!setSecretField || !secretFields);

  return (
    <div style={{ display: "grid", gap: 12, color: colors?.text.primary }}>
      <label style={labelStyle}>
        {t("deployment")}
        <select value={deployment} style={inputStyle} onChange={(event) => changeDeployment(event.target.value as Deployment)}>
          <option value="self_hosted">{t("self_hosted")}</option>
          <option value="aws_managed">{t("aws_managed")}</option>
          <option value="aws_serverless">{t("aws_serverless")}</option>
        </select>
      </label>
      <label style={labelStyle}>
        {t("authentication")}
        <select value={auth} style={inputStyle} onChange={(event) => changeAuth(event.target.value)}>
          {modes[deployment].map((mode) => <option key={mode} value={mode}>{t(`auth.${mode}`)}</option>)}
        </select>
      </label>
      <label style={labelStyle}>
        {t("endpoint")}
        <input type="url" required style={inputStyle} value={extra.endpoint ?? ""} placeholder="https://localhost:9200" onChange={(event) => setExtraField("endpoint", event.target.value)} />
      </label>
      {auth === "basic" && extra.endpoint?.toLowerCase().startsWith("http://") && (
        <p role="alert" style={{ color: colors?.accent.warning }}>{t("insecure_basic")}</p>
      )}
      {aws && (
        <label style={labelStyle}>
          {t("region")}
          <input required style={inputStyle} value={extra.aws_region ?? ""} placeholder="us-east-1" onChange={(event) => setExtraField("aws_region", event.target.value)} />
        </label>
      )}
      {auth === "aws_profile" && (
        <label style={labelStyle}>
          {t("profile")}
          <input required style={inputStyle} value={extra.aws_profile ?? ""} onChange={(event) => setExtraField("aws_profile", event.target.value)} />
          <small>{t("sso_hint")}</small>
        </label>
      )}
      {auth === "aws_static" && secretKeys.map((key) => {
        const field = secretFields?.[key];
        const stored = field?.hasStoredValue && !field.dirty;
        return (
          <label key={key} style={labelStyle}>
            {t(key)}
            <input type="password" autoComplete="off" disabled={secureStorageUnavailable} style={inputStyle} value={field?.value ?? ""} placeholder={stored ? t("stored_secret") : ""} onChange={(event) => setSecretField?.(key, event.target.value)} />
            {stored && <button type="button" onClick={() => setSecretField?.(key, "")}>{t("clear_secret")}</button>}
          </label>
        );
      })}
      {secureStorageUnavailable && <p role="alert" style={{ color: colors?.accent.error }}>{t("secure_storage_required")}</p>}
      <label style={labelStyle}>
        {t("index_pattern")}
        <input style={inputStyle} value={extra.index_pattern ?? ""} placeholder="*" onChange={(event) => setExtraField("index_pattern", event.target.value)} />
      </label>
      <label style={labelStyle}>
        {t("verified_exact_fields")}
        <input style={inputStyle} value={extra.verified_exact_fields ?? ""} placeholder="count, status" onChange={(event) => setExtraField("verified_exact_fields", event.target.value)} />
        <small>{t("verified_exact_fields_warning")}</small>
      </label>
      <label style={labelStyle}>
        {t("page_size")}
        <input type="number" min={1} max={1000} style={inputStyle} value={extra.page_size ?? ""} placeholder="100" onChange={(event) => setExtraField("page_size", event.target.value)} />
      </label>
      <label style={labelStyle}>
        {t("pagination_mode")}
        <select style={inputStyle} value={extra.pagination_mode || "offset"} onChange={(event) => setExtraField("pagination_mode", event.target.value)}>
          <option value="offset">{t("pagination_offset")}</option>
          <option value="pit">{t("pagination_pit")}</option>
        </select>
        {extra.pagination_mode === "pit" && <small>{t("pagination_pit_hint")}</small>}
      </label>
      <label style={labelStyle}>
        {t("request_timeout_ms")}
        <input type="number" min={1000} max={300000} style={inputStyle} value={extra.request_timeout_ms ?? ""} placeholder="30000" onChange={(event) => setExtraField("request_timeout_ms", event.target.value)} />
      </label>
      {errors.length > 0 && <ul aria-live="polite" style={{ color: colors?.accent.error }}>{errors.map((error) => <li key={error}>{t(error)}</li>)}</ul>}
    </div>
  );
});

export default ConnectionFields.component;
