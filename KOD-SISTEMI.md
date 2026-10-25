# Kod sistemi — plan Claude'da, kod DeepSeek'te

Kaynak: `youtube-otomasyon/kronik/ORKESTRA.md` ("Kodu DeepSeek yazar", "Brif şablonu"), 2026-10-25'te taşındı.

## Akış

```
ŞEF (Claude) brif yazar ─▶ worktree + dal açar ─▶ opencode-yaz.js ─▶ DeepSeek kodu + testi yazar
                                                                  └─▶ şef commit eder ─▶ DENETÇİ ─▶ ONAY ─▶ şef merge --no-ff
                                                                                           └─ DUZELT ─▶ düzeltme brifi ─▶ DeepSeek (en fazla 3 tur)
```

1. Worktree aç: `git worktree add -b <dal> .claude/worktrees/<ad> main`
2. Brifi yaz (aşağıdaki şablon), depo dışında ya da geçici klasörde tut.
3. Çalıştır:
   ```
   node opencode-yaz.js --brif <brif.md> --worktree .claude/worktrees/<ad> [--model <id>] [--sure-dk 20] [--dene]
   ```
4. Çıktıdaki `git status` / `git diff --stat` özetine bak, testleri kendin bir kez daha çalıştır, commit et.
5. Orta/büyük işte `denetci` ajanına ver. ONAY → `git merge --no-ff <dal>`; DUZELT → düzeltme brifi.
6. Birleştirdikten sonra worktree'yi kaldır: `git worktree remove .claude/worktrees/<ad>`.

## opencode-yaz.js

- Brifi worktree'ye `OPENCODE-BRIF.md` olarak koyar (git dışı), `opencode run -m <model> "<sabit istem>"` çalıştırır,
  bitince süreyi, çıkış kodunu, `git status` ve `git diff --stat` özetini basar. Log: worktree'de `opencode-<zaman>.log`.
- **Korumalar:** ana kopyada, `main`/`master` dalında ya da ayrık HEAD'de çalışmayı reddeder.
  Asla commit, push, stash, reset yapmaz. `.env` / `auth.json` okunmaz.
- **Çıkış kodları:** 0 tamam · 1 opencode başlatılamadı/hata · 2 argüman hatası · 3 worktree koruması · 4 zaman aşımı.
- `--dene`: brifi kopyalar ve komutu basar, opencode'u çağırmaz (kuru çalışma).
- Testi: `node test/faz1.js` (ağ yok, opencode çağrılmaz).

## Modeller

| Model | Ne zaman |
|---|---|
| `opencode-go/deepseek-v4.1-flash` | Varsayılan (OpenCode Go aboneliği, ücretli) |
| `opencode/deepseek-v4.1-flash-free` | Ücretsiz deneme |
| `opencode-go/deepseek-v4-pro` | Flash 3 turda beceremezse |

Liste: `opencode models`. Giriş: kanal sahibi kendisi `opencode auth login` yapar; anahtar sohbete ve loga girmez.

## Brif şablonu

DeepSeek bu sohbeti görmez; bildiği tek şey brif. Söyleneni yapar, söylenmeyeni düşünmez.
Her brif aynı altı başlıkla yazılır:

```
GÖREV: Ne yapılacak ve neden (2–3 cümle).
SENİN DOSYALARIN: değiştirebileceği dosyalar (ve yeni test: test/fazN.js)
DOKUNMA: dokunmaması gereken dosyalar/alanlar
ÖNCE OKU: CLAUDE.md, ilgili kaynak dosyalar
BİTTİ TANIMI:
  - ölçülebilir kabul maddeleri
  - node test/fazN.js yeşil + eski testler yeşil
  - mutasyon kontrolü: düzeltme geri alınınca test kırılıyor (sonucu rapora yaz)
RAPOR: {"dosyalar": [], "ozet": "", "olcumler": {}, "acikSorunlar": []}
```

- Uç durumları açıkça yaz: boş girdi, NaN, sonsuz, negatif, Windows yolu, Türkçe karakterli yol.
  (Saha: brifte yazılmayan `Infinity` girdisi bozuk çıktı verdi.)
- Kabul testi mümkünse brifle birlikte ver.
- **Brifi küçük tut.** Tek oturumda bitecek boyut: 1 dosya + 1 test ideal. Büyük iş birden çok brife bölünür.
  (Saha: iki özelliği bir arada isteyen brif 30 dk zaman aşımına takıldı, hiçbir şey yazılmadı.)

## Sahadan dersler

- `opencode run` stdin bir boru olarak açık kalırsa girdi bekleyip sonsuza kadar asılır → betik stdin'i kapalı başlatır.
- opencode proje klasörünü `cwd`'den değil miras kalan `PWD`'den alır → betik `PWD`'yi worktree'ye ayarlar.
- opencode'u çağıran kodu değiştirdiysen, birleştirmeden önce geçici bir depoda gerçek bir mini işle
  (ör. "selam.txt yaz") uçtan uca dene. Yeşil birim testi bunu yakalamaz.
- Denetçi raporundaki sayıları şef kendisi doğrular.
- Ne zaman Claude'a dönülür: mimari karar, belirsiz iş ya da 3 DUZELT turunda bitmeyen iş → kullanıcıya sorulur.
