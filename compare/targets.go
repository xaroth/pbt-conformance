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
	otherBuilds    = 9

	clusterJewelBuilds  = 20
	timelessJewelBuilds = 15
	mercenaryBuilds     = 5
	treeVariantBuilds   = 3
)

// Targets returns how many counted builds of each kind the current league holds; smoke tests
// count towards none, and there are no party builds.
func Targets() map[Kind]int {
	return map[Kind]int{
		KindSpell: spellBuilds, KindAttack: attackBuilds, KindDoT: dotBuilds, KindMinion: minionBuilds,
		KindTotem: totemBuilds, KindTrapMine: trapMineBuilds, KindOther: otherBuilds,
	}
}

// Quotas returns how many counted builds of the current league hold each content at least.
func Quotas() map[Content]int {
	return map[Content]int{
		ClusterJewel: clusterJewelBuilds, TimelessJewel: timelessJewelBuilds, Mercenary: mercenaryBuilds,
		TreeVariant: treeVariantBuilds,
	}
}
