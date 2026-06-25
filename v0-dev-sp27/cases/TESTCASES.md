# testcase 总览 (test-cases-joker-go.js)

63 个测试 case, 通过 Go HTTP API 调 ofc-go 跑摆牌, 检查是否符合期望.

## 文件位置
`v0-dev/test-cases-joker-go.js` — 唯一 source of truth.

## 调用
```bash
./run-testcase.sh ckpts/X.json [runs=1]
# OR 直接:
node test-cases-joker-go.js http://127.0.0.1:18001
```

## Card 表示

JS object: `{rank, suit, jid?}`
- rank: '2'-'9', 'T', 'J', 'Q', 'K', 'A', 'X' (joker)
- suit: 'c' (clubs)/'d' (diamonds)/'h' (hearts)/'s' (spades)/'j' (joker)
- jid: joker 编号 (0/1, 用于多 joker 区分)

API 字符串: `Kc` / `Td` / `X` / `Xj0` `Xj1`

## 5 个分类

### 1️⃣ 鬼牌专项 (case 1-10)
joker 摆位 + 锁 fantasy 基本逻辑.

### 2️⃣ UR 系列 (case 11-27)
用户实战 R1 弱点. 含 A+joker 联动 / 同色高散底 / TT 配置 / 233 分配 / AcJh+低散.

### 3️⃣ R2-R5 (case 28-50)
进阶决策: 凑底花/连号 / 不破中道 pair / 弃牌权衡 / R5 末轮防 foul.

### 4️⃣ Deck-aware (case 51-58)
桌面已用 A/K 影响 fantasy 选择. UR3 hand + 不同 used 数量.

### 5️⃣ KK + A used (case 59-63)
R2 KK pair, 0/1/2/3/4 A used 各场景看 AI 权衡.

---

## Case 详细 (按用户最新期望)

| # | round | dealt | check 逻辑 | 用户期望 |
|---|---|---|---|---|
| 1 | R1 | X Kc Kd 3h 7s | 鬼顶 + KK 底 | 鬼顶, 37 中, KK 底 |
| 2 | R1 | X Qc 2d 5h 8s | 鬼顶 | 鬼+Q 顶等高牌 |
| 3 | R1 | X Ac Ad 3h 7s | AA 顶 (实对, 不带 joker) | AA 顶进范, 鬼放底/中 |
| 4 | R1 | X X Tc Jc Qc | 1 鬼顶 + 1 鬼分到 mid/bot | 1 鬼顶, 另 1 鬼配 ♣ 凑 SF |
| 5 | R3 | X Kh 7s | 鬼顶或鬼中 | 鬼优先顶/中, 不补底 flush |
| **6** ⚡ | R1 | X Kc Kd Ah As | **AA 顶 (≥2 A, 无 joker) + KK 底** | **AA 上顶 追 A 范, KK 底, 鬼中** |
| 7 | R3 | X Qd 3c | 鬼顶 | 鬼上顶配 9 等高牌 |
| 8 | R1 | X X 2c 3d 4h | 1 鬼顶 + 1 鬼 mid 或 bot | 双鬼分顶底等大牌 |
| 9 | R2 | X 7h 2c | 不弃鬼 + 鬼上顶 | 鬼上顶 |
| 10 | R1 | X Td Jd As 7c | 鬼+A 顶 + TJ ♦ 同行 | A鬼顶 + 3 中 + TJ 同花底 |
| 11 | R1 | 4d 5h Ah As X | A 顶 (AA 或 joker+A) | AA 顶 |
| 12 | R1 | As 4c 8h X 5h | A+joker 一起顶 | A鬼顶 |
| 13 | R1 | 9s 2c X 5h Ac | A+joker 一起顶 | A鬼顶 |
| 14 | R1 | 9c As Qs Js 7h | Js Qs 同色都在底 | JsQs 同色高散底道 |
| 15 | R1 | 4h Ts Kh 3c Qh | Ts 或 Kh 至少一在底 | plan1 K头追范 / plan2 4 hearts 全底 |
| 16 | R1 | 8c 2d Qh 9c Kc | 9c 或 Qh 至少一在底 | 高散至少一底 |
| 17 | R1 | Td Th 3h 9s Ks | TT 在底 + 顶无 3 | TT 保留底道 |
| 18 | R1 | Qd Tc 4s Td 6d | TT 在底 + Qd 在底 | Qd+TT 同底 |
| **19** ⚡ | R1 | 2s 5s 3s Js Ac | **4 ♠ 全底 + Ac 顶** | **底花 + A 顶** |
| 20 | R1 | 3c Td 8s 7h 4d | 7h 8s Td 都在底 | 顺面集底 |
| 21 | R1 | 4d 6c 9h Ac 7d | 9h 在底 | 9h 应底 |
| 22 | R1 | Td Th 3h 9s Ks | TT 底 + 顶无 3 | TT 应底, 3 不上底 |
| 23 | R2 | 4d Qc 6s | mid 仍有 33 | R1 33+T 中 → R2 拿 Q 不替换 |
| **24** ⚡ | R1 | X 3h Ks As 2d | **joker+As 顶 + 23 中 + Ks 底** | **A鬼顶 + 23 中 + K 底 (固定)** |
| 25 | R1 | 3d Js 8s Td 3h | 33 中 + 顶无 3 | 33 保对, 顶不破 |
| 26 | R1 | 2c Th 3c 5c 3h | 33 在中 + Th 在底 | 233 分中底 |
| 27 | R1 | 2s 4d Jh 3c Ac | Ac 顶 + Jh 底 + 2/3/4 ≥2 在中 | AcJh + 低散中 |

⚡ = 严格化 case (按用户最新期望更新, 比旧版严).

### R2-R5 (case 28-50)

| # | 期望 |
|---|---|
| 28 R2 | 加 9♥ 凑底 4 同色 |
| 29 R2 | 不破中 33 (拿 A 别压中) |
| 30 R3 | 不弃 joker |
| 31 R2 | 9♣ 上底凑 open-ended straight |
| 32 R2 | 双鬼 ≥1 上顶 |
| 33 R2 | KK 上底 anchor |
| 34 R2 | 2 ♥ 集中底凑 flush |
| 35 R2 | 5 不上中 (避 mid trips foul) |
| 36 R2 | A 配顶 joker 锁 AA |
| 37 R3 | 弃 9♦ 不凑 mid straight |
| 38 R3 | 9 不上中 (避 trips foul) |
| 39 R3 | 6 不上中 (低牌取小) |
| 40 R3 | 弃 8♦ 不凑 mid flush |
| 41 R3 | 2c 必上底 |
| 42 R3 | 弃 Q, 4 上中 |
| 43 R3 | K 不浪费在顶 (joker+A 已锁) |
| 44 R4 | 9♥ 完成底 straight |
| 45 R4 | 2c 上顶 (K 顶/底 都可) |
| 46 R4 | A 不弃 |
| 47 R4 | 不破底 KK |
| 48 R5 | Q♥ 完成底 flush |
| 49 R5 | 8♥ 上顶 (joker 降为 8 不 foul) |
| 50 R5 | 7♥ 顶 + 8♠ 底凑 straight |

### Deck-aware (case 51-58)

| # | dealt | 桌面 used | 期望 |
|---|---|---|---|
| 51 R1 | 9s 2c X 5h Ac | 0 A used | 鬼+A 顶 |
| 52 R1 | 同上 | 2 A used | 仍鬼+A 顶 (无 AAA 升级) |
| 53 R1 | 同上 | 3 A used | 仍鬼+A 顶 (Ac 是最后 A) |
| 54 R1 | 9s 2c X 5h 8c | 4 A used | joker 单顶等 K/Q |
| 55 R1 | 9s 2c X 5h Kh | 4A+3K used | joker 必上顶 |
| 56 R1 | 9s 2c X 5h Kh | 0 A used | 鬼顶 + Kh 底 |
| 57 R3 | Ah 8h 2h | 3 A used | A+joker 顶 |
| 58 R3 | Ah 8h 2h | 0 A used | A+joker 顶 (锁 AA) |

### KK + A used (case 59-63)

state: top[Qd] mid[5c 6c] bot[3h 9s], R2 dealt: Kh Ks 4d.

| # | A used | 期望 |
|---|---|---|
| 59 | 0 | KK 必上底 等 A/鬼 |
| 60 | 1 | KK 必上底 |
| 61 | 2 | KK 必上底 |
| 62 | 3 | KK 顶或底 (不可中) |
| 63 | 4 | KK 必上顶 锁 fantasy |

## 严格化历史 (用户提的 update)

2026-05-10 前 case 6/19/24 跑分较松, 后改严:

- **case 6**: 旧 = `KK 底 + ≥1 A 顶` (允许 joker+A 顶). 新 = `AA 顶 (实对, 无 joker) + KK 底`.
- **case 19**: 旧 = `4 ♠ 在底`. 新 = `4 ♠ 在底 AND Ac 顶`.
- **case 24**: 旧 = `23 中 + K 底`. 新 = `joker+As 顶 + 23 中 + K 底`.

## 修改 case

要加新 case 或改判断逻辑, 编辑 `test-cases-joker-go.js`. helper functions:

```js
cntJoker(cards)           // 数 joker
cntRank(cards, 'A')       // 数某 rank
cntSuit(cards, 's')       // 数某花色
hasCard(cards, 'A', 'c')  // 是否有 Ac
```

每个 case 接受 `r` (摆牌结果 `{top, middle, bottom, discarded}`), 返回 boolean.

新 case 加完直接 `./run-testcase.sh` 测.

## 历史

- 旧 26 case 时代 (v7_fan): 1.0.3 production round-028 中位 19/26
- 现 63 case (v0 时代): round-004-baseline 最强 56/63 (旧松规则) / 53/63 (新严规则)
