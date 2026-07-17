# Octavius フロッピー & ブート リファレンス

自分でローダ / ディスクドライバを書くときの参照用メモ。エミュレータ実装
（`floppy.go` / `keyboard.go` / `main.go`）から起こした「今どう動いているか」の記録。

---

## 1. I/O ポート一覧

デバイスは**メモリではなく I/O ポート**（`IN` / `OUT` 命令）でアクセスする。
occ からは組み込みの `__in(port)` / `__out(port, value)` で叩ける。

### フロッピー (`floppy.go`)

| ポート | 名前 | 向き | 意味 |
|--------|------|------|------|
| `0x10` | `CMD`  | write | `0x01`=READ, `0x02`=WRITE, `0x03`=RESET |
| `0x11` | `ARG`  | write | C/H/S を1バイトずつ。**MSB を必ず 1**（`0x80 \| value`）にする |
| `0x12` | `DATA` | read/write | 転送データ1バイト（READ で読む / WRITE で書く） |
| `0x13` | `STAT` | read | ステータス（下記ビット） |

`STAT` のビット:

| bit | 意味 |
|-----|------|
| b0 (`0x01`) | busy |
| b1 (`0x02`) | dataready |
| b2 (`0x04`) | error |
| b3 (`0x08`) | writeprotect |

ジオメトリ: **C**ylinder 0–79, **H**ead 0–1, **S**ector 0–17, 1セクタ 512バイト。
（イメージ全体 = 80×2×18×512 = 1.44 MiB）

### キーボード (`keyboard.go`)

| ポート | 名前 | 向き | 意味 |
|--------|------|------|------|
| `0x0c` | `key_DATA`   | read | 先頭キーのコード。**読むと1つ消費**（前詰め） |
| `0x0d` | `key_BUFFER` | read | バッファ内のキー数（0 なら空） |

---

## 2. 特権について

`IN` / `OUT`（および `LST` など）は**特権命令**。`statsReg` の bit2 が
- `0` → 特権モード（`IN`/`OUT` が実行できる）
- `1` → ユーザモード（`IN`/`OUT` すると割り込み `0x7f` が上がるだけで実行されない）

リセット直後は `statsReg = 0` なので**特権モード**。ブート/ローダ段では普通に叩ける。

---

## 3. タイミングモデル（超重要）

メインループ（`main.go`）は1周でこの順に実行する:

```
tick()        // CPU命令を1個実行
floppyTick()  // フロッピーの状態機械を1ステップ
keyTick()     // キーボード
interrupt()
advancePC()
```

つまり **「1 CPU命令 = 1 floppyTick」**。フロッピーの状態はCPUと別に、1命令ごとに
1ステップ進む。

### ARG の収集のクセ

`floppyTick()` は CMD が READ/WRITE の間、毎ティックこう動く（`args` は収集済み C/H/S）:

```
if ARG != 0 && len(args) < 2:  args に (ARG & 0x7f) を1個追加
else if len(args) == 2:        3個目を追加し、waitTime をセット
if len(args) == 3 && waitTime == 0: STAT |= dataready; DATA = map[...headbyte]
```

**ハンドシェイクが無い**ことに注意。`ARG` ポートの現在値を毎ティック見て、まだ3個
揃っていなければ追加する。したがって:

> **同じ `ARG` 値が複数ティックにわたって載っていると、その値が重複追加される。**
> 正しく C/H/S を渡すには、**3つの `OUT ARG` を「間に他の命令を挟まず」連続実行**
> し、各 `OUT ARG` の直後の1ティックだけでサンプリングさせる必要がある。

ブートROM（後述）は C=H=S=0 しか読まない（3つとも `0x80`）ので、重複しても結果が
同じ＝問題にならない。**任意のセクタを読む汎用ローダを書くときはここが罠。**

⚠️ **occ の `__out` での注意**: `__out(ARG, v)` は `MOV ax,v; MOV dx,0; OUT 0x11,ax`
と複数命令に展開される。`__out(ARG,C); __out(ARG,H); __out(ARG,S)` と並べても、間の
`MOV`（次の値の準備）のティックで**前の ARG が再サンプリングされて重複**する。
→ C/H/S が別値の読み出しは、現状の occ コード生成では素直に書けない。ローダ側は
手書きアセンブラで「値を3レジスタに先読み → OUT ARG 3連発」にするか、ハード側の
ARG プロトコルにハンドシェイク（例: ARG 書き込みで即消費 / busy クリア）を入れるのが筋。

### DATA ストリーミング

`DATA` ポートを `IN`/`OUT` すると `updateFloppyIO()` が走り `headbyte++`。
- READ: 各ティックで `floppyTick` が `DATA = map[...headbyte]` を載せ、`IN DATA` で
  読むと次バイトへ進む。
- WRITE: `OUT DATA` で `map[...headbyte] = DATA` して進む。
- `headbyte` が 511 に達した後のアクセスは `STAT |= error`。1セクタ=512バイト読んだら
  `RESET`（CMD=0x03）で `args`/`headbyte`/`STAT` をクリアして次へ。

---

## 4. 組み込みブートローダ ROM（正典の実例）

`reset()`（`main.go`）が物理アドレス `0xff00` に機械語を焼いており、電源投入時
`cs:ip = 0xff:00` から実行される。命令長は固定4バイト。**これがセクタ0を 0x7c00 に
読み込んで飛ぶ「動く実例」**。相対ジャンプのオフセットは現在命令アドレス基準（バイト）。

```
addr   bytes         disasm                 意味
-----  ------------  ---------------------  --------------------------------------
ff00   01 02 0f 01   MOV bx, 0x01           bx = READ コマンド値
ff04   01 01 0f 80   MOV ax, 0x80           ax = 0x80 (ARG: MSB=1, 値0)
ff08   01 09 0f 7c   MOV ds, 0x7c           ds = 0x7c  (転送先 0x7c00)
ff0c   17 10 02 00   OUT 0x10, bx           CMD = READ
ff10   17 11 01 00   OUT 0x11, ax           ARG = 0x80 → C=0   ┐ 3連続
ff14   17 11 01 00   OUT 0x11, ax           ARG = 0x80 → H=0   │ （間に命令なし）
ff18   17 11 01 00   OUT 0x11, ax           ARG = 0x80 → S=0   ┘
ff1c   16 02 13 00   IN  bx, 0x13           bx = STAT           ← poll ループ先頭
ff20   0d 02 0f 03   CMP bx, 0x03           STAT == busy|dataready ?
ff24   0f 08 00 00   JZ  +8                 揃ったら idx(ff2c) へ
ff28   0e 0f f4 00   JMP -12                まだなら STAT poll へ戻る
ff2c   01 02 0f 00   MOV bx, 0x00           bx = 0 (セグメント内オフセット)
ff30   01 0b 0f 00   MOV flag, 0x00         キャリア等クリア
ff34   16 01 12 00   IN  ax, 0x12           ax = DATA 1バイト     ← バイト読みループ先頭
ff38   01 0c 01 00   MOV [ds:bx], ax        [ds:bx] へ書く
ff3c   15 02 00 00   INC bx                 bx++
ff40   18 08 00 00   JC  +8                 bx が 0xff→0x00 で桁上げ → idx(ff48) へ
ff44   0e 0f f0 00   JMP -16                次バイトへ (ff34 に戻る)
ff48   15 03 00 00   INC cx                 cx++ (セグメント数)
ff4c   0d 03 0f 02   CMP cx, 0x02           2セグメント(=512B)読んだ?
ff50   0f 0c 00 00   JZ  +12                完了なら idx(ff5c) へ
ff54   15 09 00 00   INC ds                 ds++ (次の256Bセグメント)
ff58   0e 0f d8 00   JMP -40                ff30 (MOV flag,0) に戻って続きを読む
ff5c   17 10 03 00   OUT 0x10, cx           CMD に cx を書く（下記注意）
ff60   01 01 0f 00   MOV ax, 0x00
ff64   01 02 01 00   MOV bx, ax             (mode 0x01 = レジスタ間) bx=0
ff68   01 03 01 00   MOV cx, ax             cx=0
ff6c   01 09 01 00   MOV ds, ax             ds=0
ff70   01 0b 01 00   MOV flag, ax           flag=0
ff74   0e 2f 7c 00   JMP 0x7c:0x00          0x7c00 へジャンプ
```

**やっていること**: セクタ0（C0/H0/S0）を512バイト、`0x7c00`〜`0x7dff` に読み込み、
レジスタをクリアして `0x7c00` へジャンプ。→ occ の `.org 0x7c00` 前提と一致。

⚠️ 注意点:
- ポーリングは `STAT == 0x03`（busy=1 かつ dataready=1）でのみ抜ける。`waitTime`
  （5〜24ティックのランダム）を消化するまで dataready が立たない。
- **外側ループは必ず ff30 の `MOV flag, 0` を通ること**。`CMP cx, 2` は cx=1 のとき
  CF を立てたまま残すので、ff34 に直接戻るとバイト読みループの `INC bx` + `JC` が
  即座に誤発火して**257バイト目以降が読めなくなる**（`rom_test.go` が回帰テスト）。
  自前ローダも同様に、CMP の後に回るループの先頭では flag をクリアすること。

---

## 5. 自前ローダを書くときのレシピ

PC/AT 的に「ブートセクタ(512B)が残りを自力ロードする」構成にする場合:

1. **stage-1（セクタ0, 512B以内）**: 上記ROMは0x7c00に置いたstage-1へ飛ぶだけなので、
   stage-1 で `ss`/`sp`/`ds` を張り、フロッピーから stage-2 を任意セグメントへ読み込む。
2. **セクタ読み込みルーチン**（擬似コード。ARG 3連発の制約に注意）:
   ```
   OUT CMD, READ
   ; C,H,S を先にレジスタへ用意しておき、間に命令を挟まず3連発:
   OUT ARG, (0x80|C)
   OUT ARG, (0x80|H)
   OUT ARG, (0x80|S)
   loop: IN r, STAT ; if (r & 0x03) != 0x03 goto loop     ; dataready 待ち
   for i in 0..511: { IN r, DATA ; store r to dst++ ; wait dataready as needed }
   OUT CMD, RESET
   ```
3. **occ から使う場合**: C=H=S が定数（例: 常に同一セクタ）なら `__in`/`__out` で書ける。
   異なる C/H/S を渡す汎用読み出しは §3 の ARG 重複問題があるため、stage-1 の読み込み部
   だけは手書きアセンブラにするのが安全。MMIO（読み込んだバイトの格納先）はポインタで
   書ける: `char *dst = 0x8000; *dst = b;`。

## 6. occ 側で必要になりそうな拡張（メモ）

- ~~`.org` / 転送先アドレスをフラグで可変化~~ → **実装済み**: `occ -org 0x8000`。
  あわせて oasm に `.align N` ディレクティブも追加済み（occ 出力の文字列リテラルの
  後ろにアセンブラを連結するとき、命令フェッチの4バイト整列を保つのに使う）。
- インラインアセンブラ（`asm("...")`）があれば §3 の ARG 3連発を occ 内で表現できる。
