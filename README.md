# pbt-conformance

Conformance cases for [pbt-engine](https://github.com/xaroth/pbt-engine): Path of Exile builds per game version, the values Path of Building computes for them, and a comparison against the engine's own results.

Path of Building is the first smoke test, not the source of truth: the game is. A difference between the two is investigated and either fixed in the engine or recorded as a deviation with its evidence.

## Pins

`pins.json` records everything a run needs to be repeated: per game and league, the dataset (version and content id) and the Path of Building release (tag and commit), plus the sources the PoB runtime is built from. A PoB release targets the league of the newest non-variant tree in its `src/GameVersions.lua`; a league's pin is the latest release targeting it, so it stops moving once PoB targets the next league. Only the current league's pin moves.

## Run

The Path of Building runtime is built once into a cache directory, from the sources in `pins.json`: LuaJIT, the lua-utf8 module and a PoB checkout per pinned release. It needs git, make and a C compiler; about a minute the first time.

```sh
go run ./cmd/pbtc setup --cache ~/.cache/pbt-conformance
```

Setup checks that each PoB release targets its pinned league's tree.

## Develop

Requires Go 1.27.

```sh
go build ./...
go test ./...
golangci-lint run
PBTC_CACHE=~/.cache/pbt-conformance go test ./runtime   # also build and check the PoB runtime
```

## Licence

Apache-2.0, see [LICENSE](LICENSE) and [NOTICE](NOTICE).

This product isn't affiliated with or endorsed by Grinding Gear Games in any way. Path of Exile and its game data are the property of Grinding Gear Games.
