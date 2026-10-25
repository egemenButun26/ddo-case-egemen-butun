# ParkAkıl — Claude kuralları

Kod sistemi `youtube-otomasyon` deposundan taşındı. Ayrıntı: `KOD-SISTEMI.md`.

## Orkestra kuralı

- Sen şefsin: işi böl, brif yaz, model seç, denetçiye kontrol ettir, birleştir, sade Türkçeyle raporla.
- Kendi işini kendin onaylama.
- **Kodu DeepSeek yazar.** Kod işi brifle `node opencode-yaz.js` üzerinden DeepSeek'e gider;
  ben planlar, denetçiye kontrol ettirir, birleştiririm. Mekanik işler (test koşturma, toplu dosya işi,
  düzeltme turları) de DeepSeek'e gider. Claude yalnız karar, denetim ve rapor için kullanılır.
- Model (Claude ajanları): mekanik iş **haiku**; araştırma ve denetim **sonnet**; tasarım ve riskli denetim **opus**.
  Genel amaçlı ajana modeli açıkça ver.
- Her kod işi ayrı çalışma kopyasında (worktree) ve kendi dalında:
  `git worktree add -b <dal> .claude/worktrees/<ad> main`
- Ana dala yalnızca sen, testler geçtikten sonra, `merge --no-ff` ile birleştirirsin.
- DÜZELT en fazla 3 tur; sonra bana sor.
- Küçük iş (tek dosya, ~30 satır altı): denetçi yok, diff'e sen bak. Orta: DeepSeek + denetçi. Büyük: işi böl, parça parça.
- Workflow (çok ajanlı otomatik plan) yalnızca ben "workflow kullan" dersem.

## Kadro

| Kim | Model | İş |
|---|---|---|
| DeepSeek (opencode) | `opencode-go/deepseek-v4.1-flash` | Kod + test yazar, testi çalıştırır. Commit atmaz. |
| `denetci` | sonnet (riskte opus) | Bağımsız kontrol; ONAY ya da DUZELT. Dosya değiştirmez. |

## Değişmez kurallar

- **Testler** `test/fazN.js` (her iş yeni numara). Hepsi `node test/fazN.js` ile tek başına çalışır.
- **Yeşil test kanıt değil.** Düzeltme geri alınınca test kırılmıyorsa test bir şey ölçmüyor (mutasyon kontrolü).
- **Para kuralı.** Ücretli API, model ya da abonelik için önce tahmini tutarı yaz, onayımı al.
- **Sırlar.** `.env` ve opencode `auth.json` okunmaz, yazdırılmaz, loga ya da sohbete geçmez.
- **Tehlikeli git yok.** stash, reset --hard, rebase, push --force yasak.
- **Uydurma yok.** Bilmediğin şeyi "doğrulanamadı" diye yaz.
