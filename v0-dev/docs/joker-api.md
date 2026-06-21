# 鬼牌(Joker)传参 API 规范

> 2026-06-21 定。背景:prod 一直传裸 `"X"`,导致两类 bug —— ① `computeDeckRemaining` 鬼双计(盘上鬼 `Xj0` + usedCards `"X"` 同物两个 key);② `jokersInDeck` 特征(f[36])只按 canonical key `Xj0`-`Xj3` 查,裸 `"X"` 命中不了 → 以为鬼没被用 → value-head 偏。训练全程用 canonical(`PlaceCard` 把盘上鬼变 `Xj0`),所以 **prod 必须也传 canonical 才对齐训练**。

## 核心约定
**每张物理鬼分配一个唯一稳定 ID:`Xj0`、`Xj1`(2 鬼局)/ `Xj0`~`Xj3`(4 鬼局)。同一张鬼在任何字段都用同一个 ID,不同鬼用不同 ID。绝不传裸 `"X"`。**

- 鬼**可互换**(`Xj0` 与 `Xj1` 在 value-head 里完全等价,已实测 te 一致),所以编号本身随意,只要**唯一 + 一致**。
- 按出现顺序编号即可:第一张出现的鬼 = `Xj0`,第二张 = `Xj1`……

## 各字段
| 字段 | 鬼怎么传 |
|---|---|
| `state.top/middle/bottom`(本家盘)| 用该鬼 ID,如 `"Xj0"`。`PlaceCard` 保留不重分配。 |
| `dealt`(当轮发牌)| 用 ID,且跟盘上/已出的鬼**用不同编号**(它是不同物理鬼)。 |
| `usedCards`(出了库、但**不在本家盘**)| **你历轮的弃牌 + 对手可见的牌**,各一个唯一 ID。 |

### usedCards 到底放什么(重要,易错)
`computeDeckRemaining` 把"已用牌" = **`usedCards` ∪ 你的 `top/mid/bot` 盘**(并集去重)。
**你盘上的牌它直接从盘数了** → `usedCards` 不要再列一遍(非鬼自动去重无害,但鬼因为盘上是 `Xj0`、若你传裸 `"X"` 去重不掉 → 双计)。

> **规则:`usedCards` = 牌库已没有、但「既不在你 `top/mid/bot` 盘上、也不是当轮手牌 `dealt`」的牌。**
> = **你历轮弃掉的牌 + 对手可见的牌**。
> 排除两类(已在别的字段传):① 当轮刚发的 → `dealt`;② 你盘上已摆的 → `top/mid/bot`。

具体到"上一轮发的鬼"(或任何历轮的牌)——看它现在去哪了:

| 历轮发的牌/鬼 | 现在在哪 | 写不写 usedCards |
|---|---|---|
| **摆到盘上了** | 在 `top/mid/bot` | ❌ 别写(盘里已数,写了双计) |
| **弃掉了** | 不在盘上 | ✅ 要写(出库了但盘上没有,不写牌库会当它还在) |

即:**不是"历轮发的都不写";是「摆盘上的不写、弃掉的要写」**。对手可见的牌同理(不在你盘上)→ 要写。

## 三条铁律
1. **每张鬼唯一 ID** —— 两张不同鬼用同一个 `Xj0` → 撞 key 只算 1 张。
2. **本家盘上的鬼别再进 usedCards** —— 盘里已传(`PlaceCard` 会算),re-list 就是双计。
3. **绝不传裸 `"X"`** —— ① 多个 `"X"` 进 `map[string]bool` 会合并成 1 个;② `jokersInDeck` 等特征只认 `Xj0`-`Xj3`,裸 `"X"` 命中不了 → 特征错。

## jokerRem / jokersInDeck 语义
- `computeDeckRemaining` 数鬼 = `jokerCount − 不同鬼 ID 个数`(跨 board + usedCards 去重)。
- `jokersInDeck`(特征 f[36])= `4 − (UsedCards 里 Xj0..Xj3 命中数)`(上界口径,4 是固定基数,不是本局鬼数)。
- 两者都**只对 canonical key 正确**。

## 例子(2 鬼局,`jokerCount=2`)
```jsonc
// 2 鬼都在本家盘
{ "state": { "top": ["Xj0", "..."], "middle": ["Xj1","..."], "usedCards": ["...无鬼..."] } }   // jokerRem=0

// 2 鬼都是对手的(本家盘无鬼)
{ "state": { "top": [], "usedCards": ["Xj0", "Xj1", "..."] } }                                 // jokerRem=0

// 1 本家盘 + 1 对手
{ "state": { "top": ["Xj0","..."], "usedCards": ["Xj1", "..."] } }                             // jokerRem=0

// 本家盘 1 鬼,牌库还剩 1
{ "state": { "top": ["Xj0","..."], "usedCards": ["...无鬼..."] } }                             // jokerRem=1

// 当轮发到鬼(盘上已有 Xj0)
{ "dealt": ["Xj1", "..."], "state": { "top": ["Xj0","..."] } }                                // 摆上后盘有 Xj0,Xj1
```

## 为什么服务端不能自动修
服务端收到 usedCards 的 `"X"` 时,**分不清它是"本家盘上鬼的 re-list"还是"对手的鬼"** —— 前者该忽略(盘已算)、后者该计入。所以无法可靠 canonical 化。**必须前端按本规范传。**

## 迁移
- 旧 prod / 旧 case 传裸 `"X"`。服务端有个临时安全网(`computeDeckRemaining` 盘上有鬼时跳裸 `"X"`,治本家盘双计),但治不了 `jokersInDeck` 的 f[36] 错、也治不了 map 合并。
- **彻底干净 = 前端 + case 全部改 canonical `Xj0/Xj1`。**
