import { defineSlot, usePluginConnection, usePluginModal, usePluginTheme, usePluginTranslation } from "@tabularis/plugin-api";
import { rawSource, RawSourceView } from "./raw-source-view";

const RawSourceAction = defineSlot("data-grid.context-menu.items", ({ context, pluginId }) => {
  const { connectionId } = usePluginConnection();
  const { openModal } = usePluginModal();
  const { colors } = usePluginTheme();
  const t = usePluginTranslation(pluginId);
  const source = rawSource(context.rowData, context.connectionId, connectionId);
  if (source === null) return null;

  return (
    <button
      type="button"
      style={{ width: "100%", textAlign: "left", padding: "6px 12px", color: colors?.text.primary, background: "transparent", border: 0, cursor: "pointer" }}
      onClick={() => {
        if (context.connectionId !== connectionId) return;
        openModal({ title: t("raw_source_title"), size: "lg", content: <RawSourceView text={source} label={t("raw_source_label")} foreground={colors?.text.primary} background={colors?.bg.input} /> });
      }}
    >
      {t("raw_source_action")}
    </button>
  );
});

export default RawSourceAction.component;
