-- Runs build XMLs through Path of Building headless. Reads one XML path per line from stdin and
-- writes one line per build to stdout: "PBTC " followed by JSON with the file, PoB's
-- allocated node count, its main skill and the requested output keys (arguments). A dotted key
-- such as "Minion.<key>" or "MainHand.<key>" is read from that table of PoB's outputs: the main
-- skill's minion's or one hand's. PoB writes its own lines
-- to stdout too; the prefix tells ours apart.
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

local function run(path)
	local f = assert(io.open(path, "rb"))
	local xml = f:read("*a")
	f:close()
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

for path in io.lines() do
	local ok, res = pcall(run, path)
	if not ok then
		res = { file = path, error = tostring(res) }
	end
	io.write("PBTC ", dkjson.encode(res), "\n")
	io.flush()
end
