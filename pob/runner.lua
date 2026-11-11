-- Runs build XMLs through Path of Building headless. Reads one XML path per line from stdin and
-- writes one line per build to stdout: "PBTC " followed by JSON with the file, PoB's
-- allocated node count, its main skill and the requested output keys (arguments). A dotted key
-- such as "Minion.<key>" or "MainHand.<key>" is read from that table of PoB's outputs: the main
-- skill's minion's or one hand's. PoB writes its own lines
-- to stdout too; the prefix tells ours apart.
--
-- A line "<path>\t<ally path>" runs the build with its ally's party buffs, as PoB's Party tab
-- imports them: the ally is calculated with "Export support" on, and the buffs it exports
-- (auras, curses, warcries, links, enemy conditions and modifiers) are loaded into the build's
-- Party section in place of its own.
local keys = { ... }
arg = {} -- HeadlessWrapper reads arg; the keys must not reach it
dofile("HeadlessWrapper.lua")
local dkjson = require("dkjson")

local function lookup(outputs, key)
	local v = outputs
	for part in key:gmatch("[^.]+") do
		if type(v) ~= "table" then
			return nil
		end
		v = v[part]
	end
	return v
end

local function mainSkill()
	local skill = build.calcsTab.mainEnv.player.mainSkill
	local flags = skill.skillFlags or {}
	local res = {
		id = skill.activeEffect.grantedEffect.id,
		name = skill.activeEffect.grantedEffect.name,
		totem = flags.totem or nil,
		trap = flags.trap or nil,
		mine = flags.mine or nil,
	}
	for i, g in ipairs(build.skillsTab.socketGroupList) do
		if g == skill.socketGroup then
			res.group = i
		end
	end
	local minion = skill.minion
	if minion and minion.mainSkill then
		res.minion = { name = minion.minionData.name, skill = minion.mainSkill.activeEffect.grantedEffect.name }
	end
	return res
end

local function read(path)
	local f = assert(io.open(path, "rb"))
	local xml = f:read("*a")
	f:close()
	return xml
end

-- partyOf returns the Party section of the ally's XML with its exported buffs as imported ones.
local function partyOf(allyPath)
	loadBuildFromXML(read(allyPath), allyPath)
	build.partyTab.enableExportBuffs = true
	build.buildFlag = true
	runCallback("OnFrame")
	local party = assert(build:SaveDB("ally"):match("<Party.-</Party>"), "ally exports no party section")
	return (party:gsub("ExportedBuffs", "ImportedBuffs"))
end

-- withParty returns xml with its Party section replaced by party.
local function withParty(xml, party)
	xml = xml:gsub("<Party.-</Party>", ""):gsub("<Party[^>]*/>", "")
	local out, n = xml:gsub("</PathOfBuilding>%s*$", function() return party .. "\n</PathOfBuilding>\n" end)
	assert(n == 1, "no closing PathOfBuilding element")
	return out
end

local function run(line)
	local path, ally = line:match("^([^\t]+)\t(.+)$")
	path = path or line
	local xml = read(path)
	if ally then
		xml = withParty(xml, partyOf(ally))
	end
	loadBuildFromXML(xml, path)
	local allocated = 0
	for _ in pairs(build.spec.allocNodes) do
		allocated = allocated + 1
	end
	local outputs = {}
	for _, k in ipairs(keys) do
		local v = lookup(build.calcsTab.mainOutput, k)
		if type(v) == "number" then
			outputs[k] = v
		end
	end
	return { file = path, allocated = allocated, outputs = outputs, mainSkill = mainSkill() }
end

for line in io.lines() do
	local ok, res = pcall(run, line)
	if not ok then
		res = { file = line:match("^[^\t]+"), error = tostring(res) }
	end
	io.write("PBTC ", dkjson.encode(res), "\n")
	io.flush()
end
