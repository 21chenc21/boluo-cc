# engine/nlhe6 — 6-max NLHE engine

Multi-player (2-6) NLHE state machine. HUNL is the special case NumPlayers=2.

## Design choices

### Why separate package vs extending engine/nlhe
HUNL hard-codes P0/P1, betting close conditions are simpler, no side pots. Extending nlhe with NumPlayers would require rewriting state/action/eval paths and break all HU tests. Parallel package lets us iterate without affecting HUNL stability. Once 6-max stabilizes, consider whether to merge.

Card / hand eval / abstraction (engine/nlhe/abstraction) ARE reused — they don't depend on player count.

### Position model
Positions are **seat-relative-to-button**, not absolute. The "Button" rotates each hand. State stores:
- `Button` (uint8 0..N-1): which seat has the dealer button this hand
- `Seats` ([N]Seat): per-seat state (hole, stack, bet-this-street, folded, all-in)
- `Cur` (uint8): seat currently to act

Canonical position labels (relative to button):
- HU (N=2): button=SB, other=BB. Same SB/BB confusion as HUNL but with `Button` index.
- 6-max (N=6): BTN(0), SB(+1), BB(+2), UTG(+3), MP(+4), CO(+5).

Acting order:
- **Preflop**: first-to-act is BB+1 (=UTG in 6-max, =SB in HU since SB is BB+1 mod 2).
- **Postflop**: first-to-act is BTN+1 (=SB in 6-max, =BB in HU).

### Action close conditions
A street's action round ends when one of:
1. All-but-one players folded → game terminal (fold-win).
2. All non-folded players have either matched the current bet OR are all-in, AND every non-folded non-all-in player has acted at least once this street.

After action closes:
- If river → showdown terminal (deal remaining board if needed, compute side pots).
- Else → advance to next street.

### Side pots
When multiple players go all-in at different stack sizes, contributions cap each pot's eligible winners.

Algorithm (run at terminal showdown):
1. Collect each player's total wagered amount `w_i`.
2. Sort active players by ascending `w_i`.
3. For player at sorted index k, level = w_i. Pot at this level contributes
   `(level - prev_level) × (N_active_at_this_level + N_folded_who_paid)`.
   Folded players' contributions go into the lowest pot they qualified for.
4. Each pot has eligible winners = players whose `w_i >= level`.
5. Within each pot, split among winners with best hand rank (ties split evenly).

### Snapshot / Restore
Like engine/nlhe: O(1) snapshot of mutable fields. Hole cards immutable per game. Board cards tracked via NumBoard.

### Reused from engine/nlhe
- `Card`, `Suit`, `Rank` types (just alias)
- `Evaluate7` (hand eval) — works regardless of player count
- `abstraction` package — card abstractions agnostic to player count

### Reused conceptually but ported
- Action enum (Fold / CheckCall / Bet / AllIn) — same logical structure
- Bet sizes (fraction of pot) — same
- Snapshot/Restore pattern — same

## Test plan
- types_test: position rotation correctness
- state_test: basic flow (2/3/4/5/6 players, preflop close, multi-street)
- sidepot_test: ascending all-in stacks, simultaneous all-in, fold-then-all-in
- showdown_test: split pots, ties, kicker correctness
- heavy_stress: 100k random games × NumPlayers ∈ [2,6], 0 invariant violation
- multiconfig_stress: varying stack depths + bet abstractions

## Reuse readiness for MCCFR
The new state machine API mirrors engine/nlhe (`State`, `Apply`, `Snapshot`, `LegalActions`, `Payoff`, `NeedsBoard`). MCCFR walk in `cfr.go` should port with minor changes:
- Traverse all N players sequentially per iter (was 2 in HUNL)
- Per-player external sampling: traverser expands, all others sample

InfosetID layout will need 3-bit position (was 1-bit). MultiStreetBuckets.ID layout extension to come in W2.

## Status
- 2026-05-25: package skeleton + design.
- W1.1 next: types.go + action.go.
