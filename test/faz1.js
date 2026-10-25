// test/faz1.js — opencode-yaz.js: arguman ayirma, komut kurma, worktree korumalari
// ve --dene uctan uca. Ag yok, opencode cagrilmaz.
//  1 argumanAyir: gecerli girdi, varsayilanlar ve hatalar | 2 komutKur: sabit istem
//  3 worktreeKontrol: ana kopya / main worktree / deneme worktree / git olmayan klasor
//  4 --dene: kopyalar, komutu basar, 0 | 5 --dene ana kopyada: 3, brif olusmaz
"use strict";
const assert = require("assert");
const fs = require("fs");
const os = require("os");
const path = require("path");
const { spawnSync, spawn } = require("child_process");
const KOK = path.join(__dirname, "..");
const YAZ = require(path.join(KOK, "opencode-yaz.js"));
const BETIK = path.join(KOK, "opencode-yaz.js");
let n = 0;
const ok = (ad, f) => { f(); n++; console.log("  ok  " + ad); };
const TMP = fs.mkdtempSync(path.join(os.tmpdir(), "faz1-"));
process.on("exit", () => { try { fs.rmSync(TMP, { recursive: true, force: true }); } catch (e) { /* gecici */ } });

const BRIF = path.join(TMP, "brif.md");
fs.writeFileSync(BRIF, "# gorev\n\nsu dosyayi duzelt.\n", "utf8");

function git(yol, argumanlar) {
  const r = spawnSync("git", ["-C", yol].concat(argumanlar), { encoding: "utf8", windowsHide: true });
  assert.strictEqual(r.status, 0, "git " + argumanlar.join(" ") + ": " + r.stderr);
  return r.stdout;
}

ok("1a argumanAyir: gecerli tam girdi", () => {
  const a = YAZ.argumanAyir(["--brif", BRIF, "--worktree", TMP, "--model", "test/model", "--sure-dk", "7", "--dene"]);
  assert.deepStrictEqual(a, { brif: BRIF, worktree: TMP, model: "test/model", sureDk: 7, dene: true });
});

ok("1b argumanAyir: varsayilan model ve 20 dk", () => {
  const a = YAZ.argumanAyir(["--brif", BRIF, "--worktree", TMP]);
  assert.strictEqual(a.model, YAZ.VARSAYILAN_MODEL);
  assert.strictEqual(a.sureDk, 20);
  assert.strictEqual(a.dene, false);
});

ok("1c argumanAyir: --brif eksik -> hata", () => {
  assert.ok(YAZ.argumanAyir(["--worktree", TMP]).hata);
});

ok("1d argumanAyir: bilinmeyen bayrak -> hata", () => {
  assert.ok(YAZ.argumanAyir(["--brif", BRIF, "--worktree", TMP, "--bilinmez"]).hata);
});

ok("1e argumanAyir: --sure-dk 0 ve abc -> hata", () => {
  assert.ok(YAZ.argumanAyir(["--brif", BRIF, "--worktree", TMP, "--sure-dk", "0"]).hata);
  assert.ok(YAZ.argumanAyir(["--brif", BRIF, "--worktree", TMP, "--sure-dk", "abc"]).hata);
});

ok("1f argumanAyir: degeri eksik bayrak -> hata", () => {
  assert.ok(YAZ.argumanAyir(["--brif", BRIF, "--worktree"]).hata);
});

ok("2 komutKur: argumanlar birebir, istem SABIT_ISTEM", () => {
  const k = YAZ.komutKur({ model: "opencode-go/deepseek-v4.1-flash" });
  assert.strictEqual(k.komut, "opencode");
  assert.deepStrictEqual(k.argumanlar, ["run", "-m", "opencode-go/deepseek-v4.1-flash", YAZ.SABIT_ISTEM]);
  assert.strictEqual(k.argumanlar[3], YAZ.SABIT_ISTEM);
  assert.strictEqual(YAZ.komutKur({}).argumanlar[2], YAZ.VARSAYILAN_MODEL);
  assert.ok(!k.argumanlar[3].includes("su dosyayi duzelt"), "brif icerigi isteme girmemeli");
});

const ANA = path.join(TMP, "ana");
const WT_MAIN = path.join(TMP, "wt-main");
const WT_DENEME = path.join(TMP, "wt-deneme");
fs.mkdirSync(ANA);
git(ANA, ["init", "-q"]);
fs.writeFileSync(path.join(ANA, "dosya.txt"), "ilk\n", "utf8");
git(ANA, ["add", "."]);
git(ANA, ["-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "ilk"]);
git(ANA, ["branch", "-M", "main"]);
git(ANA, ["checkout", "-q", "-b", "yan"]);
git(ANA, ["worktree", "add", "-q", WT_MAIN, "main"]);
git(ANA, ["worktree", "add", "-q", "-b", "deneme", WT_DENEME]);

ok("3a worktreeKontrol: ana kopya reddedilir", () => {
  const r = YAZ.worktreeKontrol(ANA);
  assert.strictEqual(r.tamam, false);
  assert.ok(r.neden);
});

ok("3b worktreeKontrol: main dalindaki ek worktree reddedilir", () => {
  const r = YAZ.worktreeKontrol(WT_MAIN);
  assert.strictEqual(r.tamam, false);
  assert.ok(r.neden);
});

ok("3c worktreeKontrol: deneme dalindaki ek worktree kabul edilir", () => {
  assert.deepStrictEqual(YAZ.worktreeKontrol(WT_DENEME), { tamam: true, dal: "deneme" });
});

ok("3d worktreeKontrol: git olmayan klasor reddedilir", () => {
  const duz = path.join(TMP, "duz");
  fs.mkdirSync(duz);
  const r = YAZ.worktreeKontrol(duz);
  assert.strictEqual(r.tamam, false);
  assert.ok(r.neden);
});

ok("4 --dene: kod 0, brif kopyalanir, komut basilir", () => {
  const r = spawnSync(process.execPath, [BETIK, "--brif", BRIF, "--worktree", WT_DENEME, "--model", "test/model", "--dene"],
    { encoding: "utf8", windowsHide: true });
  assert.strictEqual(r.status, 0, r.stderr);
  const kopya = path.join(WT_DENEME, "OPENCODE-BRIF.md");
  assert.ok(fs.existsSync(kopya), "brif kopyalanmali");
  assert.strictEqual(fs.readFileSync(kopya, "utf8"), fs.readFileSync(BRIF, "utf8"));
  assert.ok((r.stdout + r.stderr).includes("opencode run -m"), r.stdout + r.stderr);
});

ok("5 --dene ana kopyada: kod 3 ve brif olusmaz", () => {
  const r = spawnSync(process.execPath, [BETIK, "--brif", BRIF, "--worktree", ANA, "--dene"],
    { encoding: "utf8", windowsHide: true });
  assert.strictEqual(r.status, 3, r.stderr);
  assert.ok(!fs.existsSync(path.join(ANA, "OPENCODE-BRIF.md")), "ana kopyada brif olusmamali");
});

ok("6a argumanAyir: kabuk enjeksiyonu ve tire ile baslayan model -> hata", () => {
  const kotu = ["x & echo A", "a|b", "$(id)", "x%PATH%", "a b", "a^b", "-x"];
  for (const m of kotu) {
    const a = YAZ.argumanAyir(["--brif", BRIF, "--worktree", TMP, "--model", m]);
    assert.ok(a.hata, "reddedilmeli: " + m);
  }
});

ok("6b argumanAyir: gecerli modeller kabul edilir", () => {
  for (const m of ["opencode-go/deepseek-v4.1-flash", "opencode/deepseek-v4.1-flash-free"]) {
    const a = YAZ.argumanAyir(["--brif", BRIF, "--worktree", TMP, "--model", m]);
    assert.strictEqual(a.hata, undefined, "kabul edilmeli: " + m);
    assert.strictEqual(a.model, m);
  }
});

const WT_DETACHED = path.join(TMP, "wt-detached");
git(ANA, ["worktree", "add", "-q", "--detach", WT_DETACHED]);

ok("7a worktreeKontrol: ayrik HEAD (detached) reddedilir", () => {
  const r = YAZ.worktreeKontrol(WT_DETACHED);
  assert.strictEqual(r.tamam, false);
  assert.ok(r.neden);
});

ok("7b --dene ayrik HEAD worktree: kod 3", () => {
  const r = spawnSync(process.execPath, [BETIK, "--brif", BRIF, "--worktree", WT_DETACHED, "--dene"],
    { encoding: "utf8", windowsHide: true });
  assert.strictEqual(r.status, 3, r.stderr);
  assert.ok(!fs.existsSync(path.join(WT_DETACHED, "OPENCODE-BRIF.md")), "brif olusmamali");
});

ok("8 --dene brif zaten hedefte: kod 0, kopya denenmez, icerik bozulmaz", () => {
  const hedef = path.join(WT_DENEME, "OPENCODE-BRIF.md");
  const once = fs.readFileSync(hedef, "utf8");
  fs.chmodSync(hedef, 0o444);
  let r;
  try {
    r = spawnSync(process.execPath, [BETIK, "--brif", hedef, "--worktree", WT_DENEME, "--dene"],
      { encoding: "utf8", windowsHide: true });
  } finally {
    fs.chmodSync(hedef, 0o666);
  }
  assert.strictEqual(r.status, 0, r.stderr);
  assert.ok((r.stdout + r.stderr).includes("zaten hedefte"), r.stdout + r.stderr);
  assert.strictEqual(fs.readFileSync(hedef, "utf8"), once, "brif icerigi bozulmamali");
});

ok("9a spawnSecenekleri: stdin ignore, shell true, cwd worktree", () => {
  const s = YAZ.spawnSecenekleri(WT_DENEME);
  assert.strictEqual(s.stdio[0], "ignore");
  assert.strictEqual(s.shell, true);
  assert.strictEqual(s.cwd, WT_DENEME);
});

ok("10a spawnSecenekleri: env kopyasi, PWD worktree, process.env bozulmaz", () => {
  const oncekiPwd = process.env.PWD;
  const s = YAZ.spawnSecenekleri(WT_DENEME);
  assert.strictEqual(s.env.PWD, path.resolve(WT_DENEME));
  assert.strictEqual(process.env.PWD, oncekiPwd, "process.env.PWD degismemeli");
  assert.notStrictEqual(s.env, process.env, "env kopya olmali");
  assert.strictEqual(s.env.PATH, process.env.PATH, "diger ortam degiskenleri kopyalanmali");
});

async function okA(ad, f) {
  await f();
  n++;
  console.log("  ok  " + ad);
}

async function asenkronTestler() {
  await okA("9b spawnSecenekleri ile stdin kapali: STDIN-KAPALI", () => new Promise((resolve, reject) => {
    const kod = "process.stdin.on('end',()=>{console.log('STDIN-KAPALI');process.exit(0)});process.stdin.resume();";
    const komut = '"' + process.execPath + '" -e "' + kod + '"';
    const c = spawn(komut, YAZ.spawnSecenekleri(TMP));
    let cikti = "";
    let bitti = false;
    const zaman = setTimeout(() => {
      if (bitti) return;
      bitti = true;
      spawnSync("taskkill", ["/pid", String(c.pid), "/T", "/F"], { stdio: "ignore", windowsHide: true });
      reject(new Error("stdin kapanmadi, surec 10 sn icinde bitmedi"));
    }, 10000);
    c.stdout.on("data", d => { cikti += d.toString(); });
    c.stderr.on("data", d => { cikti += d.toString(); });
    c.on("error", e => {
      if (bitti) return;
      bitti = true;
      clearTimeout(zaman);
      reject(e);
    });
    c.on("close", k => {
      if (bitti) return;
      bitti = true;
      clearTimeout(zaman);
      try {
        assert.strictEqual(k, 0, cikti);
        assert.ok(cikti.includes("STDIN-KAPALI"), cikti);
        resolve();
      } catch (e) { reject(e); }
    });
  }));

  await okA("10b alt surec: miras PWD yok sayilir, worktree gorur", () => new Promise((resolve, reject) => {
    const baska = path.join(TMP, "baska-pwd");
    fs.mkdirSync(baska, { recursive: true });
    const oncekiPwd = process.env.PWD;
    process.env.PWD = baska;
    let c;
    try {
      const kod = "process.stdout.write(String(process.env.PWD))";
      const komut = '"' + process.execPath + '" -e "' + kod + '"';
      c = spawn(komut, YAZ.spawnSecenekleri(WT_DENEME));
    } catch (e) {
      if (oncekiPwd === undefined) delete process.env.PWD;
      else process.env.PWD = oncekiPwd;
      reject(e);
      return;
    }
    if (oncekiPwd === undefined) delete process.env.PWD;
    else process.env.PWD = oncekiPwd;

    let cikti = "";
    let bitti = false;
    const zaman = setTimeout(() => {
      if (bitti) return;
      bitti = true;
      spawnSync("taskkill", ["/pid", String(c.pid), "/T", "/F"], { stdio: "ignore", windowsHide: true });
      reject(new Error("alt surec 10 sn icinde bitmedi"));
    }, 10000);
    c.stdout.on("data", d => { cikti += d.toString(); });
    c.stderr.on("data", d => { cikti += d.toString(); });
    c.on("error", e => {
      if (bitti) return;
      bitti = true;
      clearTimeout(zaman);
      reject(e);
    });
    c.on("close", k => {
      if (bitti) return;
      bitti = true;
      clearTimeout(zaman);
      try {
        assert.strictEqual(k, 0, cikti);
        assert.strictEqual(cikti.trim(), path.resolve(WT_DENEME), cikti);
        resolve();
      } catch (e) { reject(e); }
    });
  }));

  console.log(`\nfaz1: ${n} test gecti`);
}

asenkronTestler().catch(e => {
  console.error(e);
  process.exitCode = 1;
});
