package pob

import (
	"maps"
	"slices"
)

// Group is a set of outputs a build passes or fails together.
type Group string

// Groups of outputs.
const (
	// GroupSheet is the character sheet: attributes, pools, defences, resistances, regeneration.
	GroupSheet Group = "sheet"
	// GroupSkill is the main skill's damage.
	GroupSkill Group = "skill"
	// GroupDefence is block, spell suppression and recovery.
	GroupDefence Group = "defence"
	// GroupMinion is the main skill's minion and the minion's own main skill.
	GroupMinion Group = "minion"
	// GroupDelivery is a main skill used by a totem, trap or mine.
	GroupDelivery Group = "delivery"
)

// Groups returns every group, in report order.
func Groups() []Group {
	return []Group{GroupSheet, GroupSkill, GroupDefence, GroupMinion, GroupDelivery}
}

// Output is one output we compare, and how its value is read from PoB's output keys.
type Output struct {
	// Name is our name for the output; for outputs pbt-engine computes, its output key.
	Name  string
	Group Group
	// keys are the PoB output keys value reads.
	keys  []string
	value func(map[string]float64) (float64, bool)
}

// Value returns the output's value from PoB's outputs, and false when PoB gave none.
func (o *Output) Value(outputs map[string]float64) (float64, bool) {
	return o.value(outputs)
}

// minionPrefix marks a PoB output key of the main skill's minion rather than of the player; the
// runner reads it from PoB's outputs of the minion.
const minionPrefix = "Minion."

// Outputs returns the table of every output and its PoB keys, in report order.
func Outputs() []Output {
	out := []Output{
		key("player.strength", GroupSheet, "Str"),
		key("player.dexterity", GroupSheet, "Dex"),
		key("player.intelligence", GroupSheet, "Int"),
		key("player.life", GroupSheet, "Life"),
		key("player.unreserved_life", GroupSheet, "LifeUnreserved"),
		key("player.mana", GroupSheet, "Mana"),
		key("player.unreserved_mana", GroupSheet, "ManaUnreserved"),
		key("player.energy_shield", GroupSheet, "EnergyShield"),
		key("player.armour", GroupSheet, "Armour"),
		key("player.evasion_rating", GroupSheet, "Evasion"),
		key("player.life_regeneration", GroupSheet, "LifeRegen"),
		key("player.mana_regeneration", GroupSheet, "ManaRegen"),
		key("player.energy_shield_regeneration", GroupSheet, "EnergyShieldRegen"),
	}
	for _, e := range []struct{ ours, pob string }{{"fire", "Fire"}, {"cold", "Cold"}, {"lightning", "Lightning"}, {"chaos", "Chaos"}} {
		out = append(out,
			key("player."+e.ours+"_resistance", GroupSheet, e.pob+"Resist"),
			// PoB has no plain maximum resistance key.
			sum("player.maximum_"+e.ours+"_resistance", GroupSheet, e.pob+"Resist", "Missing"+e.pob+"Resist"),
			key("player.uncapped_"+e.ours+"_resistance", GroupSheet, e.pob+"ResistTotal"),
		)
	}
	out = append(out, averageHit("skill", GroupSkill, "")...)
	out = append(out,
		key("skill.hit_chance", GroupSkill, "HitChance"),
		// PoB's hit DPS uses HitSpeed where the skill hits at another rate than it is used.
		first("skill.hits_per_second", GroupSkill, "HitSpeed", "Speed"),
		key("skill.critical_strike_chance", GroupSkill, "CritChance"),
		key("skill.hit_dps", GroupSkill, "TotalDPS"),
		key("skill.dot_dps", GroupSkill, "TotalDotDPS"),
		key("skill.total_dps", GroupSkill, "CombinedDPS"),

		key("player.block_chance", GroupDefence, "BlockChance"),
		key("player.spell_block_chance", GroupDefence, "SpellBlockChance"),
		key("player.spell_suppression_chance", GroupDefence, "SpellSuppressionChance"),
		key("player.life_regeneration_recovery", GroupDefence, "LifeRegenRecovery"),
		key("player.energy_shield_regeneration_recovery", GroupDefence, "EnergyShieldRegenRecovery"),
		key("player.life_leech_rate", GroupDefence, "LifeLeechRate"),
		key("player.energy_shield_leech_rate", GroupDefence, "EnergyShieldLeechRate"),
		key("player.life_recharge", GroupDefence, "LifeRecharge"),
		key("player.energy_shield_recharge", GroupDefence, "EnergyShieldRecharge"),

		key("minion.life", GroupMinion, minionPrefix+"Life"),
	)
	out = append(out, averageHit("minion", GroupMinion, minionPrefix)...)
	return append(out,
		key("minion.hit_chance", GroupMinion, minionPrefix+"HitChance"),
		key("minion.total_dps", GroupMinion, minionPrefix+"CombinedDPS"),

		key("delivery.totem_life", GroupDelivery, "TotemLife"),
		deliveryDPS("delivery.total_dps"),
	)
}

// averageHit returns the average hit outputs of actor ("skill" or "minion"), whose PoB keys
// start with prefix. PoB gives a skill's average hit as AverageHit, and an attack's per hand
// (MainHand.AverageHit, OffHand.AverageHit) without combining them: an attack with both hands
// alternates them or hits with both at once, depending on the skill. So <actor>.average_hit is
// AverageHit, or the one hand's for an attack with one hand, and absent when both hands attack;
// then <actor>.main_hand_average_hit and <actor>.off_hand_average_hit give each hand's.
func averageHit(actor string, g Group, prefix string) []Output {
	whole, mh, oh := prefix+"AverageHit", prefix+"MainHand.AverageHit", prefix+"OffHand.AverageHit"
	bothHands := func(o map[string]float64) bool {
		_, ok1 := o[mh]
		_, ok2 := o[oh]
		return ok1 && ok2
	}
	hand := func(name, k string) Output {
		return Output{Name: name, Group: g, keys: []string{mh, oh}, value: func(o map[string]float64) (float64, bool) {
			v, ok := o[k]
			return v, ok && bothHands(o)
		}}
	}
	return []Output{
		{Name: actor + ".average_hit", Group: g, keys: []string{whole, mh, oh}, value: func(o map[string]float64) (float64, bool) {
			if v, ok := o[whole]; ok {
				return v, true
			}
			if bothHands(o) {
				return 0, false
			}
			if v, ok := o[mh]; ok {
				return v, true
			}
			v, ok := o[oh]
			return v, ok
		}},
		hand(actor+".main_hand_average_hit", mh),
		hand(actor+".off_hand_average_hit", oh),
	}
}

// Keys returns every PoB output key the table reads, sorted.
func Keys() []string {
	return keysOf(Outputs(), Groups())
}

func keysOf(outputs []Output, groups []Group) []string {
	set := map[string]bool{}
	for _, o := range outputs {
		if slices.Contains(groups, o.Group) {
			for _, k := range o.keys {
				set[k] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func key(name string, g Group, k string) Output {
	return Output{Name: name, Group: g, keys: []string{k}, value: func(o map[string]float64) (float64, bool) {
		v, ok := o[k]
		return v, ok
	}}
}

// sum is a + b, given when both are.
func sum(name string, g Group, a, b string) Output {
	return Output{Name: name, Group: g, keys: []string{a, b}, value: func(o map[string]float64) (float64, bool) {
		va, ok1 := o[a]
		vb, ok2 := o[b]
		return va + vb, ok1 && ok2
	}}
}

// first is the first of the keys PoB gives.
func first(name string, g Group, keys ...string) Output {
	return Output{Name: name, Group: g, keys: keys, value: func(o map[string]float64) (float64, bool) {
		for _, k := range keys {
			if v, ok := o[k]; ok {
				return v, true
			}
		}
		return 0, false
	}}
}

// deliveryDPS is the main skill's total DPS with every totem the build places. PoB's hit DPS
// is that of one totem, while its damage over time already counts the stacks all totems
// apply; a trap or mine has no count.
func deliveryDPS(name string) Output {
	const total, hit, count = "CombinedDPS", "TotalDPS", "TotemsSummoned"
	return Output{Name: name, Group: GroupDelivery, keys: []string{total, hit, count}, value: func(o map[string]float64) (float64, bool) {
		t, ok1 := o[total]
		h, ok2 := o[hit]
		n, ok3 := o[count]
		if !ok3 {
			n = 1
		}
		return t + h*(n-1), ok1 && ok2
	}}
}
