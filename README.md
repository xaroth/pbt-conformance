# pbt-conformance

Conformance cases for [pbt-engine](https://github.com/xaroth/pbt-engine): Path of Exile builds per game version, the values Path of Building computes for them, and a comparison against the engine's own results.

Path of Building is the first smoke test, not the source of truth: the game is. A difference between the two is investigated and either fixed in the engine or recorded as a deviation with its evidence.

## Pins

`pins.json` records everything a run needs to be repeated: the [pbt-data](https://github.com/xaroth/pbt-data) commit the datasets come from; per game and league, the dataset (version and content id, checked after pulling) and the Path of Building release (tag and commit); and the sources the PoB runtime is built from. Bumping the pbt-data commit moves every league to the datasets of that commit; their versions and ids in `pins.json` move with it. A PoB release targets the league of the newest non-variant tree in its `src/GameVersions.lua`; a league's pin is the latest release targeting it, so it stops moving once PoB targets the next league. Only the current league's pin moves.

## Run

The Path of Building runtime is built once into a cache directory, from the sources in `pins.json`: LuaJIT, the lua-utf8 module and a PoB checkout per pinned release. It needs git, make and a C compiler, and running PoB needs the system zlib (`libz.so.1`); about a minute the first time.

```sh
go run ./cmd/pbtc setup --cache ~/.cache/pbt-conformance
```

Setup checks that each PoB release targets its pinned league's tree.

Each build lives in `builds/<league>/<NN>-<class>-L<level>-<target>/` and is recorded in three layers, so a difference can be traced to the tree, the gear or the rest:

| File | Layer | `pob.json` key |
|---|---|---|
| `pob.xml` | the build as complete as PoB has it (tree, items, skills, config) | `full` |
| `pob-gear.xml` | the tree with its socketed jewels and the items, without flasks, skills or config | `gear` |
| `pob-treeonly.xml` | the tree only | `treeOnly` |

`pbtc pob` derives `pob-gear.xml` and `pob-treeonly.xml` from `pob.xml`. Both drop every top-level element but `Build`, `Tree` and, for the gear file, `Items`, reset the bandit and pantheon choices on `Build` to `None`, and keep the rest of the file as it is. The gear file also drops the flask slots and their items; the tree-only file drops the jewels from the tree's sockets. It then runs the three files through the league's pinned PoB and writes PoB's values to `pob.json`, with the release that made them:

```sh
go run ./cmd/pbtc pob --cache ~/.cache/pbt-conformance            # every pinned league
go run ./cmd/pbtc pob --cache ~/.cache/pbt-conformance --check    # fail if a stored derived XML or pob.json is stale
```

`pob.json` holds, per layer, PoB's value for each output key the comparison reads, by PoB's key name; a key PoB gives no number for is absent, not zero. The outputs and their PoB keys are listed in one table, `pob.Outputs`, by group:

| Group | Outputs | Layers |
|---|---|---|
| `sheet` | attributes, life, mana, energy shield, armour, evasion, the resistances and their maximums, life, mana and energy shield regeneration, unreserved life and mana | all |
| `defence` | block, spell block, spell suppression, life and energy shield regeneration recovery, life and energy shield leech rates, life and energy shield recharge | all |
| `skill` | the main skill's average hit, chance to hit, hits per second, critical strike chance, hit, damage over time and total damage per second | full |
| `minion` | the main skill's minion's life, and its own main skill's average hit, chance to hit and total damage per second, when the main skill has a minion | full |
| `delivery` | totem life, and total damage per second with every totem placed, when the main skill is used by a totem, trap or mine | full |

The full layer also records the main skill PoB took (`mainSkill`: its socket group, id and name, whether a totem, trap or mine uses it, and its minion). The other layers have no skills of their own, so they record the sheet and defences only. Minion values are read from PoB's outputs of the minion and stored as `Minion.<key>`, and an attack's per-hand values as `MainHand.<key>` and `OffHand.<key>`. The average hit is PoB's `AverageHit`, without the chance to hit, which is its own output. PoB gives an attack's average hit per hand only, and an attack with both hands either alternates them or hits with both at once, depending on the skill; so for an attack with both hands the average hit is recorded per hand (`main_hand_average_hit`, `off_hand_average_hit`) and the single `average_hit` is absent.

A build directory may hold `party.json`, `{"ally": "<build directory>"}`, naming another build of its league whose party buffs it is recorded with too, under `party` in `pob.json`: the full build as PoB's Party tab computes it after importing the ally's exported support. The runner calculates the ally with "Export support" on and loads the auras, curses, warcries, links and enemy conditions and modifiers it exports into the build's Party section, in place of the build's own.

Rerun it after adding or changing a build, bumping a pin, or changing the runner, and commit the derived XMLs and `pob.json` with it. `go test ./pob` fails when a stored `pob-gear.xml` or `pob-treeonly.xml` is not what `pob.xml` derives to.

`pbtc compare` computes pbt-engine's sheet for every build with the pinned dataset and compares it with PoB's values for the tree-only build, the part the engine models so far; it does not read the gear layer yet. It imports `pob-treeonly.xml` with pbt-engine's PoB importer (`pob.FromXML`: class, ascendancy, level, tree, mastery choices, and as config the bandits and the campaign stage from PoB's resistance penalty) and simulates it with `simulator`. It imports `pob.xml` too and checks the importer on both: the tree-only file must import without warnings, and the full file to the same tree (config apart: tree-only files keep PoB's default bandit choice) with warnings only for what it holds beside the tree (items, skills, jewels, config, pantheon). Anything else is a mismatch `import:<code>`, or `import:full-tree` when the trees differ. Every simulation warning except stats left out as not modelled (a dropped node, a mastery choice that does not fit, points over budget) is reported as a mismatch `warning:<code>` and must be triaged too. It needs no Lua or PoB, only the datasets, which it pulls from the pinned pbt-data commit into the cache (`<cache>/data/<commit>/<game>`; a later run fetches nothing). `pbtc data` pulls them and prints that directory; `--datasets` compares with datasets already on disk (one directory per league or per version) instead. CI pulls the pinned datasets, runs the tests with them and runs compare.

```sh
go run ./cmd/pbtc compare --cache ~/.cache/pbt-conformance
go run ./cmd/pbtc data --cache ~/.cache/pbt-conformance       # just pull; prints the directory
```

An output matches when both values are equal as PoB displays them, for the sheet and defences, and within 0.1% of PoB's for the damage outputs; `pob.Outputs` gives each output's precision. Every mismatch is listed with PoB's tree-only and full values. A mismatch is either fixed in pbt-engine or accepted in `triage.json` as a `deviation` (PoB behaviour we do not follow, with our evidence) or `pending` (a mechanic the engine does not model yet). An entry matches by league, build name pattern and output, and optionally by our value and PoB's (`ours`, `pob`): with values set, a triaged mismatch whose numbers change shows as untriaged again. Compare fails on untriaged mismatches and on triage entries that match nothing. The dataset must be the pinned one: its content id is checked.

## Scoreboard

`pbtc compare --scoreboard` follows the mismatch list, unchanged, with the scoreboard; `--json` writes the list and the scoreboard as JSON instead. Both fail as `pbtc compare` does.

```sh
go run ./cmd/pbtc compare --cache ~/.cache/pbt-conformance --scoreboard [--hints hints.json]
go run ./cmd/pbtc compare --cache ~/.cache/pbt-conformance --json > scoreboard.json
```

- Per league, layer and group of outputs, the builds PoB gives that group for, and how many pass, fail and are not compared. A build passes a group when pbt-engine gives every output of it PoB gives and each matches: the sheet and defences as PoB displays them, the others within 0.1%. It fails on a mismatch, or when the engine gives some outputs of the group but not all (incomplete). It is not compared when the engine gives none: the engine simulates only the tree-only file so far, so the gear, full and party layers are not compared. The party layer (the full build with its ally's buffs) is counted only for builds with a `party.json`. The row `checks` counts the builds with an import, warning or allocated-node mismatch.
- Per league and kind of build, the builds that pass every group of every layer and their checks, then the same per content a build holds.
- The outputs the engine lacks in groups it gives other outputs of, by the number of builds.
- What the engine leaves out as not modelled, by the number of builds it appears in, most first: stats with no meaning yet (`unmodelled-stat` warnings of the tree-only simulation), gems and the parts of the full file the importer leaves out, and mechanics a known stat waits for (charges, items, a condition). `--hints` names a JSON file mapping a game stat id to a short text, shown beside that stat.

A build's kind and contents come from `build.json` in its directory; a build without one is `untagged`, and a smoke test such as a level 1 start has `{"smoke": true}` alone, no kind and no contents, and shows as `smoke`:

```json
{"kind": "attack", "contains": ["cluster-jewel", "item-skill"]}
```

| Field | Values |
|---|---|
| `kind` | `spell` (a spell that hits), `attack`, `damage-over-time`, `minion`, `totem`, `trap-mine`, `party`, `other` |
| `smoke` | `true` for a smoke test; then `kind` and `contains` are absent |
| `contains` | `cluster-jewel`, `timeless-jewel`, `mercenary`, `tree-variant` (a Ruthless or alternate tree), `item-skill` (a skill granted by an item, a graft included) |

`go test ./compare` checks every `build.json` against its `pob.xml` (`compare.DetectContents`): a cluster or timeless jewel socketed in the active tree (a timeless jewel by its base or its "are Conquered by" line, which the abyss timeless jewels have), a Ruthless or alternate active tree, and an item PoB uses (flasks apart, weapons of the weapon set in use) with a line that grants or triggers a skill, or anything in a graft slot. `contains` must list exactly those, plus `mercenary` where poe.ninja shows one, which PoB's file does not hold.

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
