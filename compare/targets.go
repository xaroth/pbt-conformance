package compare

// Target counts of the current league's counted builds per kind, and the least number of them
// that hold each content.
const (
	spellBuilds    = 17
	attackBuilds   = 22
	dotBuilds      = 17
	minionBuilds   = 17
	totemBuilds    = 9
	trapMineBuilds = 9
	// countedBuilds is the total; the builds beyond the kinds' targets are of any kind.
	countedBuilds = 100

	clusterJewelBuilds  = 20
	timelessJewelBuilds = 15
	mercenaryBuilds     = 5
	treeVariantBuilds   = 3
)

// Targets returns how many counted builds of each kind the current league holds at least;
// smoke tests count towards none, and there are no party builds. Other has no target of its
// own: the builds beyond these targets, up to Total, may be of any kind.
func Targets() map[Kind]int {
	return map[Kind]int{
		KindSpell: spellBuilds, KindAttack: attackBuilds, KindDoT: dotBuilds, KindMinion: minionBuilds,
		KindTotem: totemBuilds, KindTrapMine: trapMineBuilds,
	}
}

// Total returns how many counted builds the current league holds.
func Total() int { return countedBuilds }

// Quotas returns how many counted builds of the current league hold each content at least.
func Quotas() map[Content]int {
	return map[Content]int{
		ClusterJewel: clusterJewelBuilds, TimelessJewel: timelessJewelBuilds, Mercenary: mercenaryBuilds,
		TreeVariant: treeVariantBuilds,
	}
}
