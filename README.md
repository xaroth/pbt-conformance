# pbt-conformance

Conformance cases for [pbt-engine](https://github.com/xaroth/pbt-engine): Path of Exile builds per game version, the values Path of Building computes for them, and a comparison against the engine's own results.

Path of Building is the first smoke test, not the source of truth: the game is. A difference between the two is investigated and either fixed in the engine or recorded as a deviation with its evidence.

## Pins

`pins.json` records everything a run needs to be repeated: the [pbt-data](https://github.com/xaroth/pbt-data) commit the datasets come from; per game and league, the dataset (version and content id, checked after pulling) and the Path of Building release (tag and commit); and the sources the PoB runtime is built from. Bumping the pbt-data commit moves every league to the datasets of that commit; their versions and ids in `pins.json` move with it. A PoB release targets the league of the newest non-variant tree in its `src/GameVersions.lua`; a league's pin is the latest release targeting it, so it stops moving once PoB targets the next league. Only the current league's pin moves.

## Run

The Path of Building runtime is built once into a cache directory, from the sources in `pins.json`: LuaJIT, the lua-utf8 module and a PoB checkout per pinned release. It needs git, make and a C compiler; about a minute the first time.

```sh
go run ./cmd/pbtc setup --cache ~/.cache/pbt-conformance
```

Setup checks that each PoB release targets its pinned league's tree.

Each build lives in `builds/<league>/<NN>-<class>-L<level>-<target>/`: `pob.xml` holds the build as complete as PoB has it (tree, items, skills), `pob-treeonly.xml` the same tree without the rest, so a difference can be traced to the tree or the gear. `pbtc pob` runs both through the league's pinned PoB and writes PoB's values to `pob.json`, with the release that made them:

```sh
go run ./cmd/pbtc pob --cache ~/.cache/pbt-conformance            # every pinned league
go run ./cmd/pbtc pob --cache ~/.cache/pbt-conformance --check    # fail if a stored pob.json is stale
```

Rerun it after adding or changing a build, bumping a pin, or changing the runner, and commit the `pob.json` changes with it.

`pbtc compare` computes pbt-engine's sheet for every build with the pinned dataset and compares it with PoB's values for the tree-only build, the part the engine models so far. It reads `pob-treeonly.xml` for class, ascendancy, level, tree, mastery choices, and as config the bandits and the campaign stage (from PoB's resistance penalty), and simulates it with `simulator`. Every simulation warning except stats left out as not modelled (a dropped node, a mastery choice that does not fit, points over budget) is reported as a mismatch `warning:<code>` and must be triaged too. It needs no Lua or PoB, only the datasets, which it pulls from the pinned pbt-data commit into the cache (`<cache>/data/<commit>/<game>`; a later run fetches nothing). `pbtc data` pulls them and prints that directory; `--datasets` compares with datasets already on disk (one directory per league or per version) instead. CI pulls the pinned datasets, runs the tests with them and runs compare.

```sh
go run ./cmd/pbtc compare --cache ~/.cache/pbt-conformance
go run ./cmd/pbtc data --cache ~/.cache/pbt-conformance       # just pull; prints the directory
```

Every mismatch is listed with PoB's tree-only and full values. A mismatch is either fixed in pbt-engine or accepted in `triage.json` as a `deviation` (PoB behaviour we do not follow, with our evidence) or `pending` (a mechanic the engine does not model yet). An entry matches by league, build name pattern and output, and optionally by our value and PoB's (`ours`, `pob`): with values set, a triaged mismatch whose numbers change shows as untriaged again. Compare fails on untriaged mismatches and on triage entries that match nothing. The dataset must be the pinned one: its content id is checked.

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
