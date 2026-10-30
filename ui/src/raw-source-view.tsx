export function RawSourceView({ text, label, foreground, background }: { text: string; label: string; foreground?: string; background?: string }) {
  return (
    <textarea
      aria-label={label}
      readOnly
      value={text}
      spellCheck={false}
      style={{ boxSizing: "border-box", width: "100%", height: "100%", minHeight: 360, resize: "vertical", fontFamily: "monospace", color: foreground, backgroundColor: background }}
    />
  );
}

export function rawSource(row: Record<string, unknown> | undefined, rowConnection: string | null, activeConnection: string | null): string | null {
  if (!rowConnection || rowConnection !== activeConnection) return null;
  const value = row?._source;
  return typeof value === "string" ? value : null;
}
