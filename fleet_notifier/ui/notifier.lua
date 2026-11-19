--[[
  Fleet Order Notifier -- UI side.

  Loaded into the menus Lua environment through ../ui.xml. The Mission Director
  script (../md/fleet_orders.xml) raises one string per finished order, split by
  "|" in this order:

      shipcode | shipname | ordername | sectorname | captainname

  The fields are forwarded as JSON to the Go server with the djfhe_http
  extension. See ../../README.md for setup.
]]

-- Go server address. It runs on the machine that runs X4, so 127.0.0.1 is
-- correct; use the LAN address if you move the server to another host.
local ENDPOINT = "http://127.0.0.1:47100/x4/order-finished"

-- Must match X4_TOKEN on the server. Leave "" when X4_TOKEN is not set.
local TOKEN = ""

local function log(msg)
    DebugError("[FleetNotifier] " .. tostring(msg))
end

-- djfhe_http sets package.path when its own lua file is loaded. Extension load
-- order is not guaranteed, so resolve it on first use instead of at load time.
local Request

local function getRequest()
    if Request then
        return Request
    end

    local ok, mod = pcall(require, "djfhe.http.request")
    if not ok then
        log("djfhe_http is not available, no notifications will be sent (" .. tostring(mod) .. ")")
        return nil
    end

    Request = mod
    return Request
end

local function splitFields(text)
    local fields = {}
    for field in string.gmatch(text .. "|", "([^|]*)|") do
        fields[#fields + 1] = field
    end
    return fields
end

local function onOrderFinished(_, param)
    log("order finished event: " .. tostring(param))

    if type(param) ~= "string" or param == "" then
        return
    end

    local parts = splitFields(param)
    local payload = {
        code = parts[1] or "",
        ship = parts[2] or "",
        order = parts[3] or "",
        sector = parts[4] or "",
        captain = parts[5] or "",
    }

    if payload.ship == "" then
        log("dropping event without a ship name: " .. tostring(param))
        return
    end

    local requestModule = getRequest()
    if not requestModule then
        return
    end

    local request = requestModule.new("POST"):setUrl(ENDPOINT):setBody(payload)
    if TOKEN ~= "" then
        request:addHeader("Authorization", "Bearer " .. TOKEN)
    end

    request:send(function(response, err)
        if err then
            log("HTTP error: " .. tostring(err))
            return
        end
        if response then
            local status = response:getStatus()
            if type(status) == "number" and status >= 400 then
                log("server returned " .. tostring(status) .. ": " .. tostring(response:getBody()))
            end
        end
    end)
end

-- Handshake from the MD script: proves the MD -> Lua path works even when no
-- order is given. Shows up in debuglog.txt as "Ping".
local function onPing(_, param)
    log("Ping from MD: " .. tostring(param))
end

local ok, err = pcall(RegisterEvent, "FleetNotifier.OrderFinished", onOrderFinished)
if not ok then
    log("RegisterEvent(OrderFinished) failed: " .. tostring(err))
end

local okPing, errPing = pcall(RegisterEvent, "FleetNotifier.Ping", onPing)
if not okPing then
    log("RegisterEvent(Ping) failed: " .. tostring(errPing))
end

log("ui loaded")
