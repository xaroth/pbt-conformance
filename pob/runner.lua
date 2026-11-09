-- Runs build XMLs through Path of Building headless. Reads one XML path per line from stdin and
-- writes one line per build to stdout: "PBTC " followed by JSON with the file, PoB's
-- allocated node count and the requested output keys (arguments). PoB writes its own lines
-- to stdout too; the prefix tells ours apart.
local keys = { ... }
arg = {} -- HeadlessWrapper reads arg; the keys must not reach it
dofile("HeadlessWrapper.lua")
local dkjson = require("dkjson")

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
		local v = build.calcsTab.mainOutput[k]
		if type(v) == "number" then
			outputs[k] = v
		end
	end
	return { file = path, allocated = allocated, outputs = outputs }
end

for path in io.lines() do
	local ok, res = pcall(run, path)
	if not ok then
		res = { file = path, error = tostring(res) }
	end
	io.write("PBTC ", dkjson.encode(res), "\n")
	io.flush()
end
