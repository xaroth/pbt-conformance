# pbt-conformance

Conformance cases for [pbt-engine](https://github.com/xaroth/pbt-engine): Path of Exile builds per game version, the values Path of Building computes for them, and a comparison against the engine's own results.

Path of Building is the first smoke test, not the source of truth: the game is. A difference between the two is investigated and either fixed in the engine or recorded as a deviation with its evidence.

## Pins

`pins.json` records everything a run needs to be repeated: per game and league, the dataset (version and content id) and the Path of Building release (tag and commit); and the sources the PoB runtime is built from. Datasets are pulled by id from the bucket of [pbt-data](https://github.com/xaroth/pbt-data) at `https://pbt-data.xaroth.nl/`; moving a league to a newer dataset means pinning that dataset's version and id, as the bucket's `index.json` lists them. A PoB release targets the league of the newest non-variant tree in its `src/GameVersions.lua`; a league's pin is the latest release targeting it, so it stops moving once PoB targets the next league. Only the current league's pin moves.

## Run

The Path of Building runtime is built once into a cache directory, from the sources in `pins.json`: LuaJIT, the lua-utf8 module and a PoB checkout per pinned release. It needs git, make and a C compiler, and running PoB needs the system zlib (`libz.so.1`); about a minute the first time.

```sh
go run ./cmd/pbtc setup --cache ~/.cache/pbt-conformance
```

Setup checks that each PoB release targets its pinned league's tree.

Each build lives in `builds/<league>/<NN>-<class>-L<level>-<target>/` and is recorded in four layers, so a difference can be traced to the tree, the gear, the main skill or the rest:

| File | Layer | `pob.json` key |
|---|---|---|
| `pob.xml` | the build as complete as PoB has it (tree, items, skills, config) | `full` |
| `pob-skill.xml` | the gear layer with the main skill alone against the enemy | `skill` |
| `pob-gear.xml` | the tree with its socketed jewels and the items, without flasks, config or skills of its own | `gear` |
| `pob-treeonly.xml` | the tree only | `treeOnly` |

`pbtc pob` derives `pob-skill.xml`, `pob-gear.xml` and `pob-treeonly.xml` from `pob.xml`. They drop every top-level element but `Build`, `Tree`, for the gear and skill files `Items` and `Skills`, and for the skill file `Config`; reset the bandit and pantheon choices on `Build` to `None`; and keep the rest of the file as it is. The gear and skill files also drop the flask slots and their items; the tree-only file drops the jewels from the tree's sockets. The gear file keeps, of the active skill set, only the groups granted by items or the tree, disabled with their gems: PoB would add them back enabled, and it calculates the group `mainSocketGroup` points at even when that group is disabled. So the gear layer's main skill is PoB's default attack. The skill file keeps, of the active skill set, the main socket group; the groups granted by items or the tree, disabled, since PoB would add them back enabled; and the other enabled groups with only their support gems enabled, since PoB applies supports across groups (to a skill an item grants, and through linked slots). A gem is a support when its `skillId` starts with `Support`. It renumbers `mainSocketGroup` to match, and of the active config set it keeps the inputs of PoB's "Enemy Stats" section. So the main skill is calculated with the supports it has in the full build and without auras, curses, other buffs, flasks or conditions; `go test ./pob` checks that every build's skill layer has the full layer's main skill, level, quality and supports. It then runs the four files through the league's pinned PoB and writes PoB's values to `pob.json`, with the release that made them:

```sh
go run ./cmd/pbtc pob --cache ~/.cache/pbt-conformance            # every pinned league
go run ./cmd/pbtc pob --cache ~/.cache/pbt-conformance --check    # fail if a stored derived XML or pob.json is stale
```

`pob.json` holds, per layer, PoB's value for each output key the comparison reads, by PoB's key name; a key PoB gives no number for is absent, not zero. The outputs and their PoB keys are listed in one table, `pob.Outputs`, by group:

| Group | Outputs | Layers |
|---|---|---|
| `sheet` | attributes, life, mana, energy shield, armour, evasion, the resistances and their maximums, life, mana and energy shield regeneration, unreserved life and mana, movement speed | all |
| `reservation` | the life and mana every skill reserves: PoB's pool less its unreserved part | all |
| `defence` | block, spell block, spell suppression, life and energy shield regeneration recovery, life and energy shield leech rates, life and energy shield recharge | all |
| `skill` | the main skill's average hit, chance to hit, hits per second, critical strike chance, hit, damage over time and total damage per second | skill, full |
| `hit-types` | the main skill's hit damage per damage type (physical, lightning, cold, fire, chaos), minimum and maximum | skill, full |
| `minion` | the main skill's minion's life, and its own main skill's average hit, chance to hit and total damage per second, when the main skill has a minion | skill, full |
| `delivery` | totem life, and total damage per second with every totem placed, when the main skill is used by a totem, trap or mine | skill, full |

The full and skill layers also record the main skill PoB took (`mainSkill`: its socket group, id and name, whether it is an attack, whether a totem, trap or mine uses it, its level and quality, its minion, and the supports PoB applied to it with their level and quality, sorted by id since PoB's own order varies between runs). The gear and tree-only layers have no skills, so they record the sheet and defences only. Minion values are read from PoB's outputs of the minion and stored as `Minion.<key>`, and an attack's per-hand values as `MainHand.<key>` and `OffHand.<key>`. The average hit is PoB's `AverageHit`, without the chance to hit, which is its own output. PoB gives an attack's average hit per hand only, and an attack with both hands either alternates them or hits with both at once, depending on the skill; so for an attack with both hands the average hit is recorded per hand (`main_hand_average_hit`, `off_hand_average_hit`) and the single `average_hit` is absent. The hit damage per type follows the same rule (`physical_minimum`, `main_hand_physical_minimum`, and so on).

A build directory may hold `party.json`, `{"ally": "<build directory>"}`, naming another build of its league whose party buffs it is recorded with too, under `party` in `pob.json`: the full build as PoB's Party tab computes it after importing the ally's exported support. The runner calculates the ally with "Export support" on and loads the auras, curses, warcries, links and enemy conditions and modifiers it exports into the build's Party section, in place of the build's own.

Rerun it after adding or changing a build, bumping a pin, or changing the runner, and commit the derived XMLs and `pob.json` with it. `go test ./pob` fails when a stored `pob-gear.xml` or `pob-treeonly.xml` is not what `pob.xml` derives to.

`pbtc compare` computes pbt-engine's sheet for every build with the pinned dataset and compares its sheet and defences with PoB's values for the tree-only build, the part the engine models so far; it does not read the gear layer yet. Once the engine gives outputs that read skills (the groups recorded only with a main skill, and reservation), compare also imports and simulates `pob-skill.xml` and `pob.xml`, compares those outputs with PoB's for the skill layer and judges them on the skill and full layers; until then it simulates neither. It imports `pob-treeonly.xml` with pbt-engine's PoB importer (`pob.FromXML`: class, ascendancy, level, tree, mastery choices, and as config the bandits and the campaign stage from PoB's resistance penalty) and simulates it with `simulator`. It imports `pob.xml` too and checks the importer on both: the tree-only file must import without warnings, and the full file to the same tree (config apart: tree-only files keep PoB's default bandit choice) with warnings only for what it holds beside the tree (items, skills, jewels, config, pantheon). Anything else is a mismatch `import:<code>`, or `import:full-tree` when the trees differ. Every simulation warning (a dropped node, a mastery choice that does not fit, points over budget) is reported as a mismatch `warning:<code>` and must be triaged too, except stats left out as not modelled and choices PoB ignores too: an override on a node that is not allocated, and a mastery effect on a mastery an override replaces. It needs no Lua or PoB, only the datasets, which it pulls by their pinned ids from pbt-data's bucket into the cache (`<cache>/data/<game>/<league>`; a later run fetches nothing). `pbtc data` pulls them and prints that directory; `--datasets` compares with datasets already on disk (one directory per league or per version) instead. CI pulls the pinned datasets, runs the tests with them and runs compare.

```sh
go run ./cmd/pbtc compare --cache ~/.cache/pbt-conformance
go run ./cmd/pbtc data --cache ~/.cache/pbt-conformance       # just pull; prints the directory
```

An output matches when both values are equal as PoB displays them, for the sheet and defences, and within 0.1% of PoB's for the damage outputs; `pob.Outputs` gives each output's precision. PoB gives no unreserved life or mana when the pool is 0; the comparison reads that as 0. Every mismatch is listed with PoB's tree-only and full values. A mismatch is either fixed in pbt-engine or accepted in `triage.json` as a `deviation` (PoB behaviour we do not follow, with our evidence) or `pending` (a mechanic the engine does not model yet). An entry matches by league, build name pattern and one output, for an `import:` or `warning:` output also by a pattern on the mismatch's subject (`subject`, such as `maximum_life_+%_if_no_life_tags_on_body_armour from *`), and optionally by our value and PoB's (`ours`, `pob`): with values set, a triaged mismatch whose numbers change shows as untriaged again. An entry without an output, or an import or warning entry without a subject, is rejected, so no entry accepts whatever a build brings. An entry can instead list `cases`, for one cause that many builds share: each case names a build exactly, an output, and for an output with values our value and PoB's, `pob` left out when PoB gives none, or for an import or warning output its exact subject. A case matches only those values or that subject, so a changed number or a new note shows as untriaged, and every case must match: one that matches nothing is stale like an entry that matches nothing.

```json
{"id": "tattoos", "kind": "pending", "reason": "Tattoos replace tree nodes; the importer leaves them out",
 "cases": [{"build": "40-witch-L100-occultist-winter-orb", "output": "import:pob-override-ignored", "subject": "node 6230"},
           {"build": "40-witch-L100-occultist-winter-orb", "output": "player.life", "ours": 4012, "pob": 4310}]}
```

Compare fails on untriaged mismatches and on triage entries or cases that match nothing. The dataset must be the pinned one: its content id is checked.

## Scoreboard

`pbtc compare --scoreboard` follows the mismatch list, unchanged, with the scoreboard; `--json` writes the list and the scoreboard as JSON instead. Both fail as `pbtc compare` does.

```sh
go run ./cmd/pbtc compare --cache ~/.cache/pbt-conformance --scoreboard [--hints hints.json]
go run ./cmd/pbtc compare --cache ~/.cache/pbt-conformance --json > scoreboard.json
```

- Per league, layer and group of outputs, the builds PoB gives that group for, and how many pass, fail and are not compared. A build passes a group when pbt-engine gives every output of it PoB gives and each matches: the sheet and defences as PoB displays them, the others within 0.1%. It fails on a mismatch, or when the engine gives some outputs of the group but not all (incomplete). It is not compared when the engine gives none: the engine gives the tree-only file's sheet and defences and no skill outputs so far, so the gear, skill, full and party layers are not compared. The party layer (the full build with its ally's buffs) is counted only for builds with a `party.json`. The row `checks` counts the builds with an import, warning or allocated-node mismatch.
- Per league and kind of build, the builds that pass every group of every layer and their checks, then the same per content a build holds.
- The outputs the engine lacks in groups it gives other outputs of, by the number of builds.
- What the engine leaves out as not modelled, by the number of builds it appears in, most first: stats with no meaning yet (`unmodelled-stat` warnings of the tree-only simulation), the gems of the full file (imported, not simulated yet) under the mechanic `skills`, the parts of the full file the importer leaves out, and mechanics a known stat waits for (charges, items, a condition). `--hints` names a JSON file mapping a game stat id to a short text, or to an object whose `hint` field is that text (other fields are ignored), shown beside that stat. Without `--hints` the hint column stays empty.

A build's kind and contents come from `build.json` in its directory; a build without one is `untagged`, and a smoke test such as a level 1 start has `{"smoke": true}` alone, no kind and no contents, and shows as `smoke`. A build taken from poe.ninja also names its origin:

```json
{"kind": "attack", "contains": ["cluster-jewel", "item-skill"],
 "origin": {"url": "https://poe.ninja/poe1/builds/allflame/character/<account>/<character>", "league": "Allflame", "snapshot": "0208-20261111-47167"}}
```

| Field | Values |
|---|---|
| `kind` | `spell` (a spell that hits), `attack`, `damage-over-time`, `minion`, `totem`, `trap-mine`, `party` |
| `smoke` | `true` for a smoke test; then `kind` and `contains` are absent |
| `contains` | `cluster-jewel`, `timeless-jewel`, `mercenary`, `tree-variant` (a Ruthless or alternate tree), `item-skill` (a skill granted by an item, a graft included) |
| `origin` | where the build was taken from: the character's page, the league's name and the version of poe.ninja's data that was read |

A build's kind follows from what PoB records for it in the full layer (`pob.json`), in this order: no skill of its own (PoB's default attack) is `smoke`; a main skill used by a totem is `totem`, by a trap or mine `trap-mine`; one with a minion is `minion`; otherwise `damage-over-time` when damage over time (ignite, bleed, poison and the like) is at least half of PoB's combined damage per second (`(CombinedDPS - TotalDPS) / CombinedDPS >= 0.5`), and else `attack` when PoB marks the skill an attack and `spell` otherwise (`compare.DetectKind`). `go test ./compare` checks that every 3.29 build has a `build.json` and that its kind follows this rule, and that the league holds `compare.Total` (100) counted builds, at least `compare.Targets` of each kind, every one of a kind with a target, and at least `compare.Quotas` of each content.

`go test ./compare` checks every `build.json` against its `pob.xml` (`compare.DetectContents`): a cluster or timeless jewel socketed in the active tree (a timeless jewel by its base or its "are Conquered by" line, which the abyss timeless jewels have), a Ruthless or alternate active tree, and an item PoB uses (flasks apart, weapons of the weapon set in use) with a line that grants or triggers a skill, or anything in a graft slot. `contains` must list exactly those, plus `mercenary` where poe.ninja shows one, which PoB's file does not hold.

## Picking builds

`pbtc ninja` picks builds for a league (default: the current one) from poe.ninja, by popularity, and adds them with their origin:

```sh
go run ./cmd/pbtc ninja --cache ~/.cache/pbt-conformance --overview allflame --variant allflamer --dry-run
go run ./cmd/pbtc ninja --cache ~/.cache/pbt-conformance --overview allflame --variant allflamer
go run ./cmd/pbtc pob --cache ~/.cache/pbt-conformance
```

`--overview` names poe.ninja's league, whose passive tree must be the build league's; `--variant` a league on its Ruthless or alternate tree. The candidates are, in this order: for each of the `--skills` (120) most popular main skills, the first characters of `--per-skill` (2) different ascendancies poe.ninja lists for it; the same for the `--mercenaries` (5) most popular mercenary classes; then the first `--variant-rows` (20) characters of different ascendancies of the variant league. Each candidate's PoB build runs through the league's pinned PoB, which gives its kind as above and its main skill; `compare.DetectContents` gives its contents, and poe.ninja whether it has a mercenary. A candidate is listed and not picked when poe.ninja does not give its character (a 4xx such as a 404 for a removed character, a 429 or 5xx that outlasts the retries, or an unreadable answer), when PoB fails to load it, or when PoB gives its main skill no damage (for a minion skill, the minion's). Builds without damage are mostly ones PoB cannot read from the export (a spectre build, for one, without the spectre, which poe.ninja does not name), so none is picked.

The pick is deterministic for a given set of responses. In candidate order, one build per main skill and ascendancy, a candidate is picked while its kind is short of its target (`compare.Targets`; the counted builds already in the league count too). Then candidates of any kind are picked in the same order until the league has `compare.Total` (100) counted builds. Then, for each content short of its quota (`compare.Quotas`), the first unpicked candidate with it replaces the last picked candidate that lacks it, holds no content at or under its own quota, and is of the same kind or of a kind above its target. `--dry-run` prints the picks (kind, main skill, ascendancy, level, contents, source), the counts per kind against the targets, the total, the counts per content and the candidates not picked with the reason, and writes nothing; without it the picks are written as `builds/<league>/<NN>-<class>-L<level>-<ascendancy>-<skill>/` with `pob.xml` and `build.json`, numbered after the existing builds, and `pbtc pob` records them.

Requests go to poe.ninja's builds API (the league index, searches, dictionaries and characters) one at a time with a descriptive User-Agent, at least 1.5 s apart, and back off on 429 and 5xx (honouring `Retry-After`, else from 5 s doubling, six attempts). Responses are cached under `<cache>/ninja/http` by their URL, the league's data version included: a 200 response by its body, a 4xx other than 429 by its status, which later runs return as the same failure. A rerun makes none of those requests again; only a 429 or 5xx that outlasted the retries is asked for again. Cache files are written to a temporary file and renamed, so an interrupted run leaves none half written. PoB's results are cached under `<cache>/ninja/pob/<PoB commit>-<runner id>`, the runner id a hash of the runner and the output keys it reads (`pob.RunnerID`). Delete the cache to read a newer snapshot.

## Import cases

`imports/<case>/` checks pbt-engine's PoB importer on what the conformance builds lack: cluster jewels, a tattoo, two trees, player config, the layout of older PoB files, the ruthless and alternate trees, a secondary ascendancy and a PoE2 file. Each case holds `pob.xml`, made from a conformance build's `pob-treeonly.xml` with one thing added (its `README.md` says what), and `expect.json`: the build it must import to, which is the source build with only the intended change, and its warnings by code and subject, or the error. `go test ./imports` runs them with `$PBT_DATASETS`, as CI does.

## Develop

Requires Go 1.27.

```sh
go build ./...
go test ./...
golangci-lint run
PBTC_CACHE=~/.cache/pbt-conformance go test ./runtime ./pob   # also build the PoB runtime and run PoB
PBT_DATASETS=$(go run ./cmd/pbtc data --cache ~/.cache/pbt-conformance) PBTC_CACHE=~/.cache/pbt-conformance go test ./compare   # also compare with real datasets
```

## Licence

Apache-2.0, see [LICENSE](LICENSE) and [NOTICE](NOTICE).

This product isn't affiliated with or endorsed by Grinding Gear Games in any way. Path of Exile and its game data are the property of Grinding Gear Games.
