package compare

import "github.com/xaroth/pbt-conformance/pob"

// dotShare is the share of PoB's combined damage per second at or above which a build's kind is
// damage over time.
const dotShare = 0.5

// DetectKind returns a build's kind from PoB's result for its full XML: no skill of its own is
// Smoke; a skill used by a totem is a totem, by a trap or mine a trap or mine; one with a minion
// a minion; then damage over time when it is at least half the combined damage per second, and
// else attack or spell by the skill.
func DetectKind(full pob.Result) Kind {
	ms := full.MainSkill
	o := full.Outputs
	switch {
	case ms == nil || ms.Group == 0:
		return Smoke
	case ms.Totem:
		return KindTotem
	case ms.Trap || ms.Mine:
		return KindTrapMine
	case ms.Minion != nil:
		return KindMinion
	case o["CombinedDPS"] > 0 && (o["CombinedDPS"]-o["TotalDPS"])/o["CombinedDPS"] >= dotShare:
		return KindDoT
	case ms.Attack:
		return KindAttack
	}
	return KindSpell
}
