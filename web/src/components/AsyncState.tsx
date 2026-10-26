type AsyncStateProps = {
  durum: "yukleniyor" | "bos" | "hata";
  mesaj?: string;
  bosMetni?: string;
  onTekrar?: () => void;
};

export function AsyncState({ durum, mesaj, bosMetni, onTekrar }: AsyncStateProps) {
  if (durum === "yukleniyor") {
    return (
      <p className="durum-bildirimi" role="status" aria-live="polite">
        Yükleniyor…
      </p>
    );
  }

  if (durum === "bos") {
    return (
      <p className="durum-bildirimi" role="status">
        {bosMetni ?? "Bu ekran henüz bağlanmadı."}
      </p>
    );
  }

  return (
    <div className="durum-bildirimi durum-bildirimi--hata" role="alert">
      <p>{mesaj ?? "Bir hata oluştu."}</p>
      {onTekrar ? (
        <button type="button" className="dugme dugme--ikincil" onClick={onTekrar}>
          Tekrar dene
        </button>
      ) : null}
    </div>
  );
}
