--[[
  Fleet Order Notifier -- UI side.

  Loaded into the menus Lua environment through ../ui.xml. The Mission Director
  script (../md/fleet_orders.xml) raises one string per finished order, split by
  "|" in this order:

      shipcode | shipname | ordername | sectorname

  The fields are forwarded as JSON to the Go server with the djfhe_http
  extension. See ../../README.md for setup.
]]

-- Go server address. It runs on the machine that runs X4, so 127.0.0.1 is
-- correct; use the LAN address if you move the server to another host.
local ENDPOINT = "http://127.0.0.1:8080/x4/order-finished"

-- Must match X4_TOKEN on the server. Leave "" when X4_TOKEN is not set.
local TOKEN = ""

local function log(msg)
    DebugError("[FleetNotifier] " .. tostring(msg))
end

local loaded, Request = pcall(require, "djfhe.http.request")
if not loaded then
    -- djfhe_http missing or disabled: stay quiet instead of breaking the UI.
    log("djfhe_http is not available, no notifications will be sent (" .. tostring(Request) .. ")")
    return
end

local function splitFields(text)
    local fields = {}
    for field in string.gmatch(text .. "|", "([^|]*)|") do
        fields[#fields + 1] = field
    end
    return fields
end

local function onOrderFinished(_, param)
    if type(param) ~= "string" or param == "" then
        return
    end

    local parts = splitFields(param)
    local payload = {
        code = parts[1] or "",
        ship = parts[2] or "",
        order = parts[3] or "",
        sector = parts[4] or "",
    }

    if payload.ship == "" then
        log("dropping event without a ship name: " .. tostring(param))
        return
    end

    local request = Request.new("POST"):setUrl(ENDPOINT):setBody(payload)
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

local ok, err = pcall(RegisterEvent, "FleetNotifier.OrderFinished", onOrderFinished)
if not ok then
    log("RegisterEvent failed: " .. tostring(err))
end
