type DolulukCubuguProps = {
  dolu?: number;
  kapasite?: number;
  etiket?: string;
};

export function dolulukYuzdesi(dolu?: number, kapasite?: number): number {
  if (
    typeof dolu !== "number" ||
    typeof kapasite !== "number" ||
    !Number.isFinite(dolu) ||
    !Number.isFinite(kapasite) ||
    kapasite <= 0
  ) {
    return 0;
  }
  const yuzde = Math.round((dolu / kapasite) * 100);
  return Math.min(100, Math.max(0, yuzde));
}

export function DolulukCubugu({ dolu, kapasite, etiket }: DolulukCubuguProps) {
  const yuzde = dolulukYuzdesi(dolu, kapasite);
  return (
    <div className="doluluk">
      {etiket ? <p className="doluluk-etiketi">{etiket}</p> : null}
      <div
        className="doluluk-cubugu"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={yuzde}
        aria-label={etiket ? `${etiket} doluluk` : "Doluluk"}
      >
        <span className="doluluk-dolgu" style={{ width: `${yuzde}%` }} />
      </div>
      <p className="soluk doluluk-metni">
        %{yuzde} · {typeof dolu === "number" ? dolu : "—"} /{" "}
        {typeof kapasite === "number" ? kapasite : "—"}
      </p>
    </div>
  );
}
