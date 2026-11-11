package pob

import "testing"

// The runner ID changes with the runner and with the keys it reads.
func TestRunnerID(t *testing.T) {
	t.Parallel()
	keys := Keys()
	base := runnerID(runner, keys)
	if base != RunnerID() {
		t.Errorf("RunnerID %s, want %s", RunnerID(), base)
	}
	if runnerID(append([]byte("-- changed\n"), runner...), keys) == base {
		t.Error("same ID for another runner")
	}
	if runnerID(runner, keys[1:]) == base {
		t.Error("same ID for other keys")
	}
	if runnerID(runner, []string{"ab", "c"}) == runnerID(runner, []string{"a", "bc"}) {
		t.Error("same ID for keys split differently")
	}
}
